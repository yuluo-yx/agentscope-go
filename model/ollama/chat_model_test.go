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

package ollama_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/ollama"
)

func TestChatModelCallFormatsRequestAndParsesResponse(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newOllamaChatServer(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		requestCh <- body
		writeOllamaLine(t, w, map[string]any{
			"model":             "qwen3:8b",
			"created_at":        time.Now().Format(time.RFC3339Nano),
			"message":           map[string]any{"role": "assistant", "content": "ollama ok"},
			"done":              true,
			"prompt_eval_count": 3,
			"eval_count":        2,
		})
	})
	defer server.Close()

	model, err := ollama.NewChatModel(
		ollama.NewCredential(ollama.WithHost(server.URL)),
		"qwen3:8b",
		ollama.WithStream(false),
		ollama.WithChatParameters(ollama.ChatParameters{Temperature: floatPtr(0.3), MaxTokens: intPtr(64)}),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("user", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	resp, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if got := model.Name(); got != "ollama:qwen3:8b" {
		t.Fatalf("Name mismatch: %q", got)
	}
	if got := resp.GetTextContent(""); got == nil || *got != "ollama ok" {
		t.Fatalf("response text mismatch: %#v", got)
	}
	if resp.Usage.InputTokens != 3 || resp.Usage.OutputTokens != 2 {
		t.Fatalf("usage not parsed: %#v", resp.Usage)
	}
	body := <-requestCh
	if body["model"] != "qwen3:8b" || body["stream"] != false {
		t.Fatalf("request envelope mismatch: %#v", body)
	}
	options := body["options"].(map[string]any)
	if options["temperature"] != 0.3 || options["num_predict"] != float64(64) {
		t.Fatalf("options not forwarded: %#v", options)
	}
}

func TestChatModelStreamEmitsDeltasAndFinalResponse(t *testing.T) {
	t.Parallel()

	server := newOllamaChatServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeOllamaLine(t, w, map[string]any{"message": map[string]any{"role": "assistant", "content": "hel"}, "done": false})
		writeOllamaLine(t, w, map[string]any{"message": map[string]any{"role": "assistant", "content": "lo"}, "done": true, "prompt_eval_count": 1, "eval_count": 2})
	})
	defer server.Close()

	model, err := ollama.NewChatModel(ollama.NewCredential(ollama.WithHost(server.URL)), "qwen3:8b")
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("user", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	stream, err := model.Stream(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	var chunks []asmodel.ChatResponse
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 3 {
		t.Fatalf("unexpected chunk count: %d %#v", len(chunks), chunks)
	}
	if text := chunks[0].Content.GetTextContent(""); chunks[0].IsLast || text == nil || *text != "hel" {
		t.Fatalf("first delta mismatch: %#v", chunks[0])
	}
	final := chunks[len(chunks)-1]
	if text := final.GetTextContent(""); !final.IsLast || text == nil || *text != "hello" {
		t.Fatalf("final response mismatch: %#v", final)
	}
}

func TestChatModelStreamEmitsTerminalError(t *testing.T) {
	t.Parallel()

	server := newOllamaChatServer(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		http.Error(w, "stream exploded", http.StatusInternalServerError)
	})
	defer server.Close()

	model, err := ollama.NewChatModel(ollama.NewCredential(ollama.WithHost(server.URL)), "qwen3:8b")
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("user", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	stream, err := model.Stream(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	var chunks []asmodel.ChatResponse
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected one terminal error chunk, got %d %#v", len(chunks), chunks)
	}
	if chunks[0].Error == nil || !strings.Contains(chunks[0].Error.Error(), "stream exploded") {
		t.Fatalf("expected stream error on terminal chunk, got %#v", chunks[0].Error)
	}
	if !chunks[0].IsLast {
		t.Fatalf("terminal error chunk should be marked last: %#v", chunks[0])
	}
}

func newOllamaChatServer(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		handler(w, r, body)
	}))
}

func writeOllamaLine(t *testing.T, w http.ResponseWriter, value map[string]any) {
	t.Helper()
	writer := bufio.NewWriter(w)
	if _, err := fmt.Fprint(writer, mustJSON(t, value), "\n"); err != nil {
		t.Fatalf("Fprint returned error: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	return string(data)
}

func floatPtr(value float64) *float64 {
	return &value
}

func intPtr(value int) *int {
	return &value
}
