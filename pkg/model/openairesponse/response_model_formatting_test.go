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

package openairesponse

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

func TestResponseParameterCloneValidationAndModelOptions(t *testing.T) {
	t.Parallel()

	maxTokens := int64(64)
	temperature := 0.5
	store := true
	parallelTools := false
	params := ResponseParameters{
		MaxTokens:         &maxTokens,
		Temperature:       &temperature,
		Store:             &store,
		ParallelToolCalls: &parallelTools,
		ThinkingEnable:    true,
		ReasoningEffort:   "high",
	}
	cloned := params.Clone()
	maxTokens = 1
	temperature = 1.5
	store = false
	parallelTools = true
	if *cloned.MaxTokens != 64 || *cloned.Temperature != 0.5 || *cloned.Store != true || *cloned.ParallelToolCalls != false {
		t.Fatalf("parameters were not cloned: %#v", cloned)
	}
	if err := cloned.Validate(); err != nil {
		t.Fatalf("valid parameters returned error: %v", err)
	}
	if err := (ResponseParameters{MaxTokens: responseInt64Ptr(0)}).Validate(); err == nil {
		t.Fatal("expected invalid max tokens to fail")
	}
	if err := (ResponseParameters{Temperature: responseFloatPtr(-0.1)}).Validate(); err == nil {
		t.Fatal("expected low temperature to fail")
	}
	if err := (ResponseParameters{Temperature: responseFloatPtr(2.1)}).Validate(); err == nil {
		t.Fatal("expected high temperature to fail")
	}
	if err := (ResponseParameters{ReasoningEffort: "extreme"}).Validate(); err == nil {
		t.Fatal("expected unsupported reasoning effort to fail")
	}

	if _, err := NewResponseModel(NewCredential(""), "gpt-5.4"); err == nil {
		t.Fatal("expected invalid credential to fail")
	}
	if _, err := NewResponseModel(NewCredential("key"), ""); err == nil {
		t.Fatal("expected empty model to fail")
	}
	model, err := NewResponseModel(
		NewCredential("key", WithOrganization("org-1")),
		"gpt-5.4",
		WithResponseHTTPClient(nil),
		WithResponseContextSize(123),
		WithResponseParameters(params),
	)
	if err != nil {
		t.Fatalf("NewResponseModel returned error: %v", err)
	}
	if model.contextSize != 123 || model.credential.Organization != "org-1" {
		t.Fatalf("model options not applied: %#v", model)
	}
}

func TestNilResponseModelBranches(t *testing.T) {
	t.Parallel()

	var model *ResponseModel
	if got := model.Name(); got != "openai_response:<nil>" {
		t.Fatalf("nil model name mismatch: %q", got)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("expected nil Call to fail")
	}
	if _, err := model.Stream(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("expected nil Stream to fail")
	}
	if _, err := model.GenerateStructured(context.Background(), asmodel.StructuredOutputRequest{}); err == nil {
		t.Fatal("expected nil GenerateStructured to fail")
	}

	msg, err := message.NewUserMessage("user", "count me")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	tokens, err := model.CountTokens(asmodel.CallRequest{Messages: []*message.Message{msg}})
	if err != nil {
		t.Fatalf("CountTokens returned error: %v", err)
	}
	if tokens == 0 {
		t.Fatal("expected approximate token count for nil model")
	}
}

func TestResponseFormattingHelpersCoverHintsDataAndTools(t *testing.T) {
	t.Parallel()

	textHint := message.NewHintBlock("hint text")
	nestedHint := message.NewHintBlock(message.ContentBlockList{
		message.NewTextBlock("nested"),
		message.NewDataBlock(message.NewBase64Source("aW1hZ2U=", "image/png")),
	})
	content, items, err := formatResponseContent(message.ContentBlockList{
		message.NewTextBlock("hello"),
		textHint,
		nestedHint,
		message.NewThinkingBlock("hidden"),
		message.NewToolCallBlock("tool-1", "Read", `{"path":"README.md"}`, message.WithToolCallExtra("call_id", "call-provider-1")),
		message.NewToolResultBlock("tool-1", "Read", message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("tool output")}}),
	})
	if err != nil {
		t.Fatalf("formatResponseContent returned error: %v", err)
	}
	if len(content) != 4 {
		t.Fatalf("content count mismatch: %d %#v", len(content), content)
	}
	if len(items) != 2 {
		t.Fatalf("item count mismatch: %d %#v", len(items), items)
	}
	callItem := items[0].(map[string]any)
	if callItem["call_id"] != "call-provider-1" || callItem["arguments"] != `{"path":"README.md"}` {
		t.Fatalf("tool call item mismatch: %#v", callItem)
	}
	resultItem := items[1].(map[string]any)
	if resultItem["output"] != "tool output" {
		t.Fatalf("tool result item mismatch: %#v", resultItem)
	}

	input, err := formatResponseInput([]*message.Message{
		nil,
		{Role: message.RoleAssistant, Content: message.ContentBlockList{message.NewTextBlock("assistant")}},
	})
	if err != nil {
		t.Fatalf("formatResponseInput returned error: %v", err)
	}
	if len(input) != 1 {
		t.Fatalf("nil messages should be skipped: %#v", input)
	}

	if _, _, err := formatResponseContent(message.ContentBlockList{nil}); err == nil {
		t.Fatal("expected nil content block to fail")
	}
	if _, err := hintResponseContent(message.NewHintBlock(message.ContentBlockList{message.NewThinkingBlock("unsupported")})); err == nil {
		t.Fatal("expected unsupported hint block to fail")
	}
	if part, err := responseDataPart(nil); err != nil || part != nil {
		t.Fatalf("nil data part mismatch: part=%#v err=%v", part, err)
	}
	if _, err := responseDataPart(message.NewDataBlock(message.NewURLSource("https://example.com/audio.wav", "audio/wav"))); !isCapability(err, asmodel.ModelCapabilityAudio) {
		t.Fatalf("audio URL should be rejected with capability error, got %T %v", err, err)
	}
	if _, err := responseDataPart(message.NewDataBlock(message.NewBase64Source("YXVkaW8=", "audio/wav"))); !isCapability(err, asmodel.ModelCapabilityAudio) {
		t.Fatalf("audio base64 should be rejected with capability error, got %T %v", err, err)
	}
	if _, err := responseDataPart(message.NewDataBlock(message.NewURLSource("https://example.com/file.bin", "application/octet-stream"))); err == nil {
		t.Fatal("expected unsupported URL media type to fail")
	}
	if _, err := responseDataPart(message.NewDataBlock(message.NewBase64Source("ZGF0YQ==", "application/octet-stream"))); err == nil {
		t.Fatal("expected unsupported base64 media type to fail")
	}

	tools, choice, err := formatResponseTools([]asmodel.ToolSchema{responseToolSchema("Read"), responseToolSchema("Write")}, &types.ToolChoice{
		Mode:  string(types.ToolChoiceRequired),
		Tools: []string{"Read"},
	})
	if err != nil {
		t.Fatalf("formatResponseTools returned error: %v", err)
	}
	if len(tools) != 1 || choice != string(types.ToolChoiceRequired) {
		t.Fatalf("tool filtering mismatch: tools=%#v choice=%#v", tools, choice)
	}
	_, choice, err = formatResponseTools([]asmodel.ToolSchema{responseToolSchema("Read")}, &types.ToolChoice{Mode: "Read"})
	if err != nil {
		t.Fatalf("forced tool choice returned error: %v", err)
	}
	if choice.(map[string]any)["name"] != "Read" {
		t.Fatalf("forced tool choice mismatch: %#v", choice)
	}
	if _, _, err := formatResponseTools([]asmodel.ToolSchema{responseToolSchema("Read")}, &types.ToolChoice{Mode: "Missing"}); err == nil {
		t.Fatal("expected unavailable tool choice to fail")
	}
	if got := toolResultText(message.ToolResultOutput{Raw: "raw"}); got != "raw" {
		t.Fatalf("raw tool result mismatch: %q", got)
	}
	if got := toolResultText(message.ToolResultOutput{}); got != "" {
		t.Fatalf("empty tool result mismatch: %q", got)
	}
}

func TestResponseParsingStreamAndErrorHelpers(t *testing.T) {
	t.Parallel()

	payload := responsePayloadJSON{
		ID: "resp-1",
		Output: []responseOutputItem{
			{Type: "message", Content: []responseContentPart{{Type: "output_text", Text: "hello"}, {Type: "refusal", Text: "skip"}}},
			{ID: "fc-1", Type: "function_call", CallID: "provider-call", Name: "Read", Arguments: `{"path":"README.md"}`},
			{Type: "reasoning", Summary: []responseSummaryPart{{Type: "summary_text", Text: "thought"}, {Type: "summary_text"}}},
			{Type: "unknown"},
		},
		Usage: responseUsage{InputTokens: 3, OutputTokens: 4},
	}
	parsed := parseResponsePayload(payload, 2*time.Second)
	if parsed.ID != "resp-1" || parsed.Usage.Time != 2*time.Second || !parsed.HasContentBlocks("text", "tool_call", "thinking") {
		t.Fatalf("payload parse mismatch: %#v", parsed)
	}

	acc := newResponseStreamAccumulator(time.Now())
	if resp := acc.consumeEvent([]byte(`{"type":"response.output_item.added","item":{"id":"msg","type":"message"}}`)); resp != nil {
		t.Fatalf("non function output item should be ignored: %#v", resp)
	}
	if resp := acc.consumeEvent([]byte(`{"type":"response.function_call_arguments.delta","item_id":"late","delta":"{}"}`)); resp == nil || resp.Content[0].(*message.ToolCallBlock).ID != "late" {
		t.Fatalf("late function delta not accumulated: %#v", resp)
	}
	if resp := acc.consumeEvent([]byte(`{`)); resp == nil || resp.Error == nil || !resp.IsLast {
		t.Fatalf("invalid stream event should produce terminal error: %#v", resp)
	}
	if resp := acc.consumeEvent([]byte(`{"type":"unknown"}`)); resp != nil {
		t.Fatalf("unknown stream event should be ignored: %#v", resp)
	}

	out := make(chan asmodel.ChatResponse, 1)
	if !sendStreamResponse(context.Background(), out, nil) {
		t.Fatal("nil stream response should be treated as sent")
	}
	if !sendStreamResponse(context.Background(), out, parsed) {
		t.Fatal("buffered stream response should be sent")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sendStreamResponse(ctx, make(chan asmodel.ChatResponse), parsed) {
		t.Fatal("canceled context should prevent stream send")
	}

	streamErr := streamErrorResponse(errors.New("boom"))
	if !streamErr.IsLast || streamErr.Error == nil {
		t.Fatalf("stream error response mismatch: %#v", streamErr)
	}
	providerErr := normalizeResponseError(io.ErrUnexpectedEOF)
	var normalized *asmodel.ProviderError
	if !errors.As(providerErr, &normalized) || normalized.Provider != responseProviderName {
		t.Fatalf("normalizeResponseError mismatch: %#v", providerErr)
	}
}

func TestResponsePostAndStructuredErrorBranches(t *testing.T) {
	t.Parallel()

	model, err := NewResponseModel(NewCredential("key"), "gpt-5.4")
	if err != nil {
		t.Fatalf("NewResponseModel returned error: %v", err)
	}
	badResp, err := model.post(context.Background(), map[string]any{"bad": func() {}})
	if badResp != nil {
		_ = badResp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected JSON marshal error from post")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-empty","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	model, err = NewResponseModel(NewCredential("key", WithBaseURL(server.URL)), "gpt-5.4")
	if err != nil {
		t.Fatalf("NewResponseModel with server returned error: %v", err)
	}
	if _, err := model.GenerateStructured(context.Background(), asmodel.StructuredOutputRequest{}); err == nil {
		t.Fatal("expected missing schema to fail")
	}
	if _, err := model.GenerateStructured(context.Background(), asmodel.StructuredOutputRequest{
		Schema:      map[string]any{"type": "object"},
		CallRequest: asmodel.CallRequest{},
	}); err == nil || !strings.Contains(err.Error(), "did not contain text JSON") {
		t.Fatalf("expected empty structured text error, got %v", err)
	}

	errResp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Status:     "502 Bad Gateway",
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}
	err = responseError(errResp)
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != http.StatusBadGateway || providerErr.Message != "502 Bad Gateway" {
		t.Fatalf("responseError fallback mismatch: %#v err=%v", providerErr, err)
	}
}

func responseToolSchema(name string) asmodel.ToolSchema {
	return asmodel.ToolSchema{
		Type: "function",
		Function: asmodel.FunctionSchema{
			Name:        name,
			Description: "test tool",
			Parameters:  types.JSONSchema{"type": "object"},
		},
	}
}

func isCapability(err error, capability asmodel.ModelCapability) bool {
	var capabilityErr *asmodel.CapabilityError
	return errors.As(err, &capabilityErr) && capabilityErr.Capability == capability
}

func responseFloatPtr(value float64) *float64 {
	return &value
}

func responseInt64Ptr(value int64) *int64 {
	return &value
}
