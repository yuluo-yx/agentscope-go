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

package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/model/deepseek"
	"github.com/yuluo-yx/agentscope-go/model/moonshot"
	"github.com/yuluo-yx/agentscope-go/model/xai"
)

func TestOpenAICompatibleProviderPackagesUseProviderDefaults(t *testing.T) {
	t.Parallel()

	server := newCompatibleChatServer(t, http.StatusOK)
	defer server.Close()

	tests := []struct {
		name    string
		new     func(string) (asmodel.ChatModel, error)
		want    string
		request string
	}{
		{
			name: "deepseek",
			new: func(baseURL string) (asmodel.ChatModel, error) {
				return deepseek.NewChatModel(deepseek.NewCredential("test-key", deepseek.WithBaseURL(baseURL)), "deepseek-chat", deepseek.WithStream(false))
			},
			want:    "deepseek:deepseek-chat",
			request: "deepseek-chat",
		},
		{
			name: "dashscope",
			new: func(baseURL string) (asmodel.ChatModel, error) {
				return dashscope.NewChatModel(dashscope.NewCredential("test-key", dashscope.WithBaseURL(baseURL)), "qwen-plus", dashscope.WithStream(false))
			},
			want:    "dashscope:qwen-plus",
			request: "qwen-plus",
		},
		{
			name: "moonshot",
			new: func(baseURL string) (asmodel.ChatModel, error) {
				return moonshot.NewChatModel(moonshot.NewCredential("test-key", moonshot.WithBaseURL(baseURL)), "moonshot-v1-8k", moonshot.WithStream(false))
			},
			want:    "moonshot:moonshot-v1-8k",
			request: "moonshot-v1-8k",
		},
		{
			name: "xai",
			new: func(baseURL string) (asmodel.ChatModel, error) {
				return xai.NewChatModel(xai.NewCredential("test-key", xai.WithBaseURL(baseURL)), "grok-3", xai.WithStream(false))
			},
			want:    "xai:grok-3",
			request: "grok-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := tt.new(server.URL)
			if err != nil {
				t.Fatalf("NewChatModel returned error: %v", err)
			}
			if got := model.Name(); got != tt.want {
				t.Fatalf("Name mismatch: got %q want %q", got, tt.want)
			}
			userMsg, err := message.NewUserMessage("user", "hello")
			if err != nil {
				t.Fatalf("NewUserMessage returned error: %v", err)
			}
			resp, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
			if err != nil {
				t.Fatalf("Call returned error: %v", err)
			}
			if got := resp.GetTextContent(""); got == nil || *got != "compatible ok" {
				t.Fatalf("response text mismatch: %#v", got)
			}
		})
	}
}

func TestOpenAICompatibleProviderErrorsUseProviderName(t *testing.T) {
	t.Parallel()

	server := newCompatibleChatServer(t, http.StatusTooManyRequests)
	defer server.Close()

	model, err := deepseek.NewChatModel(
		deepseek.NewCredential("test-key", deepseek.WithBaseURL(server.URL)),
		"deepseek-chat",
		deepseek.WithStream(false),
		deepseek.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("user", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	_, err = model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err == nil {
		t.Fatal("Call should return provider error")
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error should expose ProviderError, got %T %v", err, err)
	}
	if providerErr.Provider != "deepseek" || providerErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("provider error metadata mismatch: %#v", providerErr)
	}
}

func newCompatibleChatServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		if body["model"] == "" {
			t.Fatalf("model was not forwarded: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= 400 {
			if err := json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "rate limited", "code": "rate_limit"}}); err != nil {
				t.Fatalf("Encode error response returned error: %v", err)
			}
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-compatible",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   body["model"],
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "compatible ok"},
				"finish_reason": "stop",
			}},
		}); err != nil {
			t.Fatalf("Encode response returned error: %v", err)
		}
	}))
}
