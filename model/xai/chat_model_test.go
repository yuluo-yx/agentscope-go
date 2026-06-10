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

package xai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestChatModelUsesXAIDefaultsAndDelegatesRequests(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]any, 2)
	server := newXAIServer(t, requests)
	defer server.Close()

	model, err := NewChatModel(
		NewCredential("test-key", WithBaseURL(server.URL+"/")),
		"grok-3",
		WithChatParameters(ChatParameters{Temperature: floatPtr(0.3)}),
		WithStream(false),
		WithContextSize(4096),
		WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	if got := model.Name(); got != "xai:grok-3" {
		t.Fatalf("Name mismatch: %q", got)
	}
	userMsg, err := message.NewUserMessage("user", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	resp, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if got := resp.GetTextContent(""); got == nil || *got != "xai ok" {
		t.Fatalf("Call text mismatch: %#v", got)
	}
	if body := <-requests; body["model"] != "grok-3" || body["stream"] != false {
		t.Fatalf("Call request mismatch: %#v", body)
	}

	stream, err := model.Stream(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	var chunks []asmodel.ChatResponse
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 || !chunks[len(chunks)-1].IsLast {
		t.Fatalf("stream should yield a final chunk: %#v", chunks)
	}
	if body := <-requests; body["model"] != "grok-3" || body["stream"] != true {
		t.Fatalf("Stream request mismatch: %#v", body)
	}

	tokens, err := model.CountTokens(asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		t.Fatalf("CountTokens returned error: %v", err)
	}
	if tokens == 0 {
		t.Fatal("CountTokens should estimate a positive token count")
	}
}

func TestNilChatModelReportsXAIErrors(t *testing.T) {
	t.Parallel()

	var model *ChatModel
	if got := model.Name(); got != "xai:<nil>" {
		t.Fatalf("nil Name mismatch: %q", got)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{}); err == nil || !strings.Contains(err.Error(), "xai: nil chat model") {
		t.Fatalf("nil Call error mismatch: %v", err)
	}
	if _, err := model.Stream(context.Background(), asmodel.CallRequest{}); err == nil || !strings.Contains(err.Error(), "xai: nil chat model") {
		t.Fatalf("nil Stream error mismatch: %v", err)
	}
	if _, err := model.CountTokens(asmodel.CallRequest{}); err == nil || !strings.Contains(err.Error(), "xai: nil chat model") {
		t.Fatalf("nil CountTokens error mismatch: %v", err)
	}
}

func TestListModelsReturnsXAICards(t *testing.T) {
	t.Parallel()

	cards, err := ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("xAI model cards should not be empty")
	}
	for _, card := range cards {
		if card.Extra["provider"] != providerName || !card.Supports(asmodel.ModelCapabilityTools) {
			t.Fatalf("model card provider mismatch: %#v", card)
		}
	}
}

func newXAIServer(t *testing.T, requests chan<- map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		requests <- body
		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			writer := bufio.NewWriter(w)
			fmt.Fprint(writer, "data: {\"id\":\"xai-stream\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"xai stream\"},\"finish_reason\":\"stop\"}]}\n\n")
			fmt.Fprint(writer, "data: [DONE]\n\n")
			if err := writer.Flush(); err != nil {
				t.Fatalf("Flush returned error: %v", err)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"id":      "xai-call",
			"object":  "chat.completion",
			"created": int64(1),
			"model":   body["model"],
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "xai ok"},
				"finish_reason": "stop",
			}},
		}); err != nil {
			t.Fatalf("Encode response returned error: %v", err)
		}
	}))
}

func floatPtr(value float64) *float64 {
	return &value
}
