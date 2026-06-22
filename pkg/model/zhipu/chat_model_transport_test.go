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

package zhipu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestZhipuModelOptionAndNilBranches(t *testing.T) {
	t.Parallel()

	credential := NewCredential("key")
	if credential.BaseURL != defaultBaseURL {
		t.Fatalf("default base URL mismatch: %q", credential.BaseURL)
	}
	if _, err := NewChatModel(NewCredential("key"), "glm-5.1", WithMaxRetries(0)); err == nil {
		t.Fatal("expected invalid max retries to fail")
	}
	model, err := NewChatModel(
		NewCredential("key"),
		"glm-5.1",
		WithContextSize(123),
		WithHTTPClient(nil),
		WithStream(false),
		WithChatParameters(ChatParameters{ParallelToolCalls: zhipuBoolPtr(true)}),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	if model.contextSize != 123 || model.stream || model.httpClient == nil || model.parameters.ParallelToolCalls == nil || !*model.parameters.ParallelToolCalls {
		t.Fatalf("model options not applied: %#v", model)
	}

	var nilModel *ChatModel
	if got := nilModel.Name(); got != "zhipu:<nil>" {
		t.Fatalf("nil model name mismatch: %q", got)
	}
	if _, err := nilModel.Call(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("expected nil Call to fail")
	}
	if _, err := nilModel.Stream(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("expected nil Stream to fail")
	}
	if _, err := nilModel.CountTokens(asmodel.CallRequest{}); err == nil {
		t.Fatal("expected nil CountTokens to fail")
	}
}

func TestZhipuBuildRequestAndFormattingBranches(t *testing.T) {
	t.Parallel()

	model := &ChatModel{
		model: "glm-5.1",
		parameters: ChatParameters{
			MaxTokens:         zhipuInt64Ptr(32),
			Temperature:       zhipuFloatPtr(0.4),
			TopP:              zhipuFloatPtr(0.8),
			ThinkingEnable:    true,
			ParallelToolCalls: zhipuBoolPtr(false),
		},
	}
	assistant, err := message.NewAssistantMessage("assistant", message.ContentBlockList{
		message.NewTextBlock("before "),
		message.NewToolCallBlock("call-1", "Read", `{"path":"README.md"}`),
		message.NewToolResultBlock("call-1", "Read", message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("tool output")}}),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	body, err := model.buildRequest(asmodel.CallRequest{
		Messages: []*message.Message{
			nil,
			{Name: "named-user", Role: message.RoleUser, Content: message.ContentBlockList{message.NewTextBlock("hello")}},
			assistant,
		},
		Tools:      []asmodel.ToolSchema{zhipuToolSchema("Read"), zhipuToolSchema("Write")},
		ToolChoice: &types.ToolChoice{Mode: string(types.ToolChoiceRequired), Tools: []string{"Read"}},
	}, true)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	if body["model"] != "glm-5.1" || body["stream"] != true || body["max_tokens"] != int64(32) {
		t.Fatalf("body envelope mismatch: %#v", body)
	}
	if body["temperature"] != 0.4 || body["top_p"] != 0.8 || body["parallel_tool_calls"] != false {
		t.Fatalf("parameter body mismatch: %#v", body)
	}
	messages := body["messages"].([]map[string]any)
	if len(messages) != 3 {
		t.Fatalf("messages count mismatch: %#v", messages)
	}
	if messages[0]["name"] != "named-user" || messages[1]["role"] != "assistant" || messages[2]["role"] != "tool" {
		t.Fatalf("messages were not formatted: %#v", messages)
	}
	if body["tool_choice"] != string(types.ToolChoiceRequired) {
		t.Fatalf("required tool choice mismatch: %#v", body["tool_choice"])
	}

	if _, err := zhipuMessages([]*message.Message{{Role: message.Role("other")}}); err == nil {
		t.Fatal("expected unsupported role to fail")
	}
	if _, _, _, err := splitZhipuContent(message.ContentBlockList{nil}); err == nil {
		t.Fatal("expected unsupported content block to fail")
	}
	if msg := zhipuAssistantMessage("text", nil); msg["tool_calls"] != nil || msg["content"] != "text" {
		t.Fatalf("assistant message without tools mismatch: %#v", msg)
	}

	tools, choice, err := zhipuTools([]asmodel.ToolSchema{zhipuToolSchema("Read"), zhipuToolSchema("Write")}, &types.ToolChoice{
		Mode:  "Read",
		Tools: []string{"Read"},
	})
	if err != nil {
		t.Fatalf("zhipuTools returned error: %v", err)
	}
	if len(tools) != 1 || choice.(map[string]any)["function"].(map[string]any)["name"] != "Read" {
		t.Fatalf("forced tool choice mismatch: tools=%#v choice=%#v", tools, choice)
	}
	if _, _, err := zhipuTools([]asmodel.ToolSchema{zhipuToolSchema("Read")}, &types.ToolChoice{Mode: "Missing"}); err == nil {
		t.Fatal("expected unavailable tool choice to fail")
	}
	if got := toolResultText(message.ToolResultOutput{Raw: "raw"}); got != "raw" {
		t.Fatalf("raw tool result mismatch: %q", got)
	}
	if got := toolResultText(message.ToolResultOutput{}); got != "" {
		t.Fatalf("empty tool result mismatch: %q", got)
	}
}

func TestZhipuPostRetryAndErrorBranches(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "retry", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok","choices":[]}`))
	}))
	defer server.Close()

	model := &ChatModel{
		credential: NewCredential("key", WithBaseURL(server.URL)),
		maxRetries: 2,
		httpClient: server.Client(),
	}
	resp, err := model.post(context.Background(), map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("post retry returned error: %v", err)
	}
	defer resp.Body.Close()
	if attempts != 2 || resp.StatusCode != http.StatusOK {
		t.Fatalf("retry attempts mismatch: attempts=%d status=%d", attempts, resp.StatusCode)
	}
	badResp, err := model.post(context.Background(), map[string]any{"bad": func() {}})
	if badResp != nil {
		_ = badResp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected JSON marshal error")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	canceledResp, err := model.post(canceled, map[string]any{"ok": true})
	if canceledResp != nil {
		_ = canceledResp.Body.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context should be preserved: %v", err)
	}

	networkModel := &ChatModel{
		credential: NewCredential("key", WithBaseURL(server.URL)),
		maxRetries: 1,
		httpClient: &http.Client{Transport: zhipuRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})},
	}
	networkResp, err := networkModel.post(context.Background(), map[string]any{"ok": true})
	if networkResp != nil {
		_ = networkResp.Body.Close()
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Provider != providerName {
		t.Fatalf("network error should be normalized: %#v err=%v", providerErr, err)
	}
}

func TestZhipuResponseStreamAndMetadataHelpers(t *testing.T) {
	t.Parallel()

	completion := zhipuCompletion{
		ID: "chat-1",
		Choices: []zhipuChoice{{Message: zhipuMessage{
			ReasoningContent: "plan",
			Content:          "answer",
			ToolCalls: []zhipuToolCall{{
				ID: "call-1",
				Function: zhipuToolFunction{
					Name:      "Read",
					Arguments: `{}`,
				},
			}},
		}}},
		Usage: zhipuUsageData{PromptTokens: 2, CompletionTokens: 3},
	}
	chatResp := completion.chatResponse(time.Second)
	if chatResp.ID != "chat-1" || !chatResp.HasContentBlocks("thinking", "text", "tool_call") {
		t.Fatalf("chat response mismatch: %#v", chatResp)
	}
	if usage := (zhipuUsageData{}).chatUsage(time.Second); usage != nil {
		t.Fatalf("zero usage should be nil: %#v", usage)
	}

	acc := newZhipuStreamAccumulator(time.Now())
	if resp := acc.consume(zhipuCompletion{}); resp != nil {
		t.Fatalf("empty stream chunk should be ignored: %#v", resp)
	}
	resp := acc.consume(zhipuCompletion{
		ID: "stream-1",
		Choices: []zhipuChoice{{Delta: zhipuMessage{
			ReasoningContent: "pl",
			Content:          "he",
			ToolCalls: []zhipuToolCall{{
				Index: 1,
				ID:    "call-1",
				Function: zhipuToolFunction{
					Name:      "Read",
					Arguments: `{"path"`,
				},
			}},
		}}},
		Usage: zhipuUsageData{PromptTokens: 1, CompletionTokens: 1},
	})
	if resp == nil || resp.IsLast || !resp.HasContentBlocks("thinking", "text", "tool_call") {
		t.Fatalf("stream delta mismatch: %#v", resp)
	}
	acc.consume(zhipuCompletion{Choices: []zhipuChoice{{Delta: zhipuMessage{ToolCalls: []zhipuToolCall{{
		Index:    1,
		Function: zhipuToolFunction{Arguments: `:"README.md"}`},
	}}}}}})
	final := acc.finalResponse()
	if !final.IsLast || final.ID != "stream-1" {
		t.Fatalf("stream final envelope mismatch: %#v", final)
	}
	if text := final.GetTextContent(""); text == nil || *text != "he" {
		t.Fatalf("stream final text mismatch: %#v", final)
	}
	if final.Content[2].(*message.ToolCallBlock).Input != `{"path":"README.md"}` {
		t.Fatalf("stream final tool call mismatch: %#v", final.Content[2])
	}

	out := make(chan asmodel.ChatResponse, 1)
	if !sendStreamResponse(context.Background(), out, nil) {
		t.Fatal("nil stream response should be treated as sent")
	}
	if !sendStreamResponse(context.Background(), out, final) {
		t.Fatal("buffered stream response should be sent")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if sendStreamResponse(canceled, make(chan asmodel.ChatResponse), final) {
		t.Fatal("canceled context should prevent send")
	}
	streamErr := streamErrorResponse(errors.New("boom"))
	if !streamErr.IsLast || streamErr.Error == nil {
		t.Fatalf("stream error response mismatch: %#v", streamErr)
	}

	errorOut := make(chan asmodel.ChatResponse, 1)
	parseZhipuStream(context.Background(), strings.NewReader("data: {\n\n"), time.Now(), errorOut)
	if chunk := <-errorOut; chunk.Error == nil || !chunk.IsLast {
		t.Fatalf("invalid stream JSON should emit terminal error: %#v", chunk)
	}
	scannerErrOut := make(chan asmodel.ChatResponse, 1)
	parseZhipuStream(context.Background(), zhipuErrorReader{}, time.Now(), scannerErrOut)
	if chunk := <-scannerErrOut; chunk.Error == nil || !chunk.IsLast {
		t.Fatalf("scanner error should emit terminal error: %#v", chunk)
	}

	rawErr := normalizeHTTPError(&http.Response{
		StatusCode: http.StatusBadGateway,
		Status:     "502 Bad Gateway",
		Body:       io.NopCloser(strings.NewReader("bad gateway")),
	})
	var providerErr *asmodel.ProviderError
	if !errors.As(rawErr, &providerErr) || providerErr.StatusCode != http.StatusBadGateway || providerErr.Message != "bad gateway" {
		t.Fatalf("raw HTTP error mismatch: %#v err=%v", providerErr, rawErr)
	}
	statusErr := normalizeHTTPError(&http.Response{
		StatusCode: http.StatusBadGateway,
		Status:     "502 Bad Gateway",
		Body:       io.NopCloser(strings.NewReader("")),
	})
	if !errors.As(statusErr, &providerErr) || providerErr.Message != "502 Bad Gateway" {
		t.Fatalf("status fallback mismatch: %#v err=%v", providerErr, statusErr)
	}

	cards, err := ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 || cards[0].Extra["provider"] != providerName {
		t.Fatalf("embedded model cards not loaded: %#v", cards)
	}
}

func zhipuToolSchema(name string) asmodel.ToolSchema {
	return asmodel.ToolSchema{
		Type: "function",
		Function: asmodel.FunctionSchema{
			Name:        name,
			Description: "test tool",
			Parameters:  types.JSONSchema{"type": "object"},
		},
	}
}

type zhipuRoundTripper func(*http.Request) (*http.Response, error)

func (fn zhipuRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type zhipuErrorReader struct{}

func (zhipuErrorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func zhipuBoolPtr(value bool) *bool {
	return &value
}

func zhipuFloatPtr(value float64) *float64 {
	return &value
}

func zhipuInt64Ptr(value int64) *int64 {
	return &value
}
