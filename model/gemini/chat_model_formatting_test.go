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

package gemini

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestChatParameterValidationAndModelOptionBranches(t *testing.T) {
	t.Parallel()

	credential := NewCredential("key", WithBaseURL(" https://gemini.test/ "))
	if credential.BaseURL != "https://gemini.test" {
		t.Fatalf("base URL was not trimmed: %q", credential.BaseURL)
	}

	maxTokens := int32(64)
	thinkingBudget := int32(32)
	temperature := float32(0.4)
	topP := float32(0.8)
	params := ChatParameters{
		MaxTokens:      &maxTokens,
		ThinkingBudget: &thinkingBudget,
		Temperature:    &temperature,
		TopP:           &topP,
		ThinkingEnable: true,
	}
	cloned := params.Clone()
	maxTokens = 1
	thinkingBudget = 1
	temperature = 1.5
	topP = 0.1
	if *cloned.MaxTokens != 64 || *cloned.ThinkingBudget != 32 || *cloned.Temperature != 0.4 || *cloned.TopP != 0.8 {
		t.Fatalf("parameters were not cloned: %#v", cloned)
	}
	if err := cloned.Validate(); err != nil {
		t.Fatalf("valid parameters returned error: %v", err)
	}
	if err := (ChatParameters{MaxTokens: geminiInt32Ptr(0)}).Validate(); err == nil {
		t.Fatal("expected invalid max tokens to fail")
	}
	if err := (ChatParameters{ThinkingBudget: geminiInt32Ptr(0)}).Validate(); err == nil {
		t.Fatal("expected invalid thinking budget to fail")
	}
	if err := (ChatParameters{Temperature: geminiFloat32Ptr(-0.1)}).Validate(); err == nil {
		t.Fatal("expected low temperature to fail")
	}
	if err := (ChatParameters{Temperature: geminiFloat32Ptr(2.1)}).Validate(); err == nil {
		t.Fatal("expected high temperature to fail")
	}
	if err := (ChatParameters{TopP: geminiFloat32Ptr(0)}).Validate(); err == nil {
		t.Fatal("expected low top_p to fail")
	}
	if err := (ChatParameters{TopP: geminiFloat32Ptr(1.1)}).Validate(); err == nil {
		t.Fatal("expected high top_p to fail")
	}

	if _, err := NewChatModel(NewCredential(""), "gemini-2.5-flash"); err == nil {
		t.Fatal("expected empty API key to fail")
	}
	if _, err := NewChatModel(NewCredential("key"), ""); err == nil {
		t.Fatal("expected empty model to fail")
	}
	if _, err := NewChatModel(NewCredential("key"), "gemini-2.5-flash", WithChatParameters(ChatParameters{TopP: geminiFloat32Ptr(2)})); err == nil {
		t.Fatal("expected invalid parameters to fail")
	}
	model, err := NewChatModel(
		NewCredential("key"),
		"gemini-2.5-flash",
		WithContextSize(123),
		WithHTTPClient(http.DefaultClient),
		WithChatParameters(cloned),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	if model.contextSize != 123 || *model.parameters.MaxTokens != 64 || !model.parameters.ThinkingEnable {
		t.Fatalf("model options not applied: %#v", model)
	}
}

func TestNilChatModelBranches(t *testing.T) {
	t.Parallel()

	var model *ChatModel
	if got := model.Name(); got != "gemini:<nil>" {
		t.Fatalf("nil model name mismatch: %q", got)
	}
	if _, err := model.Call(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("expected nil Call to fail")
	}
	if _, err := model.Stream(context.Background(), asmodel.CallRequest{}); err == nil {
		t.Fatal("expected nil Stream to fail")
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
		t.Fatal("expected approximate token count")
	}
}

func TestGeminiFormattingHelpersCoverContentDataAndTools(t *testing.T) {
	t.Parallel()

	systemMsg, err := message.NewSystemMessage("system", "system")
	if err != nil {
		t.Fatalf("NewSystemMessage returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("user", message.ContentBlockList{message.NewTextBlock("user")})
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	assistantMsg, err := message.NewAssistantMessage("assistant", message.ContentBlockList{message.NewTextBlock("assistant")})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	contents, system, err := formatMessages([]*message.Message{nil, systemMsg, userMsg, assistantMsg})
	if err != nil {
		t.Fatalf("formatMessages returned error: %v", err)
	}
	if system == nil || len(contents) != 2 || contents[0].Role != genai.RoleUser || contents[1].Role != genai.RoleModel {
		t.Fatalf("formatted messages mismatch: system=%#v contents=%#v", system, contents)
	}
	if _, _, err := formatMessages([]*message.Message{{Role: message.Role("other")}}); err == nil {
		t.Fatal("expected unsupported role to fail")
	}

	parts, err := formatContentParts(message.ContentBlockList{
		message.NewTextBlock("text"),
		message.NewHintBlock("hint"),
		message.NewHintBlock(message.ContentBlockList{
			message.NewTextBlock("nested"),
			message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
		}),
		message.NewDataBlock(message.NewBase64Source("aW1hZ2U=", "image/png")),
		message.NewToolCallBlock("call-1", "Read", `{"path":"README.md"}`),
		message.NewToolCallBlock("call-2", "Read", "raw input"),
		message.NewToolResultBlock("call-1", "Read", message.ToolResultOutput{Raw: "result"}),
		message.NewThinkingBlock("hidden"),
	})
	if err != nil {
		t.Fatalf("formatContentParts returned error: %v", err)
	}
	if len(parts) != 8 {
		t.Fatalf("content part count mismatch: %d %#v", len(parts), parts)
	}
	if parts[5].FunctionCall.Args["path"] != "README.md" || parts[6].FunctionCall.Args["input"] != "raw input" {
		t.Fatalf("function call parts mismatch: %#v %#v", parts[5], parts[6])
	}
	if parts[7].FunctionResponse.Response["output"] != "result" {
		t.Fatalf("function response part mismatch: %#v", parts[7].FunctionResponse)
	}
	if _, err := formatContentParts(message.ContentBlockList{nil}); err == nil {
		t.Fatal("expected nil content block to fail")
	}
	if _, err := hintContentParts(message.NewHintBlock(message.ContentBlockList{message.NewThinkingBlock("unsupported")})); err == nil {
		t.Fatal("expected unsupported hint block to fail")
	}
	if part, err := dataPart(nil); err != nil || part != nil {
		t.Fatalf("nil data part mismatch: part=%#v err=%v", part, err)
	}
	if _, err := dataPart(message.NewDataBlock(message.NewBase64Source("bad", "image/png"))); err == nil {
		t.Fatal("expected invalid base64 to fail")
	}
	if _, err := dataPart(message.NewDataBlock(message.NewBase64Source("cGRm", "application/pdf"))); !geminiCapability(err) {
		t.Fatalf("unsupported base64 media should be CapabilityError, got %T %v", err, err)
	}
	if _, err := dataPart(message.NewDataBlock(message.NewURLSource("https://example.com/file.pdf", "application/pdf"))); !geminiCapability(err) {
		t.Fatalf("unsupported URL media should be CapabilityError, got %T %v", err, err)
	}

	if tools, config, err := formatTools(nil, nil); err != nil || tools != nil || config != nil {
		t.Fatalf("empty tools mismatch: tools=%#v config=%#v err=%v", tools, config, err)
	}
	tools, config, err := formatTools([]asmodel.ToolSchema{geminiToolSchema("Read"), geminiToolSchema("Write")}, &types.ToolChoice{
		Mode:  string(types.ToolChoiceRequired),
		Tools: []string{"Read"},
	})
	if err != nil {
		t.Fatalf("formatTools returned error: %v", err)
	}
	if len(tools) != 1 || len(tools[0].FunctionDeclarations) != 2 {
		t.Fatalf("tool declarations mismatch: %#v", tools)
	}
	if config.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeAny || len(config.FunctionCallingConfig.AllowedFunctionNames) != 1 || config.FunctionCallingConfig.AllowedFunctionNames[0] != "Read" {
		t.Fatalf("required tool config mismatch: %#v", config.FunctionCallingConfig)
	}
	_, config, err = formatTools([]asmodel.ToolSchema{geminiToolSchema("Read")}, &types.ToolChoice{Mode: string(types.ToolChoiceNone)})
	if err != nil || config.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeNone {
		t.Fatalf("none tool config mismatch: config=%#v err=%v", config, err)
	}
	_, config, err = formatTools([]asmodel.ToolSchema{geminiToolSchema("Read")}, &types.ToolChoice{Mode: "Read"})
	if err != nil || config.FunctionCallingConfig.AllowedFunctionNames[0] != "Read" {
		t.Fatalf("forced tool config mismatch: config=%#v err=%v", config, err)
	}
	if _, _, err := formatTools([]asmodel.ToolSchema{geminiToolSchema("Read")}, &types.ToolChoice{Mode: "Missing"}); err == nil {
		t.Fatal("expected unavailable tool choice to fail")
	}
}

func TestGeminiResponseStreamAndErrorHelpers(t *testing.T) {
	t.Parallel()

	nilResp := parseGenerateContentResponse(nil, false, time.Second)
	if nilResp.IsLast || len(nilResp.Content) != 0 {
		t.Fatalf("nil response mismatch: %#v", nilResp)
	}
	resp := parseGenerateContentResponse(&genai.GenerateContentResponse{
		ResponseID: "resp-1",
		Candidates: []*genai.Candidate{{
			Content: genai.NewContentFromParts([]*genai.Part{
				{Text: "thought", Thought: true},
				genai.NewPartFromText("answer"),
				{FunctionCall: &genai.FunctionCall{Name: "Read", Args: map[string]any{"path": "README.md"}}},
				{InlineData: &genai.Blob{Data: []byte("bytes"), MIMEType: "image/png"}},
				nil,
			}, genai.RoleModel),
		}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:        3,
			CandidatesTokenCount:    4,
			CachedContentTokenCount: 1,
		},
	}, true, 2*time.Second)
	if resp.ID != "resp-1" || !resp.IsLast || !resp.HasContentBlocks("thinking", "text", "tool_call", "data") {
		t.Fatalf("response parse mismatch: %#v", resp)
	}
	if resp.Usage.InputTokens != 3 || resp.Usage.OutputTokens != 4 || resp.Usage.CacheInputTokens != 1 || resp.Usage.Time != 2*time.Second {
		t.Fatalf("usage mismatch: %#v", resp.Usage)
	}
	call := resp.GetContentBlocks("tool_call")[0].(*message.ToolCallBlock)
	if call.ID != "gemini-call-Read" {
		t.Fatalf("fallback tool call id mismatch: %#v", call)
	}
	if got := usage(nil, time.Second); got != nil {
		t.Fatalf("nil usage should stay nil: %#v", got)
	}

	acc := newStreamAccumulator()
	acc.add(nil)
	acc.add(asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("hel")}, false, asmodel.WithChatResponseID("stream-1")))
	acc.add(&asmodel.ChatResponse{
		Content: message.ContentBlockList{message.NewTextBlock("lo"), message.NewToolCallBlock("call-1", "Read", "{}")},
		Usage:   &asmodel.ChatUsage{InputTokens: 1},
	})
	final := acc.final(3 * time.Second)
	if final.ID != "stream-1" || !final.IsLast {
		t.Fatalf("stream final envelope mismatch: %#v", final)
	}
	if text := final.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("stream text mismatch: %#v", final)
	}
	if final.Usage.InputTokens != 1 || final.Usage.Time != 3*time.Second {
		t.Fatalf("stream usage mismatch: %#v", final.Usage)
	}

	out := make(chan asmodel.ChatResponse, 1)
	if !sendResponse(context.Background(), out, nil) {
		t.Fatal("nil response should be treated as sent")
	}
	if !sendResponse(context.Background(), out, final) {
		t.Fatal("buffered response should be sent")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sendResponse(ctx, make(chan asmodel.ChatResponse), final) {
		t.Fatal("canceled context should prevent send")
	}
	streamErr := streamErrorResponse(errors.New("boom"))
	if !streamErr.IsLast || streamErr.Error == nil {
		t.Fatalf("stream error response mismatch: %#v", streamErr)
	}
	if got := toolResultText(message.ToolResultOutput{Raw: "raw"}); got != "raw" {
		t.Fatalf("raw tool result mismatch: %q", got)
	}
	if got := toolResultText(message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("block")}}); got != "block" {
		t.Fatalf("block tool result mismatch: %q", got)
	}
	if got := toolResultText(message.ToolResultOutput{}); got != "" {
		t.Fatalf("empty tool result mismatch: %q", got)
	}
	if err := normalizeError(context.Background(), nil); err != nil {
		t.Fatalf("nil error should stay nil: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := normalizeError(canceled, errors.New("provider")); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation should be preserved: %v", err)
	}
	var providerErr *asmodel.ProviderError
	if err := normalizeError(context.Background(), errors.New("plain")); !errors.As(err, &providerErr) || providerErr.Provider != providerName {
		t.Fatalf("generic error should be normalized, got %T %v", err, err)
	}
}

func geminiToolSchema(name string) asmodel.ToolSchema {
	return asmodel.ToolSchema{
		Type: "function",
		Function: asmodel.FunctionSchema{
			Name:        name,
			Description: "test tool",
			Parameters:  types.JSONSchema{"type": "object"},
		},
	}
}

func geminiCapability(err error) bool {
	var capabilityErr *asmodel.CapabilityError
	return errors.As(err, &capabilityErr) && capabilityErr.Capability == asmodel.ModelCapabilityGeneration
}

func geminiFloat32Ptr(value float32) *float32 {
	return &value
}

func geminiInt32Ptr(value int32) *int32 {
	return &value
}
