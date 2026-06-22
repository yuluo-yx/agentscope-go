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

package openai_test

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
	asopenai "github.com/yuluo-yx/agentscope-go/pkg/model/openai"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestListModelsIncludesPythonAudioModelCard(t *testing.T) {
	t.Parallel()

	cards, err := asopenai.ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	audioMini := findOpenAICard(cards, "gpt-audio-mini")
	if audioMini.Name == "" || !audioMini.Supports(asmodel.ModelCapabilityAudio) || audioMini.Extra["api"] != "chat_completions" {
		t.Fatalf("OpenAI gpt-audio-mini should be loaded from Python model cards: %#v", audioMini)
	}
	audioProperties := modelCardProperties(t, audioMini)
	voice := audioProperties["voice"].(map[string]any)
	if voice["default"] != "alloy" {
		t.Fatalf("OpenAI audio model voice schema not merged: %#v", voice)
	}
	gpt41 := findOpenAICard(cards, "gpt-4.1")
	textProperties := modelCardProperties(t, gpt41)
	if _, exists := textProperties["voice"]; exists {
		t.Fatalf("non-audio OpenAI chat model should hide voice: %#v", textProperties["voice"])
	}
}

func TestChatModelCallFormatsRequestAndParsesResponse(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-1",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4o-mini",
			"choices": []any{
				map[string]any{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "hello",
						"tool_calls": []any{
							map[string]any{
								"id":   "call-1",
								"type": "function",
								"function": map[string]any{
									"name":      "Read",
									"arguments": `{"path":"README.md"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     7,
				"completion_tokens": 5,
				"total_tokens":      12,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 2,
				},
			},
		})
	})
	defer server.Close()

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-mini",
		asopenai.WithChatParameters(asopenai.ChatParameters{
			Temperature:       floatPtr(0.2),
			TopP:              floatPtr(0.9),
			ParallelToolCalls: boolPtr(false),
		}),
		asopenai.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}

	systemMsg, err := message.NewSystemMessage("system", "You are helpful")
	if err != nil {
		t.Fatalf("NewSystemMessage returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Read README.md")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	resp, err := model.Call(context.Background(), asmodel.CallRequest{
		Messages: []*message.Message{systemMsg, userMsg},
		Tools: []asmodel.ToolSchema{
			readToolSchema(),
			{
				Type: "function",
				Function: asmodel.FunctionSchema{
					Name:        "Write",
					Description: "write files",
					Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
		ToolChoice: &types.ToolChoice{Mode: "Read", Tools: []string{"Read"}},
	})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}

	assertCallRequest(t, <-requestCh)
	assertCallResponse(t, resp)
}

func TestChatModelVoiceParameterRequestsAudioModalities(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-voice",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-audio-mini",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
		})
	})
	defer server.Close()

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-audio-mini",
		asopenai.WithChatParameters(asopenai.ChatParameters{Voice: stringPtr("alloy")}),
		asopenai.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "say hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}

	body := <-requestCh
	audio := body["audio"].(map[string]any)
	if audio["voice"] != "alloy" || audio["format"] != "pcm16" {
		t.Fatalf("voice should request pcm16 audio output: %#v", audio)
	}
	modalities := body["modalities"].([]any)
	if len(modalities) != 2 || modalities[0] != "text" || modalities[1] != "audio" {
		t.Fatalf("voice should request text and audio modalities: %#v", modalities)
	}
}

func findOpenAICard(cards []asmodel.ModelCard, name string) asmodel.ModelCard {
	for _, card := range cards {
		if card.Name == name {
			return card
		}
	}
	return asmodel.ModelCard{}
}

func modelCardProperties(t *testing.T, card asmodel.ModelCard) map[string]any {
	t.Helper()
	properties, ok := card.ParameterSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("model card schema properties missing for %s: %#v", card.Name, card.ParameterSchema)
	}
	return properties
}

func TestChatModelStreamAccumulatesDeltas(t *testing.T) {
	t.Parallel()

	server := newStreamingChatCompletionServer(t)
	defer server.Close()

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-mini",
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Read README.md")
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
	assertStreamResponses(t, chunks)
}

func assertCallRequest(t *testing.T, body map[string]any) {
	t.Helper()
	if body["model"] != "gpt-4o-mini" {
		t.Fatalf("unexpected model in request: %#v", body["model"])
	}
	if body["stream"] != false {
		t.Fatalf("non-stream Call should set stream=false, got %#v", body["stream"])
	}
	messages := body["messages"].([]any)
	if messages[0].(map[string]any)["role"] != "system" || messages[0].(map[string]any)["content"] != "You are helpful" {
		t.Fatalf("system message not formatted: %#v", messages[0])
	}
	if messages[1].(map[string]any)["role"] != "user" || messages[1].(map[string]any)["content"] != "Read README.md" {
		t.Fatalf("user message not formatted: %#v", messages[1])
	}
	assertToolChoiceRequest(t, body)
}

func assertToolChoiceRequest(t *testing.T, body map[string]any) {
	t.Helper()
	tools := body["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["function"].(map[string]any)["name"] != "Read" {
		t.Fatalf("tools should be filtered to Read: %#v", tools)
	}
	toolChoice := body["tool_choice"].(map[string]any)
	if toolChoice["type"] != "function" || toolChoice["function"].(map[string]any)["name"] != "Read" {
		t.Fatalf("forced tool choice not formatted: %#v", toolChoice)
	}
	if body["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls should be false, got %#v", body["parallel_tool_calls"])
	}
}

func assertCallResponse(t *testing.T, resp *asmodel.ChatResponse) {
	t.Helper()
	if resp.ID != "chatcmpl-1" || !resp.IsLast {
		t.Fatalf("unexpected response envelope: %#v", resp)
	}
	if got := resp.GetTextContent(""); got == nil || *got != "hello" {
		t.Fatalf("text block mismatch: %#v", got)
	}
	call := resp.Content[1].(*message.ToolCallBlock)
	if call.ID != "call-1" || call.Name != "Read" || call.Input != `{"path":"README.md"}` {
		t.Fatalf("tool call block mismatch: %#v", call)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 5 || resp.Usage.CacheInputTokens != 2 {
		t.Fatalf("usage not parsed: %#v", resp.Usage)
	}
}

func newStreamingChatCompletionServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		assertStreamRequest(t, body)
		writeStreamChunks(t, w)
	})
}

func assertStreamRequest(t *testing.T, body map[string]any) {
	t.Helper()
	if body["stream"] != true {
		t.Fatalf("stream request should set stream=true, got %#v", body["stream"])
	}
	if _, ok := body["stream_options"]; !ok {
		t.Fatalf("stream request should include usage stream_options: %#v", body)
	}
}

func writeStreamChunks(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	chunks := []string{
		`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hel"},"finish_reason":null}]}`,
		`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"Read","arguments":"{\"path\""}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"chatcmpl-stream","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
	}
	writer := bufio.NewWriter(w)
	for _, chunk := range chunks {
		fmt.Fprintf(writer, "data: %s\n\n", chunk)
	}
	fmt.Fprint(writer, "data: [DONE]\n\n")
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
}

func assertStreamResponses(t *testing.T, chunks []asmodel.ChatResponse) {
	t.Helper()
	if len(chunks) != 4 {
		t.Fatalf("unexpected stream chunk count: %d %#v", len(chunks), chunks)
	}
	if text := chunks[0].Content.GetTextContent(""); chunks[0].IsLast || text == nil || *text != "hel" {
		t.Fatalf("first delta mismatch: %#v", chunks[0])
	}
	if text := chunks[1].Content.GetTextContent(""); text == nil || *text != "lo" || chunks[1].Content[1].(*message.ToolCallBlock).Input != `{"path"` {
		t.Fatalf("second delta mismatch: %#v", chunks[1])
	}
	final := chunks[len(chunks)-1]
	if !final.IsLast || final.ID != "chatcmpl-stream" {
		t.Fatalf("final stream response mismatch: %#v", final)
	}
	if text := final.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("final text not accumulated: %#v", final.Content[0])
	}
	toolCall := final.Content[1].(*message.ToolCallBlock)
	if toolCall.ID != "call-1" || toolCall.Name != "Read" || toolCall.Input != `{"path":"README.md"}` {
		t.Fatalf("final tool call not accumulated: %#v", toolCall)
	}
	if final.Usage.InputTokens != 3 || final.Usage.OutputTokens != 2 {
		t.Fatalf("final usage not attached: %#v", final.Usage)
	}
}

func TestChatModelFormatsAssistantToolCallsAndToolResults(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-2",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4o-mini",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "done"},
				"finish_reason": "stop",
			}},
		})
	})
	defer server.Close()

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-mini",
		asopenai.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	assistantMsg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{
		message.NewTextBlock("checking"),
		message.NewToolCallBlock("call-1", "Read", `{"path":"README.md"}`),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	toolMsg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{
		message.NewToolResultBlock("call-1", "Read", message.ToolResultOutput{Raw: "README contents"}, message.ToolResultSuccess),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage tool result returned error: %v", err)
	}

	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{assistantMsg, toolMsg}}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	messages := (<-requestCh)["messages"].([]any)
	assistant := messages[0].(map[string]any)
	if assistant["role"] != "assistant" || assistant["content"] != "checking" {
		t.Fatalf("assistant message not formatted: %#v", assistant)
	}
	toolCalls := assistant["tool_calls"].([]any)
	if toolCalls[0].(map[string]any)["id"] != "call-1" {
		t.Fatalf("tool call not formatted: %#v", toolCalls)
	}
	toolResult := messages[1].(map[string]any)
	if toolResult["role"] != "tool" || toolResult["tool_call_id"] != "call-1" || toolResult["content"] != "README contents" {
		t.Fatalf("tool result not formatted: %#v", toolResult)
	}
}

func TestChatModelMapsProviderErrors(t *testing.T) {
	t.Parallel()

	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(t, w, map[string]any{"error": map[string]any{"message": "rate limited", "code": "rate_limit"}})
	})
	defer server.Close()

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-mini",
		asopenai.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "hello")
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
	if providerErr.Provider != "openai" {
		t.Fatalf("provider metadata mismatch: %#v", providerErr)
	}
	if providerErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status code not mapped: %#v", providerErr)
	}
}

func TestChatModelUsesCustomHTTPClient(t *testing.T) {
	t.Parallel()

	requestCh := make(chan http.Header, 1)
	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- r.Header.Clone()
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-custom-client",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4o-mini",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
		})
	})
	defer server.Close()

	client := &http.Client{
		Transport: headerRoundTripper{
			base:  http.DefaultTransport,
			key:   "X-AgentScope-Test-Client",
			value: "custom",
		},
	}
	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-mini",
		asopenai.WithStream(false),
		asopenai.WithHTTPClient(client),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}

	userMsg, err := message.NewUserMessage("Tony", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if got := (<-requestCh).Get("X-AgentScope-Test-Client"); got != "custom" {
		t.Fatalf("custom HTTP client was not used, got header %q", got)
	}
}

func TestChatModelWithHeaderOverridesRequestHeader(t *testing.T) {
	t.Parallel()

	headerCh := make(chan http.Header, 1)
	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		headerCh <- r.Header.Clone()
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-header",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4o-mini",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
		})
	})
	defer server.Close()

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-mini",
		asopenai.WithStream(false),
		asopenai.WithHeader("User-Agent", "TestAgent/1.0"),
		asopenai.WithHeader("X-Custom-Header", "custom-value"),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	headers := <-headerCh
	if got := headers.Get("User-Agent"); got != "TestAgent/1.0" {
		t.Fatalf("User-Agent header not overridden, got %q", got)
	}
	if got := headers.Get("X-Custom-Header"); got != "custom-value" {
		t.Fatalf("custom header not forwarded, got %q", got)
	}
}

func TestChatModelValidationAndTokenCount(t *testing.T) {
	t.Parallel()

	if _, err := asopenai.NewChatModel(asopenai.Credential{}, ""); err == nil {
		t.Fatal("missing credential and model should return error")
	}
	credential := asopenai.NewCredential("test-key")
	model, err := asopenai.NewChatModel(credential, "gpt-4o-mini")
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	if model.Name() != "openai:gpt-4o-mini" {
		t.Fatalf("unexpected model name: %q", model.Name())
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

func TestChatModelFormatsMultimodalUserContentAndOptions(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	headerCh := make(chan http.Header, 1)
	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		headerCh <- r.Header.Clone()
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-multimodal",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4o-mini",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
		})
	})
	defer server.Close()

	maxTokens := int64(64)
	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL), asopenai.WithOrganization("org-1")),
		"gpt-4o-mini",
		asopenai.WithChatParameters(asopenai.ChatParameters{
			MaxTokens:       &maxTokens,
			ThinkingEnable:  true,
			ReasoningEffort: "high",
		}),
		asopenai.WithContextSize(2048),
		asopenai.WithMaxRetries(1),
		asopenai.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}

	userMsg, err := message.NewUserMessage("Tony", []message.ContentBlock{
		message.NewTextBlock("inspect"),
		message.NewDataBlock(message.NewBase64Source("abc", "image/png")),
		message.NewDataBlock(message.NewBase64Source("def", "audio/wav")),
		message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
	})
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}

	body := <-requestCh
	if body["max_tokens"] != float64(64) || body["reasoning_effort"] != "high" {
		t.Fatalf("parameters not forwarded: %#v", body)
	}
	content := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["text"] != "inspect" {
		t.Fatalf("text content part mismatch: %#v", content[0])
	}
	if imageURL := content[1].(map[string]any)["image_url"].(map[string]any)["url"]; imageURL != "data:image/png;base64,abc" {
		t.Fatalf("base64 image not formatted: %#v", content[1])
	}
	if audio := content[2].(map[string]any)["input_audio"].(map[string]any); audio["data"] != "def" || audio["format"] != "wav" {
		t.Fatalf("audio not formatted: %#v", content[2])
	}
	if imageURL := content[3].(map[string]any)["image_url"].(map[string]any)["url"]; imageURL != "https://example.com/image.png" {
		t.Fatalf("URL image not formatted: %#v", content[3])
	}
	if got := (<-headerCh).Get("OpenAI-Organization"); got != "org-1" {
		t.Fatalf("organization header not forwarded: %q", got)
	}
}

func TestChatModelExtraBodyDefaultsAndPerCallOverride(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 3)
	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-extra-body",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "custom-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
		})
	})
	defer server.Close()

	defaultModel, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"custom-model",
		asopenai.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel default returned error: %v", err)
	}
	if _, err := defaultModel.Call(context.Background(), asmodel.CallRequest{}); err != nil {
		t.Fatalf("default Call returned error: %v", err)
	}
	if _, exists := (<-requestCh)["enable_thinking"]; exists {
		t.Fatal("default model should not send provider-specific extra body fields")
	}

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"custom-model",
		asopenai.WithStream(false),
		asopenai.WithExtraBody(map[string]any{"enable_thinking": false}),
	)
	if err != nil {
		t.Fatalf("NewChatModel with extra body returned error: %v", err)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{}); err != nil {
		t.Fatalf("constructor extra body Call returned error: %v", err)
	}
	if got := (<-requestCh)["enable_thinking"]; got != false {
		t.Fatalf("constructor extra body mismatch: %#v", got)
	}

	if _, err := model.Call(context.Background(), asmodel.CallRequest{
		Parameters: map[string]any{"extra_body": map[string]any{"custom_option": "value"}},
	}); err != nil {
		t.Fatalf("per-call extra body Call returned error: %v", err)
	}
	body := <-requestCh
	if got := body["custom_option"]; got != "value" {
		t.Fatalf("per-call extra body should be forwarded, got %#v", got)
	}
	if _, exists := body["enable_thinking"]; exists {
		t.Fatalf("per-call extra body should override constructor default, got %#v", body)
	}
}

func TestChatModelParsesAudioOutputAndRejectsVideoInput(t *testing.T) {
	t.Parallel()

	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-audio",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4o-audio-preview",
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"audio": map[string]any{
						"id":         "audio-1",
						"data":       "UklGRg==",
						"expires_at": time.Now().Add(time.Hour).Unix(),
						"transcript": "spoken answer",
					},
				},
				"finish_reason": "stop",
			}},
		})
	})
	defer server.Close()

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-audio-preview",
		asopenai.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "speak")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	resp, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("expected audio data plus transcript, got %#v", resp.Content)
	}
	audio := resp.Content[0].(*message.DataBlock)
	source := audio.Source.(*message.Base64Source)
	if audio.ID != "audio-1" || source.Data != "UklGRg==" || source.MediaType != "audio/wav" {
		t.Fatalf("audio output not parsed: %#v source=%#v", audio, source)
	}
	if text := resp.Content[1].(*message.TextBlock).Text; text != "spoken answer" {
		t.Fatalf("audio transcript not preserved: %q", text)
	}

	videoMsg, err := message.NewUserMessage("Tony", []message.ContentBlock{
		message.NewDataBlock(message.NewBase64Source("AAAA", "video/mp4")),
	})
	if err != nil {
		t.Fatalf("NewUserMessage video returned error: %v", err)
	}
	_, err = model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{videoMsg}})
	var capabilityErr *asmodel.CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != asmodel.ModelCapabilityVideo {
		t.Fatalf("video should be rejected with CapabilityError, got %T %v", err, err)
	}
}

func TestOpenAIListModelsIncludesChatMetadataOnly(t *testing.T) {
	t.Parallel()

	cards, err := asopenai.ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	seen := map[string]asmodel.ModelCard{}
	for _, card := range cards {
		seen[card.Name] = card
	}
	chat := seen["gpt-4o-mini"]
	if chat.Name == "" || !chat.Supports(asmodel.ModelCapabilityImage) || chat.Supports(asmodel.ModelCapabilityAudio) || chat.Supports(asmodel.ModelCapabilityVideo) {
		t.Fatalf("chat metadata capability mismatch: %#v", chat)
	}
	for _, card := range cards {
		if card.Extra["api"] == "responses" {
			t.Fatalf("OpenAI Chat metadata should not include Responses API cards: %#v", card)
		}
	}
}

func TestChatModelFormatsToolResultBlocksAndToolChoiceModes(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-tool-result-blocks",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4o-mini",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": ""},
				"finish_reason": "stop",
			}},
		})
	})
	defer server.Close()

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-mini",
		asopenai.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	toolMsg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{
		message.NewToolResultBlock("call-1", "Read", message.ToolResultOutput{Blocks: message.ContentBlockList{
			message.NewTextBlock("block"),
			message.NewTextBlock(" result"),
		}}, message.ToolResultSuccess),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{
		Messages:   []*message.Message{toolMsg},
		Tools:      []asmodel.ToolSchema{readToolSchema()},
		ToolChoice: &types.ToolChoice{Mode: string(types.ToolChoiceRequired)},
	}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	body := <-requestCh
	if body["tool_choice"] != string(types.ToolChoiceRequired) {
		t.Fatalf("literal tool choice not forwarded: %#v", body["tool_choice"])
	}
	toolResult := body["messages"].([]any)[0].(map[string]any)
	if toolResult["content"] != "block result" {
		t.Fatalf("tool result block text not joined: %#v", toolResult)
	}
}

func TestChatModelValidationBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parameters asopenai.ChatParameters
	}{
		{name: "max tokens", parameters: asopenai.ChatParameters{MaxTokens: int64Ptr(0)}},
		{name: "temperature", parameters: asopenai.ChatParameters{Temperature: floatPtr(3)}},
		{name: "top p", parameters: asopenai.ChatParameters{TopP: floatPtr(0)}},
		{name: "reasoning", parameters: asopenai.ChatParameters{ReasoningEffort: "extreme"}},
		{name: "voice", parameters: asopenai.ChatParameters{Voice: stringPtr(" ")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := asopenai.NewChatModel(
				asopenai.NewCredential("test-key"),
				"gpt-4o-mini",
				asopenai.WithChatParameters(tt.parameters),
			); err == nil {
				t.Fatal("invalid parameters should return error")
			}
		})
	}

	if _, err := asopenai.NewChatModel(asopenai.NewCredential("test-key"), "gpt-4o-mini", asopenai.WithMaxRetries(0)); err == nil {
		t.Fatal("max retries <= 0 should return error")
	}
}

func TestChatModelNilAndProviderErrorBranches(t *testing.T) {
	t.Parallel()

	var model *asopenai.ChatModel
	if _, err := model.Call(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("nil model Call should return error")
	}
	if _, err := model.Stream(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("nil model Stream should return error")
	}
	if model.Name() != "openai:<nil>" {
		t.Fatalf("nil model name mismatch: %q", model.Name())
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "plain server error", http.StatusInternalServerError)
	}))
	defer server.Close()
	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-mini",
		asopenai.WithStream(false),
		asopenai.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	_, err = model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err == nil {
		t.Fatal("server error should return provider error")
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("server error not normalized: %#v", err)
	}
}

func TestChatModelNoChoicesResponse(t *testing.T) {
	t.Parallel()

	server := newChatCompletionServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		writeJSON(t, w, map[string]any{
			"id":      "chatcmpl-empty",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4o-mini",
			"choices": []any{},
		})
	})
	defer server.Close()

	model, err := asopenai.NewChatModel(
		asopenai.NewCredential("test-key", asopenai.WithBaseURL(server.URL)),
		"gpt-4o-mini",
		asopenai.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	resp, err := model.Call(context.Background(), asmodel.CallRequest{})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if resp.ID != "chatcmpl-empty" || len(resp.Content) != 0 || resp.Usage != nil {
		t.Fatalf("empty response not normalized: %#v", resp)
	}
}

func newChatCompletionServer(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) *httptest.Server {
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

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
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
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []any{"path"},
			},
		},
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

type headerRoundTripper struct {
	base  http.RoundTripper
	key   string
	value string
}

func (r headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set(r.key, r.value)
	return r.base.RoundTrip(req)
}
