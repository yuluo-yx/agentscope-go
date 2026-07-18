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
	items, err := formatResponseMessage(&message.Message{
		Role: message.RoleAssistant,
		Content: message.ContentBlockList{
			message.NewTextBlock("hello"),
			textHint,
			nestedHint,
			message.NewThinkingBlock("hidden"),
			message.NewToolCallBlock("tool-1", "Read", `{"path":"README.md"}`, message.WithToolCallExtra("call_id", "call-provider-1")),
			message.NewToolResultBlock("tool-1", "Read", message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("tool output")}}),
		},
	})
	if err != nil {
		t.Fatalf("formatResponseMessage returned error: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("item count mismatch: %d %#v", len(items), items)
	}
	assistantMsg := items[0].(map[string]any)
	if assistantMsg["role"] != string(message.RoleAssistant) {
		t.Fatalf("assistant message mismatch: %#v", assistantMsg)
	}
	assistantContent := assistantMsg["content"].([]any)
	if len(assistantContent) != 1 || assistantContent[0].(map[string]any)["type"] != "output_text" {
		t.Fatalf("assistant content mismatch: %#v", assistantContent)
	}
	hintMsg := items[1].(map[string]any)
	if hintMsg["role"] != string(message.RoleUser) || len(hintMsg["content"].([]any)) != 1 {
		t.Fatalf("text hint should become a user message: %#v", hintMsg)
	}
	nestedHintMsg := items[2].(map[string]any)
	if nestedHintMsg["role"] != string(message.RoleUser) || len(nestedHintMsg["content"].([]any)) != 2 {
		t.Fatalf("nested hint should become a user message: %#v", nestedHintMsg)
	}
	callItem := items[3].(map[string]any)
	if callItem["call_id"] != "call-provider-1" || callItem["arguments"] != `{"path":"README.md"}` {
		t.Fatalf("tool call item mismatch: %#v", callItem)
	}
	resultItem := items[4].(map[string]any)
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

	if _, err := formatResponseMessage(&message.Message{Role: message.RoleUser, Content: message.ContentBlockList{nil}}); err == nil {
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

func TestResponseFormattingReasoningReplay(t *testing.T) {
	t.Parallel()

	newReasoning := func(id, thinking string) *message.ThinkingBlock {
		return message.NewThinkingBlock(thinking, message.WithExtra("reasoning_item_id", id))
	}

	t.Run("empty thinking echoed with reasoning item id", func(t *testing.T) {
		t.Parallel()
		msg, err := message.NewAssistantMessage("assistant", message.ContentBlockList{
			message.NewTextBlock("reply"),
			newReasoning("rs_empty", ""),
		})
		if err != nil {
			t.Fatalf("NewAssistantMessage returned error: %v", err)
		}
		input, err := formatResponseInput([]*message.Message{msg})
		if err != nil {
			t.Fatalf("formatResponseInput returned error: %v", err)
		}
		if len(input) != 2 {
			t.Fatalf("expected reasoning item before assistant text: %#v", input)
		}
		reasoning := input[0].(map[string]any)
		if reasoning["type"] != "reasoning" || reasoning["id"] != "rs_empty" {
			t.Fatalf("reasoning item mismatch: %#v", reasoning)
		}
		if summary := reasoning["summary"].([]any); len(summary) != 0 {
			t.Fatalf("empty thinking should produce empty summary: %#v", summary)
		}
		assistant := input[1].(map[string]any)
		if assistant["role"] != string(message.RoleAssistant) {
			t.Fatalf("assistant text should follow the empty reasoning item: %#v", assistant)
		}
	})

	t.Run("reasoning replay keeps tool boundaries", func(t *testing.T) {
		t.Parallel()
		msg, err := message.NewAssistantMessage("assistant", message.ContentBlockList{
			newReasoning("rs_1", "thinking_1"),
			message.NewTextBlock("text_1"),
			message.NewToolCallBlock("call_1", "func_1", `{"arg":"value1"}`),
			message.NewToolResultBlock("call_1", "func_1", message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("result_1")}}),
			newReasoning("rs_2", "thinking_2"),
			message.NewTextBlock("text_2"),
		})
		if err != nil {
			t.Fatalf("NewAssistantMessage returned error: %v", err)
		}
		input, err := formatResponseInput([]*message.Message{msg})
		if err != nil {
			t.Fatalf("formatResponseInput returned error: %v", err)
		}
		if len(input) != 6 {
			t.Fatalf("item count mismatch: %d %#v", len(input), input)
		}
		if got := input[0].(map[string]any)["id"]; got != "rs_1" {
			t.Fatalf("first item should be reasoning rs_1: %#v", input[0])
		}
		if summary := input[0].(map[string]any)["summary"].([]any); len(summary) != 1 || summary[0].(map[string]any)["text"] != "thinking_1" {
			t.Fatalf("reasoning summary mismatch: %#v", summary)
		}
		textItem := input[1].(map[string]any)
		if textItem["role"] != string(message.RoleAssistant) || textItem["content"].([]any)[0].(map[string]any)["type"] != "output_text" {
			t.Fatalf("assistant text item mismatch: %#v", textItem)
		}
		if call := input[2].(map[string]any); call["type"] != "function_call" || call["call_id"] != "call_1" {
			t.Fatalf("function call item mismatch: %#v", call)
		}
		if result := input[3].(map[string]any); result["type"] != "function_call_output" || result["output"] != "result_1" {
			t.Fatalf("function call output item mismatch: %#v", result)
		}
		if got := input[4].(map[string]any)["id"]; got != "rs_2" {
			t.Fatalf("second reasoning item mismatch: %#v", input[4])
		}
	})

	t.Run("non empty reasoning splits text segments", func(t *testing.T) {
		t.Parallel()
		msg, err := message.NewAssistantMessage("assistant", message.ContentBlockList{
			newReasoning("rs_1", "thinking_1"),
			message.NewTextBlock("text_1"),
			newReasoning("rs_2", "thinking_2"),
			message.NewTextBlock("text_2"),
		})
		if err != nil {
			t.Fatalf("NewAssistantMessage returned error: %v", err)
		}
		input, err := formatResponseInput([]*message.Message{msg})
		if err != nil {
			t.Fatalf("formatResponseInput returned error: %v", err)
		}
		if len(input) != 4 {
			t.Fatalf("expected reasoning/text alternation: %#v", input)
		}
		for i, want := range []string{"rs_1", "", "rs_2", ""} {
			item := input[i].(map[string]any)
			if want != "" {
				if item["type"] != "reasoning" || item["id"] != want {
					t.Fatalf("item %d should be reasoning %s: %#v", i, want, item)
				}
			} else if item["role"] != string(message.RoleAssistant) {
				t.Fatalf("item %d should be assistant text: %#v", i, item)
			}
		}
	})

	t.Run("thinking without reasoning item id is skipped", func(t *testing.T) {
		t.Parallel()
		msg, err := message.NewAssistantMessage("assistant", message.ContentBlockList{
			message.NewThinkingBlock("scratchpad"),
			message.NewTextBlock("answer"),
		})
		if err != nil {
			t.Fatalf("NewAssistantMessage returned error: %v", err)
		}
		input, err := formatResponseInput([]*message.Message{msg})
		if err != nil {
			t.Fatalf("formatResponseInput returned error: %v", err)
		}
		if len(input) != 1 {
			t.Fatalf("thinking without id must not be replayed: %#v", input)
		}
	})
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
