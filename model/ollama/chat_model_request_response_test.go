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

package ollama

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"testing"
	"time"

	ollamaapi "github.com/ollama/ollama/api"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestCredentialOptionsAndChatModelValidation(t *testing.T) {
	t.Parallel()

	credential := NewCredential(WithHost(" http://localhost:11434/ "))
	if credential.Host != "http://localhost:11434" {
		t.Fatalf("host was not trimmed: %q", credential.Host)
	}
	if _, err := NewChatModel(credential, ""); err == nil {
		t.Fatal("expected empty model name to fail")
	}
	if _, err := NewChatModel(credential, "llama", WithChatParameters(ChatParameters{MaxTokens: ollamaIntPtr(0)})); err == nil {
		t.Fatal("expected invalid max tokens to fail")
	}
	if _, err := NewChatModel(credential, "llama", WithChatParameters(ChatParameters{Temperature: ollamaFloatPtr(2.1)})); err == nil {
		t.Fatal("expected invalid temperature to fail")
	}
	if _, err := NewChatModel(Credential{Host: "not-a-url"}, "llama"); err == nil {
		t.Fatal("expected invalid host to fail")
	}

	maxTokens := 32
	temperature := 0.4
	model, err := NewChatModel(
		credential,
		"llama",
		WithStream(false),
		WithContextSize(1234),
		WithChatParameters(ChatParameters{MaxTokens: &maxTokens, Temperature: &temperature, ThinkingEnable: true}),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	maxTokens = 1
	temperature = 1.9
	if model.stream || model.contextSize != 1234 {
		t.Fatalf("options not applied: stream=%v context=%d", model.stream, model.contextSize)
	}
	if *model.parameters.MaxTokens != 32 || *model.parameters.Temperature != 0.4 || !model.parameters.ThinkingEnable {
		t.Fatalf("parameters were not cloned: %#v", model.parameters)
	}
}

func TestNilChatModelBranches(t *testing.T) {
	t.Parallel()

	var model *ChatModel
	if got := model.Name(); got != "ollama:<nil>" {
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
		t.Fatal("expected approximate token count for text input")
	}
}

func TestBuildRequestAndMessageFormattingBranches(t *testing.T) {
	t.Parallel()

	imagePayload := base64.StdEncoding.EncodeToString([]byte("fake-png"))
	userMsg, err := message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("hello "),
		message.NewDataBlock(message.NewBase64Source(imagePayload, "image/png")),
		message.NewDataBlock(message.NewBase64Source(base64.StdEncoding.EncodeToString([]byte("not image")), "text/plain")),
		message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
	})
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	assistantMsg, err := message.NewAssistantMessage("assistant", message.ContentBlockList{
		message.NewThinkingBlock("plan "),
		message.NewTextBlock("done"),
		message.NewToolCallBlock("call-1", "Read", `{"b":2,"a":"x"}`),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	toolMsg, err := message.NewAssistantMessage("assistant", message.ContentBlockList{
		message.NewToolResultBlock("call-1", "Read", message.ToolResultOutput{
			Blocks: message.ContentBlockList{message.NewTextBlock("tool text")},
		}, message.ToolResultSuccess),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}

	model := &ChatModel{
		model: "llama",
		parameters: ChatParameters{
			MaxTokens:      ollamaIntPtr(48),
			Temperature:    ollamaFloatPtr(0.2),
			ThinkingEnable: true,
		},
	}
	request, err := model.buildRequest(asmodel.CallRequest{
		Messages: []*message.Message{nil, userMsg, assistantMsg, toolMsg},
		Tools: []asmodel.ToolSchema{
			ollamaToolSchema("Read"),
			ollamaToolSchema("Write"),
		},
		ToolChoice: &types.ToolChoice{Mode: "Read", Tools: []string{"Read"}},
	}, true)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}
	if request.Model != "llama" || request.Stream == nil || !*request.Stream {
		t.Fatalf("request envelope mismatch: %#v", request)
	}
	if request.Options["num_predict"] != 48 || request.Options["temperature"] != 0.2 {
		t.Fatalf("options mismatch: %#v", request.Options)
	}
	if request.Think == nil || request.Think.Value != true {
		t.Fatalf("thinking option not enabled: %#v", request.Think)
	}
	if len(request.Tools) != 1 || request.Tools[0].Function.Name != "Read" {
		t.Fatalf("tool choice did not filter tools: %#v", request.Tools)
	}
	if len(request.Messages) != 3 {
		t.Fatalf("formatted message count mismatch: %d %#v", len(request.Messages), request.Messages)
	}
	if request.Messages[0].Role != "user" || request.Messages[0].Content != "hello " || len(request.Messages[0].Images) != 1 {
		t.Fatalf("user message not formatted: %#v", request.Messages[0])
	}
	if request.Messages[1].Content != "plan done" || len(request.Messages[1].ToolCalls) != 1 {
		t.Fatalf("assistant message not formatted: %#v", request.Messages[1])
	}
	if got, ok := request.Messages[1].ToolCalls[0].Function.Arguments.Get("a"); !ok || got != "x" {
		t.Fatalf("tool call arguments not preserved: %#v", request.Messages[1].ToolCalls[0].Function.Arguments.ToMap())
	}
	if request.Messages[2].Role != "tool" || request.Messages[2].Content != "tool text" || request.Messages[2].ToolCallID != "call-1" {
		t.Fatalf("tool result not formatted: %#v", request.Messages[2])
	}
}

func TestFormatHelpersErrorAndEmptyBranches(t *testing.T) {
	t.Parallel()

	image, ok, err := imageData(nil)
	if err != nil || ok || image != nil {
		t.Fatalf("nil image data mismatch: image=%#v ok=%v err=%v", image, ok, err)
	}
	image, ok, err = imageData(message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")))
	if err != nil || ok || image != nil {
		t.Fatalf("url image should be ignored: image=%#v ok=%v err=%v", image, ok, err)
	}
	if _, _, err := imageData(message.NewDataBlock(message.NewBase64Source("not-base64", "image/png"))); err == nil {
		t.Fatal("expected invalid base64 image to fail")
	}

	if _, err := formatToolCall(message.NewToolCallBlock("bad", "Read", "{")); err == nil {
		t.Fatal("expected invalid tool call JSON to fail")
	}
	call, err := formatToolCall(message.NewToolCallBlock("empty", "Read", " "))
	if err != nil {
		t.Fatalf("empty tool call input returned error: %v", err)
	}
	if call.Function.Arguments.Len() != 0 {
		t.Fatalf("expected empty arguments, got %#v", call.Function.Arguments.ToMap())
	}

	if tools, err := formatTools(nil, nil); err != nil || tools != nil {
		t.Fatalf("empty tools mismatch: tools=%#v err=%v", tools, err)
	}
	if _, err := formatTools([]asmodel.ToolSchema{ollamaToolSchema("Read")}, &types.ToolChoice{Mode: "Missing"}); err == nil {
		t.Fatal("expected unavailable forced tool to fail")
	}
	if got := toolResultText(message.ToolResultOutput{Raw: "raw"}); got != "raw" {
		t.Fatalf("raw tool result mismatch: %q", got)
	}
	if got := toolResultText(message.ToolResultOutput{}); got != "" {
		t.Fatalf("empty tool result mismatch: %q", got)
	}
}

func TestResponseStreamAndErrorHelpers(t *testing.T) {
	t.Parallel()

	arguments := ollamaapi.NewToolCallFunctionArguments()
	arguments.Set("path", "README.md")
	createdAt := time.Date(2026, 6, 9, 10, 11, 12, 13, time.UTC)
	response := ollamaapi.ChatResponse{
		CreatedAt: createdAt,
		Message: ollamaapi.Message{
			Thinking: "think",
			Content:  "answer",
			ToolCalls: []ollamaapi.ToolCall{{
				ID: "call-1",
				Function: ollamaapi.ToolCallFunction{
					Name:      "Read",
					Arguments: arguments,
				},
			}},
		},
		Metrics: ollamaapi.Metrics{
			PromptEvalCount: 3,
			EvalCount:       5,
			TotalDuration:   2 * time.Second,
		},
	}

	chatResponse := ollamaResponse(response, true)
	if !chatResponse.IsLast || chatResponse.CreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("response envelope mismatch: %#v", chatResponse)
	}
	if !chatResponse.HasContentBlocks("thinking", "text", "tool_call") {
		t.Fatalf("response content blocks missing: %#v", chatResponse.Content)
	}
	if chatResponse.Usage.InputTokens != 3 || chatResponse.Usage.OutputTokens != 5 || chatResponse.Usage.Time != 2*time.Second {
		t.Fatalf("usage mismatch: %#v", chatResponse.Usage)
	}
	if usage := chatUsage(ollamaapi.ChatResponse{}); usage != nil {
		t.Fatalf("zero usage should be nil: %#v", usage)
	}

	accumulator := &streamAccumulator{}
	accumulator.add(ollamaapi.ChatResponse{Message: ollamaapi.Message{Content: "hel"}})
	accumulator.add(ollamaapi.ChatResponse{
		Message: ollamaapi.Message{Content: "lo"},
		Done:    true,
		Metrics: ollamaapi.Metrics{PromptEvalCount: 1, EvalCount: 2},
	})
	final := accumulator.final()
	if text := final.GetTextContent(""); text == nil || *text != "hello" || !final.IsLast {
		t.Fatalf("stream final mismatch: %#v", final)
	}
	if final.Usage.InputTokens != 1 || final.Usage.OutputTokens != 2 {
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
	assertProviderStatus(t, normalizeError(ollamaapi.StatusError{StatusCode: http.StatusTooManyRequests, Status: "429"}), http.StatusTooManyRequests)
	assertProviderStatus(t, normalizeError(ollamaapi.AuthorizationError{StatusCode: http.StatusUnauthorized, Status: "401"}), http.StatusUnauthorized)
	assertProviderStatus(t, normalizeError(errors.New("plain")), 0)
}

func TestListModelsLoadsEmbeddedCards(t *testing.T) {
	t.Parallel()

	cards, err := ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("expected embedded ollama model cards")
	}
	for _, card := range cards {
		if card.Extra["provider"] != providerName {
			t.Fatalf("provider default not applied to card %#v", card)
		}
		if !card.Capabilities[asmodel.ModelCapabilityTools] || !card.Capabilities[asmodel.ModelCapabilityGeneration] {
			t.Fatalf("capability defaults not applied to card %#v", card)
		}
	}
}

func ollamaToolSchema(name string) asmodel.ToolSchema {
	return asmodel.ToolSchema{
		Type: "function",
		Function: asmodel.FunctionSchema{
			Name:        name,
			Description: "test tool",
			Parameters:  types.JSONSchema{"type": "object"},
		},
	}
}

func assertProviderStatus(t *testing.T, err error, want int) {
	t.Helper()

	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected provider error, got %T %v", err, err)
	}
	if providerErr.Provider != providerName || providerErr.StatusCode != want {
		t.Fatalf("provider error mismatch: %#v", providerErr)
	}
}

func ollamaFloatPtr(value float64) *float64 {
	return &value
}

func ollamaIntPtr(value int) *int {
	return &value
}
