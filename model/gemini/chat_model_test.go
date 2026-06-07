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

package gemini_test

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
	"github.com/yuluo-yx/agentscope-go/model/gemini"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestChatModelCallUsesGenAIFormatsRequestAndParsesResponse(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newGeminiServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Fatalf("expected generateContent path, got %s", r.URL.Path)
		}
		requestCh <- body
		writeJSON(t, w, geminiResponse("gemini-1", []any{
			map[string]any{"text": "hello"},
			map[string]any{"functionCall": map[string]any{"name": "Read", "args": map[string]any{"path": "README.md"}}},
		}))
	})
	defer server.Close()

	maxTokens := int32(64)
	model, err := gemini.NewChatModel(
		gemini.NewCredential("test-key", gemini.WithBaseURL(server.URL)),
		"gemini-2.5-flash",
		gemini.WithChatParameters(gemini.ChatParameters{
			MaxTokens:      &maxTokens,
			ThinkingEnable: true,
			ThinkingBudget: int32Ptr(256),
			Temperature:    float32Ptr(0.2),
			TopP:           float32Ptr(0.9),
		}),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	systemMsg, err := message.NewSystemMessage("system", "Be concise.")
	if err != nil {
		t.Fatalf("NewSystemMessage returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", []message.ContentBlock{
		message.NewTextBlock("Read README"),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "image/png")),
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
	if _, ok := body["contents"]; !ok {
		t.Fatalf("Gemini request should use contents: %#v", body)
	}
	if _, ok := body["systemInstruction"]; !ok {
		t.Fatalf("Gemini system instruction missing: %#v", body)
	}
	if _, ok := body["tools"]; !ok {
		t.Fatalf("Gemini tools missing: %#v", body)
	}
	if _, ok := body["toolConfig"]; !ok {
		t.Fatalf("Gemini tool choice config missing: %#v", body)
	}
	generationConfig := body["generationConfig"].(map[string]any)
	if generationConfig["maxOutputTokens"] != float64(64) || generationConfig["temperature"] != 0.2 {
		t.Fatalf("Gemini generation config mismatch: %#v", generationConfig)
	}
	if text := resp.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("Gemini text not parsed: %#v", resp)
	}
	call := resp.Content[1].(*message.ToolCallBlock)
	if call.Name != "Read" || call.Input != `{"path":"README.md"}` {
		t.Fatalf("Gemini function call not parsed: %#v", call)
	}
	if resp.Usage.InputTokens != 3 || resp.Usage.OutputTokens != 4 {
		t.Fatalf("Gemini usage not parsed: %#v", resp.Usage)
	}
}

func TestChatModelStreamAccumulatesDeltas(t *testing.T) {
	t.Parallel()

	server := newGeminiServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			t.Fatalf("expected streamGenerateContent path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writer := bufio.NewWriter(w)
		for _, payload := range []map[string]any{
			geminiResponse("gemini-stream", []any{map[string]any{"text": "hel"}}),
			geminiResponse("gemini-stream", []any{map[string]any{"text": "lo"}}),
			geminiResponse("gemini-stream", []any{map[string]any{"functionCall": map[string]any{"name": "Read", "args": map[string]any{"path": "README.md"}}}}),
		} {
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("Marshal stream payload returned error: %v", err)
			}
			fmt.Fprintf(writer, "data: %s\n\n", data)
		}
		if err := writer.Flush(); err != nil {
			t.Fatalf("Flush returned error: %v", err)
		}
	})
	defer server.Close()

	model, err := gemini.NewChatModel(
		gemini.NewCredential("test-key", gemini.WithBaseURL(server.URL)),
		"gemini-2.5-flash",
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
	if len(chunks) != 4 {
		t.Fatalf("unexpected Gemini chunks: %d %#v", len(chunks), chunks)
	}
	if text := chunks[0].GetTextContent(""); chunks[0].IsLast || text == nil || *text != "hel" {
		t.Fatalf("first Gemini delta mismatch: %#v", chunks[0])
	}
	final := chunks[len(chunks)-1]
	if !final.IsLast {
		t.Fatalf("final Gemini chunk should be last: %#v", final)
	}
	if text := final.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("final Gemini text mismatch: %#v", final)
	}
	if final.Content[1].(*message.ToolCallBlock).Name != "Read" {
		t.Fatalf("final Gemini tool call mismatch: %#v", final)
	}
}

func TestChatModelErrorsContextAndCapabilityRejection(t *testing.T) {
	t.Parallel()

	server := newGeminiServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(t, w, map[string]any{"error": map[string]any{"message": "bad input", "code": 400, "status": "INVALID_ARGUMENT"}})
	})
	defer server.Close()

	model, err := gemini.NewChatModel(
		gemini.NewCredential("test-key", gemini.WithBaseURL(server.URL)),
		"gemini-2.5-flash",
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
	if !errors.As(err, &providerErr) || providerErr.Provider != "gemini" {
		t.Fatalf("Gemini error should be normalized: %#v err=%v", providerErr, err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = model.Call(cancelled, asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context should be preserved, got %v", err)
	}

	pdfMsg, err := message.NewUserMessage("Tony", []message.ContentBlock{
		message.NewDataBlock(message.NewURLSource("https://example.com/file.pdf", "application/pdf")),
	})
	if err != nil {
		t.Fatalf("NewUserMessage pdf returned error: %v", err)
	}
	_, err = model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{pdfMsg}})
	var capabilityErr *asmodel.CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != asmodel.ModelCapabilityGeneration {
		t.Fatalf("unsupported media should be rejected with CapabilityError, got %T %v", err, err)
	}
}

func TestListModelsLoadsGeminiMetadata(t *testing.T) {
	t.Parallel()

	cards, err := gemini.ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("Gemini ListModels should load embedded model cards")
	}
	card := cards[0]
	if !card.Supports(asmodel.ModelCapabilityVideo) || !card.Supports(asmodel.ModelCapabilityStructuredOutput) {
		t.Fatalf("Gemini metadata should include video and structured output: %#v", card)
	}
}

func newGeminiServer(t *testing.T, handler func(http.ResponseWriter, *http.Request, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-goog-api-key"); got != "test-key" {
			t.Fatalf("Gemini API key header mismatch: %q raw_query=%s", got, r.URL.RawQuery)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode Gemini request body returned error: %v", err)
		}
		handler(w, r, body)
	}))
}

func geminiResponse(id string, parts []any) map[string]any {
	return map[string]any{
		"responseId": id,
		"candidates": []any{
			map[string]any{
				"content": map[string]any{"role": "model", "parts": parts},
			},
		},
		"usageMetadata": map[string]any{"promptTokenCount": 3, "candidatesTokenCount": 4, "totalTokenCount": 7},
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

func int32Ptr(value int32) *int32 { return &value }

func float32Ptr(value float32) *float32 { return &value }

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("Encode response returned error: %v", err)
	}
}
