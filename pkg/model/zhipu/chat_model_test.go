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

package zhipu_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/model/zhipu"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestChatModelCallFormatsZhipuRequestAndParsesReasoning(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newZhipuServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization header mismatch: %q", got)
		}
		requestCh <- body
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-zhipu",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "glm-5.1",
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":              "assistant",
					"reasoning_content": "plan",
					"content":           "hello",
					"tool_calls": []any{map[string]any{
						"id":   "call-1",
						"type": "function",
						"function": map[string]any{
							"name":      "Read",
							"arguments": `{"path":"README.md"}`,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
			"usage": map[string]any{
				"prompt_tokens":     7,
				"completion_tokens": 5,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 2,
				},
			},
		})
	})
	defer server.Close()

	maxTokens := int64(64)
	model, err := zhipu.NewChatModel(
		zhipu.NewCredential("test-key", zhipu.WithBaseURL(server.URL)),
		"glm-5.1",
		zhipu.WithChatParameters(zhipu.ChatParameters{
			MaxTokens:      &maxTokens,
			ThinkingEnable: true,
			Temperature:    floatPtr(0.6),
			TopP:           floatPtr(0.9),
		}),
		zhipu.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Read README")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	resp, err := model.Call(context.Background(), asmodel.CallRequest{
		Messages: []*message.Message{userMsg},
		Tools:    []asmodel.ToolSchema{readToolSchema()},
		ToolChoice: &types.ToolChoice{
			Mode:  "Read",
			Tools: []string{"Read"},
		},
	})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}

	body := <-requestCh
	if body["model"] != "glm-5.1" || body["stream"] != false || body["max_tokens"] != float64(64) {
		t.Fatalf("request parameters mismatch: %#v", body)
	}
	thinking := body["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Fatalf("Zhipu thinking parameter mismatch: %#v", thinking)
	}
	toolChoice := body["tool_choice"].(map[string]any)
	if toolChoice["type"] != "function" || toolChoice["function"].(map[string]any)["name"] != "Read" {
		t.Fatalf("tool choice not formatted: %#v", toolChoice)
	}
	if resp.ID != "chatcmpl-zhipu" || !resp.IsLast {
		t.Fatalf("response envelope mismatch: %#v", resp)
	}
	if thinkingBlock := resp.Content[0].(*message.ThinkingBlock); thinkingBlock.Thinking != "plan" {
		t.Fatalf("reasoning content not parsed: %#v", thinkingBlock)
	}
	if text := resp.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("text not parsed: %#v", resp)
	}
	if call := resp.Content[2].(*message.ToolCallBlock); call.ID != "call-1" || call.Name != "Read" || call.Input != `{"path":"README.md"}` {
		t.Fatalf("tool call not parsed: %#v", call)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 5 || resp.Usage.CacheInputTokens != 2 {
		t.Fatalf("usage not parsed: %#v", resp.Usage)
	}
}

func TestChatModelStreamAccumulatesZhipuReasoning(t *testing.T) {
	t.Parallel()

	server := newZhipuServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if body["stream"] != true {
			t.Fatalf("stream request should set stream=true: %#v", body)
		}
		if _, ok := body["stream_options"]; !ok {
			t.Fatalf("stream request should include stream_options: %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writer := bufio.NewWriter(w)
		for _, chunk := range []string{
			`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"pla"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"n","content":"he"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"llo","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"Read","arguments":"{\"path\""}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
		} {
			fmt.Fprintf(writer, "data: %s\n\n", chunk)
		}
		fmt.Fprint(writer, "data: [DONE]\n\n")
		if err := writer.Flush(); err != nil {
			t.Fatalf("Flush returned error: %v", err)
		}
	})
	defer server.Close()

	model, err := zhipu.NewChatModel(
		zhipu.NewCredential("test-key", zhipu.WithBaseURL(server.URL)),
		"glm-5.1",
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Read README")
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
	if len(chunks) != 5 {
		t.Fatalf("unexpected chunk count: %d %#v", len(chunks), chunks)
	}
	if chunks[0].IsLast || chunks[0].Content[0].(*message.ThinkingBlock).Thinking != "pla" {
		t.Fatalf("first reasoning delta mismatch: %#v", chunks[0])
	}
	final := chunks[len(chunks)-1]
	if !final.IsLast || final.ID != "chatcmpl-stream" {
		t.Fatalf("final response mismatch: %#v", final)
	}
	if thinking := final.Content[0].(*message.ThinkingBlock); thinking.Thinking != "plan" {
		t.Fatalf("final reasoning mismatch: %#v", thinking)
	}
	if text := final.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("final text mismatch: %#v", final)
	}
	if call := final.Content[2].(*message.ToolCallBlock); call.ID != "call-1" || call.Name != "Read" || call.Input != `{"path":"README.md"}` {
		t.Fatalf("final tool call mismatch: %#v", call)
	}
	if final.Usage.InputTokens != 3 || final.Usage.OutputTokens != 2 {
		t.Fatalf("final usage mismatch: %#v", final.Usage)
	}
}

func TestChatModelMapsZhipuErrors(t *testing.T) {
	t.Parallel()

	server := newZhipuServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(t, w, map[string]any{"error": map[string]any{"message": "rate limited", "code": "rate_limit"}})
	})
	defer server.Close()

	model, err := zhipu.NewChatModel(
		zhipu.NewCredential("test-key", zhipu.WithBaseURL(server.URL)),
		"glm-5.1",
		zhipu.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	_, err = model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Provider != "zhipu" || providerErr.StatusCode != http.StatusTooManyRequests || providerErr.Code != "rate_limit" {
		t.Fatalf("error not normalized: %#v err=%v", providerErr, err)
	}
}

func TestChatModelValidationAndTokenCount(t *testing.T) {
	t.Parallel()

	if _, err := zhipu.NewChatModel(zhipu.Credential{}, ""); err == nil {
		t.Fatal("missing credential and model should return error")
	}
	if _, err := zhipu.NewChatModel(
		zhipu.NewCredential("test-key"),
		"glm-5.1",
		zhipu.WithChatParameters(zhipu.ChatParameters{Temperature: floatPtr(3)}),
	); err == nil {
		t.Fatal("invalid temperature should return error")
	}
	model, err := zhipu.NewChatModel(zhipu.NewCredential("test-key"), "glm-5.1")
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	if got := model.Name(); got != "zhipu:glm-5.1" {
		t.Fatalf("Name mismatch: %q", got)
	}
	userMsg, err := message.NewUserMessage("Tony", "12345678")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	count, err := model.CountTokens(asmodel.CallRequest{Messages: []*message.Message{userMsg}, Tools: []asmodel.ToolSchema{readToolSchema()}})
	if err != nil {
		t.Fatalf("CountTokens returned error: %v", err)
	}
	if count <= 2 {
		t.Fatalf("count should include message and tool schema, got %d", count)
	}
}

func newZhipuServer(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		handler(w, r, body)
	}))
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("Encode response returned error: %v", err)
	}
}

func readToolSchema() asmodel.ToolSchema {
	return asmodel.ToolSchema{
		Type: "function",
		Function: asmodel.FunctionSchema{
			Name:        "Read",
			Description: "read files",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []any{"path"},
			},
		},
	}
}

func floatPtr(value float64) *float64 { return &value }
