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

package anthropic_test

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

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	asanthropic "github.com/yuluo-yx/agentscope-go/model/anthropic"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestChatModelCallFormatsRequestAndParsesResponse(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newMessageServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, anthropicMessageResponse())
	})
	defer server.Close()

	maxTokens := int64(4096)
	model, err := asanthropic.NewChatModel(
		asanthropic.NewCredential("test-key", asanthropic.WithBaseURL(server.URL)),
		"claude-sonnet-4-5",
		asanthropic.WithChatParameters(asanthropic.ChatParameters{
			MaxTokens:            &maxTokens,
			Temperature:          floatPtr(0.2),
			TopP:                 floatPtr(0.9),
			TopK:                 int64Ptr(50),
			ThinkingBudgetTokens: int64Ptr(1024),
			ThinkingDisplay:      "summarized",
		}),
		asanthropic.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}

	systemMsg := mustSystemMessage(t, "You are helpful")
	userMsg := mustUserMessage(t, "Read README.md")
	resp, err := model.Call(context.Background(), asmodel.CallRequest{
		Messages: []*message.Message{systemMsg, userMsg},
		Tools:    []asmodel.ToolSchema{readToolSchema(), writeToolSchema()},
		ToolChoice: &types.ToolChoice{
			Mode:  "Read",
			Tools: []string{"Read"},
		},
	})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}

	assertCallRequest(t, <-requestCh)
	assertCallResponse(t, resp)
}

func TestChatModelStreamAccumulatesEvents(t *testing.T) {
	t.Parallel()

	server := newStreamingMessageServer(t)
	defer server.Close()
	model, err := asanthropic.NewChatModel(
		asanthropic.NewCredential("test-key", asanthropic.WithBaseURL(server.URL)),
		"claude-sonnet-4-5",
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}

	stream, err := model.Stream(context.Background(), asmodel.CallRequest{Messages: []*message.Message{mustUserMessage(t, "Read README.md")}})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	var chunks []asmodel.ChatResponse
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	assertStreamResponses(t, chunks)
}

func TestChatModelStreamHandlesThinkingEvents(t *testing.T) {
	t.Parallel()

	server := newThinkingStreamServer(t)
	defer server.Close()
	model, err := asanthropic.NewChatModel(
		asanthropic.NewCredential("test-key", asanthropic.WithBaseURL(server.URL)),
		"claude-sonnet-4-5",
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}

	stream, err := model.Stream(context.Background(), asmodel.CallRequest{Messages: []*message.Message{mustUserMessage(t, "think")}})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	var chunks []asmodel.ChatResponse
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	assertThinkingStreamResponses(t, chunks)
}

func TestChatModelFormatsToolResultHistory(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newMessageServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, textOnlyResponse("ok"))
	})
	defer server.Close()
	model := mustAnthropicModel(t, server.URL)
	toolMsg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{
		message.NewToolResultBlock("toolu_1", "Read", message.ToolResultOutput{Raw: "README contents"}, message.ToolResultSuccess),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}

	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{toolMsg}}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	assertToolResultRequest(t, <-requestCh)
}

func TestChatModelFormatsToolResultBlockOutput(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newMessageServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, textOnlyResponse("ok"))
	})
	defer server.Close()
	model := mustAnthropicModel(t, server.URL)
	toolMsg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{
		message.NewToolResultBlock("toolu_1", "Read", message.ToolResultOutput{Blocks: message.ContentBlockList{
			message.NewTextBlock("block"),
			message.NewTextBlock(" result"),
		}}, message.ToolResultError),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}

	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{toolMsg}}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	result := (<-requestCh)["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if result["content"].([]any)[0].(map[string]any)["text"] != "block result" || result["is_error"] != true {
		t.Fatalf("tool result block output not formatted: %#v", result)
	}
}

func TestChatModelFormatsAssistantHistory(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newMessageServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, textOnlyResponse("ok"))
	})
	defer server.Close()
	model := mustAnthropicModel(t, server.URL)
	assistantMsg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{
		message.NewHintBlock("hint"),
		message.NewThinkingBlock("checking", message.WithExtra("signature", "sig-history")),
		message.NewToolCallBlock("toolu_1", "Read", `{"path":"README.md"}`),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}

	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{assistantMsg}}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	assertAssistantHistoryRequest(t, <-requestCh)
}

func TestChatModelFormatsBuiltinToolChoiceModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "auto", mode: string(types.ToolChoiceAuto), want: "auto"},
		{name: "none", mode: string(types.ToolChoiceNone), want: "none"},
		{name: "required", mode: string(types.ToolChoiceRequired), want: "any"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := callWithToolChoice(t, tt.mode)
			choice := body["tool_choice"].(map[string]any)
			if choice["type"] != tt.want {
				t.Fatalf("tool choice mismatch: %#v", choice)
			}
		})
	}
}

func TestChatModelFormatsImageContent(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newMessageServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, textOnlyResponse("seen"))
	})
	defer server.Close()
	model := mustAnthropicModel(t, server.URL)
	userMsg, err := message.NewUserMessage("Tony", []message.ContentBlock{
		message.NewTextBlock("inspect"),
		message.NewDataBlock(message.NewBase64Source("abc", "image/png")),
		message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
	})
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	assertImageRequest(t, <-requestCh)
}

func TestChatModelRejectsUnsupportedAudioContent(t *testing.T) {
	t.Parallel()

	model := mustAnthropicModel(t, "https://example.invalid")
	userMsg, err := message.NewUserMessage("Tony", []message.ContentBlock{
		message.NewDataBlock(message.NewBase64Source("abc", "audio/wav")),
	})
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}}); err == nil {
		t.Fatal("Call should reject audio content before sending request")
	}
}

func TestChatModelMapsProviderErrors(t *testing.T) {
	t.Parallel()

	server := newMessageServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(t, w, map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "rate_limit_error", "message": "rate limited"},
		})
	})
	defer server.Close()

	_, err := mustAnthropicModel(t, server.URL).Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{mustUserMessage(t, "hello")}})
	if err == nil {
		t.Fatal("Call should return provider error")
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error should expose ProviderError, got %T %v", err, err)
	}
	if providerErr.Provider != "anthropic" || providerErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("provider metadata mismatch: %#v", providerErr)
	}
}

func TestChatModelValidationAndTokenCount(t *testing.T) {
	t.Parallel()

	if _, err := asanthropic.NewChatModel(asanthropic.Credential{}, ""); err == nil {
		t.Fatal("missing credential and model should return error")
	}
	if _, err := asanthropic.NewChatModel(asanthropic.NewCredential("test-key"), "claude-sonnet-4-5", asanthropic.WithChatParameters(asanthropic.ChatParameters{MaxTokens: int64Ptr(0)})); err == nil {
		t.Fatal("invalid max tokens should return error")
	}
	if _, err := asanthropic.NewChatModel(asanthropic.NewCredential("test-key"), "claude-sonnet-4-5", asanthropic.WithChatParameters(asanthropic.ChatParameters{MaxTokens: int64Ptr(1024), ThinkingBudgetTokens: int64Ptr(1024)})); err == nil {
		t.Fatal("thinking budget must be smaller than max tokens")
	}
	if _, err := asanthropic.NewChatModel(asanthropic.NewCredential("test-key"), "claude-sonnet-4-5", asanthropic.WithMaxRetries(0)); err == nil {
		t.Fatal("max retries <= 0 should return error")
	}
	assertInvalidParameters(t)
	model := mustAnthropicModel(t, "https://example.invalid")
	if model.Name() != "anthropic:claude-sonnet-4-5" {
		t.Fatalf("unexpected model name: %q", model.Name())
	}
	count, err := model.CountTokens(asmodel.CallRequest{
		Messages: []*message.Message{mustUserMessage(t, "12345678")},
		Tools:    []asmodel.ToolSchema{readToolSchema()},
	})
	if err != nil {
		t.Fatalf("CountTokens returned error: %v", err)
	}
	if count <= 2 {
		t.Fatalf("count should include message and tool schema, got %d", count)
	}
	var nilModel *asanthropic.ChatModel
	if nilModel.Name() != "anthropic:<nil>" {
		t.Fatalf("nil model name mismatch: %q", nilModel.Name())
	}
	if _, err := nilModel.Call(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("nil model Call should return error")
	}
	if _, err := nilModel.Stream(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("nil model Stream should return error")
	}
}

func TestChatModelRejectsInvalidHistoryBlocks(t *testing.T) {
	t.Parallel()

	model := mustAnthropicModel(t, "https://example.invalid")
	thinkingMsg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{
		message.NewThinkingBlock("unsigned"),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	if _, callErr := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{thinkingMsg}}); callErr == nil {
		t.Fatal("unsigned thinking history should return error")
	}
	toolMsg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{
		message.NewToolCallBlock("toolu_1", "Read", `{"path"`),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{toolMsg}}); err == nil {
		t.Fatal("invalid tool input JSON should return error")
	}
}

func assertCallRequest(t *testing.T, body map[string]any) {
	t.Helper()
	if body["model"] != "claude-sonnet-4-5" || body["max_tokens"] != float64(4096) {
		t.Fatalf("model parameters not formatted: %#v", body)
	}
	assertSystemAndMessages(t, body)
	assertToolRequest(t, body)
	if body["temperature"] != 0.2 || body["top_p"] != 0.9 || body["top_k"] != float64(50) {
		t.Fatalf("sampling parameters not formatted: %#v", body)
	}
	thinking := body["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != float64(1024) || thinking["display"] != "summarized" {
		t.Fatalf("thinking parameters not formatted: %#v", thinking)
	}
}

func assertSystemAndMessages(t *testing.T, body map[string]any) {
	t.Helper()
	system := body["system"].([]any)
	if system[0].(map[string]any)["text"] != "You are helpful" {
		t.Fatalf("system prompt should be top-level: %#v", system)
	}
	messages := body["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["role"] != "user" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	content := messages[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["text"] != "Read README.md" {
		t.Fatalf("user text not formatted: %#v", content)
	}
}

func assertToolRequest(t *testing.T, body map[string]any) {
	t.Helper()
	tools := body["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "Read" {
		t.Fatalf("tools should be filtered to Read: %#v", tools)
	}
	if _, ok := tools[0].(map[string]any)["input_schema"]; !ok {
		t.Fatalf("Anthropic tool schema should use input_schema: %#v", tools[0])
	}
	choice := body["tool_choice"].(map[string]any)
	if choice["type"] != "tool" || choice["name"] != "Read" {
		t.Fatalf("forced tool choice not formatted: %#v", choice)
	}
}

func assertCallResponse(t *testing.T, resp *asmodel.ChatResponse) {
	t.Helper()
	if resp.ID != "msg_1" || !resp.IsLast {
		t.Fatalf("unexpected response envelope: %#v", resp)
	}
	thinking := resp.Content[0].(*message.ThinkingBlock)
	if thinking.Thinking != "checking" || thinking.Extra["signature"] != "sig-1" {
		t.Fatalf("thinking block not parsed: %#v", thinking)
	}
	if got := resp.Content[1].(*message.TextBlock).Text; got != "hello" {
		t.Fatalf("text block mismatch: %q", got)
	}
	call := resp.Content[2].(*message.ToolCallBlock)
	if call.ID != "toolu_1" || call.Name != "Read" || call.Input != `{"path":"README.md"}` {
		t.Fatalf("tool call block mismatch: %#v", call)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 5 || resp.Usage.CacheInputTokens != 2 {
		t.Fatalf("usage not parsed: %#v", resp.Usage)
	}
}

func newStreamingMessageServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newMessageServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if body["stream"] != true {
			t.Fatalf("stream request should set stream=true, got %#v", body["stream"])
		}
		writeStreamEvents(t, w)
	})
}

func newThinkingStreamServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newMessageServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		writeThinkingStreamEvents(t, w)
	})
}

func writeStreamEvents(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	writer := bufio.NewWriter(w)
	for _, event := range anthropicStreamEvents() {
		fmt.Fprintf(writer, "event: %s\n", event.name)
		fmt.Fprintf(writer, "data: %s\n\n", event.data)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
}

func writeThinkingStreamEvents(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	writer := bufio.NewWriter(w)
	for _, event := range anthropicThinkingStreamEvents() {
		fmt.Fprintf(writer, "event: %s\n", event.name)
		fmt.Fprintf(writer, "data: %s\n\n", event.data)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
}

func assertStreamResponses(t *testing.T, chunks []asmodel.ChatResponse) {
	t.Helper()
	if len(chunks) != 5 {
		t.Fatalf("unexpected stream chunk count: %d %#v", len(chunks), chunks)
	}
	if chunks[0].IsLast || chunks[0].Content[0].(*message.TextBlock).Text != "hel" {
		t.Fatalf("first delta mismatch: %#v", chunks[0])
	}
	final := chunks[len(chunks)-1]
	if !final.IsLast || final.ID != "msg_stream" {
		t.Fatalf("final stream response mismatch: %#v", final)
	}
	if final.Content[0].(*message.TextBlock).Text != "hello" {
		t.Fatalf("final text not accumulated: %#v", final.Content[0])
	}
	toolCall := final.Content[1].(*message.ToolCallBlock)
	if toolCall.ID != "toolu_1" || toolCall.Name != "Read" || toolCall.Input != `{"path":"README.md"}` {
		t.Fatalf("final tool call not accumulated: %#v", toolCall)
	}
	if final.Usage.InputTokens != 3 || final.Usage.OutputTokens != 2 || final.Usage.CacheInputTokens != 1 {
		t.Fatalf("final usage not attached: %#v", final.Usage)
	}
}

func assertThinkingStreamResponses(t *testing.T, chunks []asmodel.ChatResponse) {
	t.Helper()
	if len(chunks) != 2 {
		t.Fatalf("unexpected thinking stream chunk count: %d %#v", len(chunks), chunks)
	}
	if chunks[0].IsLast || chunks[0].Content[0].(*message.ThinkingBlock).Thinking != "plan" {
		t.Fatalf("thinking delta mismatch: %#v", chunks[0])
	}
	final := chunks[1]
	if !final.IsLast || final.ID != "msg_thinking" {
		t.Fatalf("thinking final response mismatch: %#v", final)
	}
	thinking := final.Content[0].(*message.ThinkingBlock)
	if thinking.Thinking != "plan" || thinking.Extra["signature"] != "sig-thinking" {
		t.Fatalf("thinking final block mismatch: %#v", thinking)
	}
}

func assertToolResultRequest(t *testing.T, body map[string]any) {
	t.Helper()
	messages := body["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["role"] != "user" {
		t.Fatalf("tool result should be sent as user message: %#v", messages)
	}
	result := messages[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if result["type"] != "tool_result" || result["tool_use_id"] != "toolu_1" {
		t.Fatalf("tool result metadata not formatted: %#v", result)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"]
	if text != "README contents" || result["is_error"] != false {
		t.Fatalf("tool result content not formatted: %#v", result)
	}
}

func assertAssistantHistoryRequest(t *testing.T, body map[string]any) {
	t.Helper()
	content := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["text"] != "hint" {
		t.Fatalf("hint block not formatted as text: %#v", content[0])
	}
	thinking := content[1].(map[string]any)
	if thinking["type"] != "thinking" || thinking["thinking"] != "checking" || thinking["signature"] != "sig-history" {
		t.Fatalf("thinking history not formatted: %#v", thinking)
	}
	toolUse := content[2].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["id"] != "toolu_1" || toolUse["name"] != "Read" {
		t.Fatalf("tool use history metadata mismatch: %#v", toolUse)
	}
	if toolUse["input"].(map[string]any)["path"] != "README.md" {
		t.Fatalf("tool use input not parsed: %#v", toolUse)
	}
}

func assertImageRequest(t *testing.T, body map[string]any) {
	t.Helper()
	content := body["messages"].([]any)[0].(map[string]any)["content"].([]any)
	base64Source := content[1].(map[string]any)["source"].(map[string]any)
	if base64Source["type"] != "base64" || base64Source["media_type"] != "image/png" || base64Source["data"] != "abc" {
		t.Fatalf("base64 image not formatted: %#v", content[1])
	}
	urlSource := content[2].(map[string]any)["source"].(map[string]any)
	if urlSource["type"] != "url" || urlSource["url"] != "https://example.com/image.png" {
		t.Fatalf("URL image not formatted: %#v", content[2])
	}
}

func callWithToolChoice(t *testing.T, mode string) map[string]any {
	t.Helper()
	requestCh := make(chan map[string]any, 1)
	server := newMessageServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, textOnlyResponse("ok"))
	})
	defer server.Close()
	model, err := asanthropic.NewChatModel(
		asanthropic.NewCredential("test-key", asanthropic.WithBaseURL(server.URL)),
		"claude-sonnet-4-5",
		asanthropic.WithStream(false),
		asanthropic.WithContextSize(2048),
		asanthropic.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{
		Messages:   []*message.Message{mustUserMessage(t, "hello")},
		Tools:      []asmodel.ToolSchema{readToolSchemaWithStringRequired()},
		ToolChoice: &types.ToolChoice{Mode: mode},
	}); err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	return <-requestCh
}

func assertInvalidParameters(t *testing.T) {
	t.Helper()
	tests := []struct {
		name       string
		parameters asanthropic.ChatParameters
	}{
		{name: "temperature", parameters: asanthropic.ChatParameters{Temperature: floatPtr(2)}},
		{name: "top p", parameters: asanthropic.ChatParameters{TopP: floatPtr(0)}},
		{name: "top k", parameters: asanthropic.ChatParameters{TopK: int64Ptr(0)}},
		{name: "thinking budget", parameters: asanthropic.ChatParameters{ThinkingBudgetTokens: int64Ptr(1000)}},
		{name: "thinking display", parameters: asanthropic.ChatParameters{ThinkingDisplay: "verbose"}},
	}
	for _, tt := range tests {
		if _, err := asanthropic.NewChatModel(
			asanthropic.NewCredential("test-key"),
			"claude-sonnet-4-5",
			asanthropic.WithChatParameters(tt.parameters),
		); err == nil {
			t.Fatalf("%s should return error", tt.name)
		}
	}
}

func newMessageServer(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		handler(w, r, body)
	}))
}

func anthropicMessageResponse() map[string]any {
	return map[string]any{
		"id":            "msg_1",
		"type":          "message",
		"role":          "assistant",
		"model":         "claude-sonnet-4-5",
		"stop_reason":   "tool_use",
		"stop_sequence": nil,
		"content": []any{
			map[string]any{"type": "thinking", "thinking": "checking", "signature": "sig-1"},
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": map[string]any{"path": "README.md"}},
		},
		"usage": map[string]any{
			"input_tokens":                7,
			"output_tokens":               5,
			"cache_creation_input_tokens": 1,
			"cache_read_input_tokens":     2,
		},
	}
}

func textOnlyResponse(text string) map[string]any {
	return map[string]any{
		"id":            "msg_text",
		"type":          "message",
		"role":          "assistant",
		"model":         "claude-sonnet-4-5",
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"content":       []any{map[string]any{"type": "text", "text": text}},
		"usage":         map[string]any{"input_tokens": 1, "output_tokens": 1},
	}
}

type streamEvent struct {
	name string
	data string
}

func anthropicStreamEvents() []streamEvent {
	return []streamEvent{
		{name: "message_start", data: `{"type":"message_start","message":{"id":"msg_stream","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":0}}}`},
		{name: "content_block_start", data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{name: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}`},
		{name: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`},
		{name: "content_block_stop", data: `{"type":"content_block_stop","index":0}`},
		{name: "content_block_start", data: `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}}`},
		{name: "content_block_delta", data: `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\""}}`},
		{name: "content_block_delta", data: `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":":\"README.md\"}"}}`},
		{name: "content_block_stop", data: `{"type":"content_block_stop","index":1}`},
		{name: "message_delta", data: `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":3,"output_tokens":2,"cache_read_input_tokens":1}}`},
		{name: "message_stop", data: `{"type":"message_stop"}`},
	}
}

func anthropicThinkingStreamEvents() []streamEvent {
	return []streamEvent{
		{name: "message_start", data: `{"type":"message_start","message":{"id":"msg_thinking","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":2,"output_tokens":0}}}`},
		{name: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}`},
		{name: "content_block_delta", data: `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-thinking"}}`},
		{name: "message_stop", data: `{"type":"message_stop"}`},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode response returned error: %v", err)
	}
}

func mustAnthropicModel(t *testing.T, baseURL string) *asanthropic.ChatModel {
	t.Helper()
	model, err := asanthropic.NewChatModel(
		asanthropic.NewCredential("test-key", asanthropic.WithBaseURL(baseURL)),
		"claude-sonnet-4-5",
		asanthropic.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	return model
}

func mustSystemMessage(t *testing.T, text string) *message.Message {
	t.Helper()
	msg, err := message.NewSystemMessage("system", text)
	if err != nil {
		t.Fatalf("NewSystemMessage returned error: %v", err)
	}
	return msg
}

func mustUserMessage(t *testing.T, text string) *message.Message {
	t.Helper()
	msg, err := message.NewUserMessage("Tony", text)
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	return msg
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

func readToolSchemaWithStringRequired() asmodel.ToolSchema {
	schema := readToolSchema()
	schema.Function.Parameters["required"] = []string{"path"}
	return schema
}

func writeToolSchema() asmodel.ToolSchema {
	return asmodel.ToolSchema{
		Type: "function",
		Function: asmodel.FunctionSchema{
			Name:        "Write",
			Description: "write files",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
