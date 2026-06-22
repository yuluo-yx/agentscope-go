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

package openairesponse_test

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

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	asopenairesponse "github.com/yuluo-yx/agentscope-go/pkg/model/openairesponse"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestResponseModelCallFormatsRequestAndParsesResponse(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newResponseServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, responsePayload("resp-1", []any{
			map[string]any{
				"id":     "msg-1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []any{
					map[string]any{"type": "output_text", "text": "hello"},
				},
			},
			map[string]any{
				"id":        "fc-1",
				"type":      "function_call",
				"call_id":   "call-1",
				"name":      "Read",
				"arguments": `{"path":"README.md"}`,
			},
		}))
	})
	defer server.Close()

	maxTokens := int64(64)
	model, err := asopenairesponse.NewResponseModel(
		asopenairesponse.NewCredential("test-key", asopenairesponse.WithBaseURL(server.URL)),
		"gpt-5.4",
		asopenairesponse.WithResponseParameters(asopenairesponse.ResponseParameters{
			MaxTokens:       &maxTokens,
			ThinkingEnable:  true,
			ReasoningEffort: "medium",
			Temperature:     floatPtr(0.4),
		}),
		asopenairesponse.WithResponseContextSize(200000),
	)
	if err != nil {
		t.Fatalf("NewResponseModel returned error: %v", err)
	}

	systemMsg, err := message.NewSystemMessage("system", "You are concise.")
	if err != nil {
		t.Fatalf("NewSystemMessage returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", []message.ContentBlock{
		message.NewTextBlock("Read README"),
		message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
	})
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	resp, err := model.Call(context.Background(), asmodel.CallRequest{
		Messages: []*message.Message{systemMsg, userMsg},
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
	if body["model"] != "gpt-5.4" || body["stream"] != false || body["max_output_tokens"] != float64(64) {
		t.Fatalf("Responses request envelope mismatch: %#v", body)
	}
	if body["temperature"] != 0.4 || body["reasoning"].(map[string]any)["effort"] != "medium" {
		t.Fatalf("Responses parameters not formatted: %#v", body)
	}
	input := body["input"].([]any)
	if input[0].(map[string]any)["role"] != "system" || input[1].(map[string]any)["role"] != "user" {
		t.Fatalf("Responses input messages not formatted: %#v", input)
	}
	userContent := input[1].(map[string]any)["content"].([]any)
	if userContent[0].(map[string]any)["type"] != "input_text" || userContent[1].(map[string]any)["type"] != "input_image" {
		t.Fatalf("Responses multimodal content not formatted: %#v", userContent)
	}
	tools := body["tools"].([]any)
	if tools[0].(map[string]any)["type"] != "function" || tools[0].(map[string]any)["name"] != "Read" {
		t.Fatalf("Responses tool schema mismatch: %#v", tools)
	}
	toolChoice := body["tool_choice"].(map[string]any)
	if toolChoice["type"] != "function" || toolChoice["name"] != "Read" {
		t.Fatalf("Responses forced tool choice mismatch: %#v", toolChoice)
	}
	if model.Name() != "openai_response:gpt-5.4" {
		t.Fatalf("Responses model should be distinct from Chat Completions: %q", model.Name())
	}
	if text := resp.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("Responses text not parsed: %#v", resp)
	}
	call := resp.Content[1].(*message.ToolCallBlock)
	if call.ID != "fc-1" || call.Name != "Read" || call.Input != `{"path":"README.md"}` || call.Extra["call_id"] != "call-1" {
		t.Fatalf("Responses function call not parsed: %#v", call)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 7 {
		t.Fatalf("Responses usage not parsed: %#v", resp.Usage)
	}
}

func TestResponseModelStreamAccumulatesEvents(t *testing.T) {
	t.Parallel()

	server := newResponseServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if body["stream"] != true {
			t.Fatalf("Stream should set stream=true: %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writer := bufio.NewWriter(w)
		events := []string{
			`event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"hel"}`,
			`event: response.output_item.added
data: {"type":"response.output_item.added","item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"Read","arguments":""}}`,
			`event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","item_id":"fc-1","delta":"{\"path\""}`,
			`event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"lo"}`,
			`event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","item_id":"fc-1","delta":":\"README.md\"}"}`,
			`event: response.completed
data: {"type":"response.completed","response":{"id":"resp-stream","output":[{"id":"msg-1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},{"id":"fc-1","type":"function_call","call_id":"call-1","name":"Read","arguments":"{\"path\":\"README.md\"}"}],"usage":{"input_tokens":3,"output_tokens":4}}}`,
		}
		for _, event := range events {
			fmt.Fprint(writer, event+"\n\n")
		}
		if err := writer.Flush(); err != nil {
			t.Fatalf("Flush returned error: %v", err)
		}
	})
	defer server.Close()

	model, err := asopenairesponse.NewResponseModel(
		asopenairesponse.NewCredential("test-key", asopenairesponse.WithBaseURL(server.URL)),
		"gpt-5.4",
	)
	if err != nil {
		t.Fatalf("NewResponseModel returned error: %v", err)
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
	if len(chunks) != 6 {
		t.Fatalf("unexpected Responses stream chunks: %d %#v", len(chunks), chunks)
	}
	if text := chunks[0].GetTextContent(""); chunks[0].IsLast || text == nil || *text != "hel" {
		t.Fatalf("first delta mismatch: %#v", chunks[0])
	}
	if chunks[2].Content[0].(*message.ToolCallBlock).Input != `{"path"` {
		t.Fatalf("function call delta mismatch: %#v", chunks[2])
	}
	final := chunks[len(chunks)-1]
	if !final.IsLast || final.ID != "resp-stream" {
		t.Fatalf("final Responses chunk mismatch: %#v", final)
	}
	if text := final.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("final text mismatch: %#v", final)
	}
	if final.Usage.InputTokens != 3 || final.Usage.OutputTokens != 4 {
		t.Fatalf("final usage mismatch: %#v", final.Usage)
	}
}

func TestResponseModelMapsErrorsContextAndCapabilityRejection(t *testing.T) {
	t.Parallel()

	server := newResponseServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(t, w, map[string]any{"error": map[string]any{"message": "rate limited", "code": "rate_limit"}})
	})
	defer server.Close()
	model, err := asopenairesponse.NewResponseModel(
		asopenairesponse.NewCredential("test-key", asopenairesponse.WithBaseURL(server.URL)),
		"gpt-5.4",
	)
	if err != nil {
		t.Fatalf("NewResponseModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	_, err = model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Provider != "openai_response" || providerErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("Responses error not normalized: %#v err=%v", providerErr, err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = model.Call(cancelled, asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context should be preserved, got %v", err)
	}

	videoMsg, err := message.NewUserMessage("Tony", []message.ContentBlock{
		message.NewDataBlock(message.NewURLSource("https://example.com/video.mp4", "video/mp4")),
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

func responsePayload(id string, output []any) map[string]any {
	return map[string]any{
		"id":     id,
		"object": "response",
		"status": "completed",
		"output": output,
		"usage":  map[string]any{"input_tokens": 5, "output_tokens": 7, "total_tokens": 12},
	}
}

func newResponseServer(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected Responses path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("Authorization header mismatch: %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		handler(w, r, body)
	}))
}

func TestResponseModelListModelsLoadsMetadata(t *testing.T) {
	t.Parallel()

	cards, err := asopenairesponse.ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("ListModels should load embedded metadata")
	}
	found := false
	for _, card := range cards {
		if card.Name == "gpt-5.4" {
			found = true
			if !card.Supports(asmodel.ModelCapabilityTools) || !card.Supports(asmodel.ModelCapabilityStructuredOutput) {
				t.Fatalf("gpt-5.4 Responses metadata missing capabilities: %#v", card)
			}
		}
	}
	if !found {
		t.Fatalf("gpt-5.4 Responses metadata not found: %#v", cards)
	}
}

func TestResponseModelGenerateStructuredUsesOptionalInterface(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newResponseServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		requestCh <- body
		writeJSON(t, w, responsePayload("resp-structured", []any{
			map[string]any{
				"id":   "msg-1",
				"type": "message",
				"content": []any{
					map[string]any{"type": "output_text", "text": `{"answer":"ok","count":2}`},
				},
			},
		}))
	})
	defer server.Close()

	model, err := asopenairesponse.NewResponseModel(
		asopenairesponse.NewCredential("test-key", asopenairesponse.WithBaseURL(server.URL)),
		"gpt-5.4",
	)
	if err != nil {
		t.Fatalf("NewResponseModel returned error: %v", err)
	}
	if _, ok := any(model).(asmodel.ChatModel); !ok {
		t.Fatal("ResponseModel should still implement ChatModel")
	}
	structuredModel, ok := any(model).(asmodel.StructuredOutputModel)
	if !ok {
		t.Fatal("ResponseModel should expose structured output through optional interface")
	}
	userMsg, err := message.NewUserMessage("Tony", "answer")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	resp, err := structuredModel.GenerateStructured(context.Background(), asmodel.StructuredOutputRequest{
		CallRequest: asmodel.CallRequest{Messages: []*message.Message{userMsg}},
		Name:        "answer_schema",
		Strict:      true,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"answer": map[string]any{"type": "string"}, "count": map[string]any{"type": "integer"}},
			"required":   []any{"answer", "count"},
		},
	})
	if err != nil {
		t.Fatalf("GenerateStructured returned error: %v", err)
	}
	body := <-requestCh
	format := body["text"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["name"] != "answer_schema" || format["strict"] != true {
		t.Fatalf("structured output format not forwarded: %#v", format)
	}
	if resp.Type != asmodel.StructuredResponseType || resp.Content["answer"] != "ok" || resp.Content["count"] != float64(2) {
		t.Fatalf("structured response mismatch: %#v", resp)
	}
	if resp.Metadata["provider"] != "openai_response" {
		t.Fatalf("structured response metadata mismatch: %#v", resp.Metadata)
	}
}

func TestResponseModelRejectsChatOnlyAudioParameters(t *testing.T) {
	t.Parallel()

	model, err := asopenairesponse.NewResponseModel(asopenairesponse.NewCredential("test-key"), "gpt-5.4")
	if err != nil {
		t.Fatalf("NewResponseModel returned error: %v", err)
	}
	err = model.ValidateParameters(map[string]any{"audio": map[string]any{"format": "wav"}, "modalities": []any{"text", "audio"}})
	if err == nil || !strings.Contains(err.Error(), "Chat Completions audio") {
		t.Fatalf("Responses should reject Chat Completions audio parameters, got %v", err)
	}
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
