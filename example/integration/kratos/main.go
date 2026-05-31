// Copyright The AgentScope Go Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	khttp "github.com/go-kratos/kratos/v2/transport/http"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

type chatRequest struct {
	Message string `json:"message"`
}

type scriptedStreamModel struct {
	name  string
	parts []string
}

func newScriptedStreamModel(name string, parts ...string) *scriptedStreamModel {
	return &scriptedStreamModel{name: name, parts: append([]string(nil), parts...)}
}

func (m *scriptedStreamModel) Name() string {
	return m.name
}

func (m *scriptedStreamModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock(strings.Join(m.parts, ""))}, true), nil
}

func (m *scriptedStreamModel) Stream(ctx context.Context, _ asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		blockID := "scripted-text"
		var full strings.Builder
		for _, part := range m.parts {
			full.WriteString(part)
			chunk := asmodel.NewChatResponse(
				message.ContentBlockList{message.NewTextBlock(part, message.WithBlockID(blockID))},
				false,
			)
			select {
			case <-ctx.Done():
				return
			case out <- *chunk:
			}
			time.Sleep(5 * time.Millisecond)
		}
		final := asmodel.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock(full.String(), message.WithBlockID(blockID))},
			true,
			asmodel.WithChatResponseUsage(&asmodel.ChatUsage{InputTokens: 12, OutputTokens: len(m.parts), Type: asmodel.UsageTypeChat}),
		)
		select {
		case <-ctx.Done():
		case out <- *final:
		}
	}()
	return out, nil
}

func (m *scriptedStreamModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	server := newServer(listener)
	errs := make(chan error, 1)
	go func() {
		errs <- server.Start(context.Background())
	}()

	baseURL := "http://" + listener.Addr().String()
	waitForServer(baseURL + "/ping")
	fmt.Printf("kratos_server=%s\n", baseURL)
	fmt.Printf("kratos_chat_model_stream=%q\n", shorten(postJSON(baseURL+"/chat-model/stream", chatRequest{Message: "Stream from ChatModel"}), 180))
	fmt.Printf("kratos_agent_stream=%q\n", shorten(postJSON(baseURL+"/agent/stream", chatRequest{Message: "Stream from Agent"}), 180))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		log.Fatalf("stop: %v", err)
	}
	if err := <-errs; err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func newServer(listener net.Listener) *khttp.Server {
	server := khttp.NewServer(khttp.Listener(listener))
	server.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"message":"pong"}`)
	})
	server.HandleFunc("/chat-model/stream", chatModelStreamHandler)
	server.HandleFunc("/agent/stream", agentStreamHandler)
	return server
}

func chatModelStreamHandler(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message field is required", http.StatusBadRequest)
		return
	}
	model := newScriptedStreamModel("kratos-chat-model", "ChatModel ", "streams ", "through ", "Kratos.")
	user := mustMessage(message.NewUserMessage("user", req.Message))
	stream, err := model.Stream(r.Context(), asmodel.CallRequest{
		Messages: []*message.Message{user},
		Stream:   true,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	for response := range stream {
		if response.IsLast {
			if err := writeSSE(w, "final", map[string]any{"text": textContent(response.Content)}); err != nil {
				return
			}
			continue
		}
		if err := writeSSE(w, "delta", map[string]any{"text": textContent(response.Content)}); err != nil {
			return
		}
	}
	_ = writeSSE(w, "done", map[string]any{"state": "ok"})
}

func agentStreamHandler(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message field is required", http.StatusBadRequest)
		return
	}
	model := newScriptedStreamModel("kratos-agent-model", "Agent ", "streams ", "events ", "through ", "Kratos.")
	agent := mustAgent(agentpkg.NewAgent("kratos-agent", "Reply with one short sentence.", model))
	user := mustMessage(message.NewUserMessage("user", req.Message))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	err := agent.ReplyStream(r.Context(), user, func(event message.Event) error {
		switch e := event.(type) {
		case *message.TextBlockDeltaEvent:
			return writeSSE(w, "agent_delta", map[string]any{"delta": e.Delta})
		case *message.ModelCallEndEvent:
			return writeSSE(w, "agent_usage", map[string]any{"input_tokens": e.InputTokens, "output_tokens": e.OutputTokens})
		case *message.ReplyEndEvent:
			return writeSSE(w, "agent_done", map[string]any{"state": "ok"})
		default:
			return nil
		}
	})
	if err != nil {
		_ = writeSSE(w, "error", map[string]any{"error": err.Error()})
	}
}

func writeSSE(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func waitForServer(url string) {
	deadline := time.Now().Add(time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			log.Fatalf("server did not become ready at %s", url)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func postJSON(url string, body any) string {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return string(content)
}

func textContent(blocks message.ContentBlockList) string {
	var builder strings.Builder
	for _, block := range blocks {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func shorten(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", "\\n"))
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func mustAgent(agent *agentpkg.Agent, err error) *agentpkg.Agent {
	if err != nil {
		panic(err)
	}
	return agent
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}

var _ asmodel.ChatModel = (*scriptedStreamModel)(nil)
