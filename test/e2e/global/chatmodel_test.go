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

package global_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func TestGlobalChatModelCallStreamAndToolSchemaE2E(t *testing.T) {
	t.Parallel()

	echoTool, err := tool.NewFunctionTool(
		"Echo",
		"Echo one value.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value": map[string]any{"type": "string"},
			},
			"required": []string{"value"},
		},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("echo")}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
	requireNoErr(t, err, "NewFunctionTool returned error")
	kit, err := tool.NewToolkit(echoTool)
	requireNoErr(t, err, "NewToolkit returned error")
	schemas, err := kit.ToolSchemas()
	requireNoErr(t, err, "ToolSchemas returned error")
	systemMsg, err := message.NewSystemMessage("system", "Reply through the direct ChatModel API.")
	requireNoErr(t, err, "NewSystemMessage returned error")
	userMsg, err := message.NewUserMessage("Tony", message.ContentBlockList{
		message.NewTextBlock("call and stream"),
		message.NewDataBlock(message.NewURLSource("file:///tmp/input.txt", "text/plain")),
	})
	requireNoErr(t, err, "NewUserMessage returned error")
	request := modelpkg.CallRequest{
		Messages: []*message.Message{systemMsg, userMsg},
		Tools:    schemas,
		Metadata: map[string]any{"trace": "chatmodel-e2e"},
		Parameters: map[string]any{
			"temperature": 0,
		},
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("call ok")},
			true,
			modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{InputTokens: 11, OutputTokens: 2, Time: time.Millisecond}),
			modelpkg.WithChatResponseMetadata(map[string]any{"mode": "call"}),
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("stream ok", message.WithBlockID("stream-text"))},
			true,
			modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{InputTokens: 13, OutputTokens: 3, Time: 2 * time.Millisecond}),
			modelpkg.WithChatResponseMetadata(map[string]any{"mode": "stream"}),
		),
	}}

	callResponse, err := model.Call(context.Background(), request)
	requireNoErr(t, err, "Call returned error")
	assertChatResponseText(t, callResponse, "call ok")
	assertUsage(t, callResponse, 11, 2)
	if callResponse.Metadata["mode"] != "call" {
		t.Fatalf("call metadata mismatch: %#v", callResponse.Metadata)
	}
	tokenCount, err := model.CountTokens(request)
	requireNoErr(t, err, "CountTokens returned error")
	if tokenCount <= 0 {
		t.Fatalf("CountTokens should include messages and tool schema, got %d", tokenCount)
	}

	stream, err := model.Stream(context.Background(), request)
	requireNoErr(t, err, "Stream returned error")
	chunks := collectChatStream(t, stream)
	if len(chunks) != 2 || chunks[0].IsLast || !chunks[1].IsLast {
		t.Fatalf("stream should emit one delta and one final response, got %#v", chunks)
	}
	assertChatResponseText(t, &chunks[0], "stream ok")
	assertChatResponseText(t, &chunks[1], "stream ok")
	assertUsage(t, &chunks[1], 13, 3)
	if chunks[1].Metadata["mode"] != "stream" {
		t.Fatalf("stream metadata mismatch: %#v", chunks[1].Metadata)
	}
	if len(model.requests) != 2 || !requestIncludesTool(model.requests[0], "Echo") || !requestIncludesTool(model.requests[1], "Echo") {
		t.Fatalf("direct ChatModel requests should preserve tool schemas, got %#v", model.requests)
	}

	model.requests[0].Messages[0].Content[0].(*message.TextBlock).Text = "mutated"
	if text := systemMsg.GetTextContent(""); text == nil || *text != "Reply through the direct ChatModel API." {
		t.Fatalf("recorded direct request should be cloned away from caller messages: %#v", systemMsg)
	}
}

func TestGlobalChatModelStreamErrorE2E(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("provider stream failed")
	model := asyncErrorChatModel{err: expectedErr}
	userMsg, err := message.NewUserMessage("Tony", "stream error")
	requireNoErr(t, err, "NewUserMessage returned error")

	stream, err := model.Stream(context.Background(), modelpkg.CallRequest{Messages: []*message.Message{userMsg}})
	requireNoErr(t, err, "Stream returned setup error")
	chunks := collectChatStream(t, stream)
	if len(chunks) != 1 {
		t.Fatalf("expected one terminal error chunk, got %#v", chunks)
	}
	if chunks[0].Error == nil || !strings.Contains(chunks[0].Error.Error(), expectedErr.Error()) || !chunks[0].IsLast {
		t.Fatalf("terminal stream error not preserved: %#v", chunks[0])
	}
	if got, countErr := model.CountTokens(modelpkg.CallRequest{Messages: []*message.Message{userMsg}}); countErr != nil || got == 0 {
		t.Fatalf("CountTokens should still work for errored stream model, got count=%d err=%v", got, countErr)
	}
}

type asyncErrorChatModel struct {
	err error
}

func (m asyncErrorChatModel) Name() string {
	return "async-error-chatmodel-e2e"
}

func (m asyncErrorChatModel) Call(context.Context, modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	return nil, m.err
}

func (m asyncErrorChatModel) Stream(context.Context, modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	out := make(chan modelpkg.ChatResponse, 1)
	out <- *modelpkg.NewChatResponse(nil, true, modelpkg.WithChatResponseError(m.err))
	close(out)
	return out, nil
}

func (m asyncErrorChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func collectChatStream(t *testing.T, stream <-chan modelpkg.ChatResponse) []modelpkg.ChatResponse {
	t.Helper()
	var chunks []modelpkg.ChatResponse
	for chunk := range stream {
		chunks = append(chunks, *chunk.Clone())
	}
	return chunks
}

func assertChatResponseText(t *testing.T, response *modelpkg.ChatResponse, want string) {
	t.Helper()
	if response == nil {
		t.Fatal("chat response is nil")
	}
	if got := response.GetTextContent(""); got == nil || *got != want {
		t.Fatalf("chat response text mismatch: got %#v want %q", got, want)
	}
}

func assertUsage(t *testing.T, response *modelpkg.ChatResponse, inputTokens, outputTokens int) {
	t.Helper()
	if response == nil || response.Usage == nil {
		t.Fatalf("chat response usage is missing: %#v", response)
	}
	if response.Usage.InputTokens != inputTokens || response.Usage.OutputTokens != outputTokens || response.Usage.Type != modelpkg.UsageTypeChat {
		t.Fatalf("chat usage mismatch: %#v", response.Usage)
	}
}
