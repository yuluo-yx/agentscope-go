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

package model_test

import (
	"context"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/types"
)

type fakeChatModel struct {
	response *modelpkg.ChatResponse
}

func (f fakeChatModel) Name() string {
	return "fake"
}

func (f fakeChatModel) Call(context.Context, modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	return f.response, nil
}

func (f fakeChatModel) Stream(context.Context, modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	ch := make(chan modelpkg.ChatResponse, 1)
	ch <- *f.response.Clone()
	close(ch)
	return ch, nil
}

func (f fakeChatModel) CountTokens(modelpkg.CallRequest) (int, error) {
	return 4, nil
}

func TestChatModelInterfaceAndResponseDefaults(t *testing.T) {
	t.Parallel()

	usage := &modelpkg.ChatUsage{InputTokens: 3, OutputTokens: 5, Time: 10 * time.Millisecond}
	resp := modelpkg.NewChatResponse(
		message.ContentBlockList{message.NewTextBlock("hello")},
		true,
		modelpkg.WithChatResponseUsage(usage),
		modelpkg.WithChatResponseMetadata(map[string]any{"provider": "fake"}),
	)

	var model modelpkg.ChatModel = fakeChatModel{response: resp}
	got, err := model.Call(context.Background(), modelpkg.CallRequest{})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if got.Type != modelpkg.ChatResponseType || !got.IsLast {
		t.Fatalf("unexpected chat response flags: %#v", got)
	}
	if got.ID == "" || got.CreatedAt == "" {
		t.Fatalf("response should have id and creation time: %#v", got)
	}
	if got.Usage.Type != modelpkg.UsageTypeChat {
		t.Fatalf("chat usage type should default to chat, got %q", got.Usage.Type)
	}
	if got.Metadata["provider"] != "fake" {
		t.Fatalf("metadata not preserved: %#v", got.Metadata)
	}

	cloned := got.Clone()
	cloned.Content[0].(*message.TextBlock).Text = "changed"
	cloned.Metadata["provider"] = "changed"
	if got.Content[0].(*message.TextBlock).Text != "hello" || got.Metadata["provider"] != "fake" {
		t.Fatalf("chat response clone mutated original: %#v", got)
	}
}

func TestApproximateTokenCountUsesMessagesAndToolSchemas(t *testing.T) {
	t.Parallel()

	msg, err := message.NewUserMessage("Tony", []message.ContentBlock{
		message.NewTextBlock("12345678"),
		message.NewDataBlock(message.NewBase64Source("12345678", "image/png")),
	})
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	tools := []modelpkg.ToolSchema{{
		Type: "function",
		Function: modelpkg.FunctionSchema{
			Name:        "Read",
			Description: "read files",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		},
	}}

	if got := modelpkg.ApproximateTokenCount([]*message.Message{msg}, tools); got <= 4 {
		t.Fatalf("token count should include text, base64 and tool schema bytes, got %d", got)
	}
}

func TestChatResponseOptionsAndCallRequestClone(t *testing.T) {
	t.Parallel()

	resp := modelpkg.NewChatResponse(
		message.ContentBlockList{message.NewThinkingBlock("plan"), message.NewToolCallBlock("call-1", "Read", "{}")},
		false,
		modelpkg.WithChatResponseID("resp-1"),
		modelpkg.WithChatResponseCreatedAt("2026-05-28T00:00:00Z"),
	)
	if resp.ID != "resp-1" || resp.CreatedAt != "2026-05-28T00:00:00Z" || resp.IsLast {
		t.Fatalf("response options not applied: %#v", resp)
	}

	msg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{message.NewHintBlock("hint")})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	request := modelpkg.CallRequest{
		Messages: []*message.Message{msg},
		Tools: []modelpkg.ToolSchema{{
			Type: "function",
			Function: modelpkg.FunctionSchema{
				Name:       "Read",
				Parameters: map[string]any{"type": "object"},
			},
			Metadata: map[string]any{"group": "basic"},
		}},
		ToolChoice: &types.ToolChoice{Mode: "Read", Tools: []string{"Read"}},
		Metadata:   map[string]any{"trace": []any{"a"}},
		Parameters: map[string]any{"temperature": 0},
	}

	cloned := request.Clone()
	cloned.Messages[0].Content[0].(*message.HintBlock).Hint = "changed"
	cloned.Tools[0].Function.Parameters["type"] = "changed"
	cloned.ToolChoice.Tools[0] = "Write"
	cloned.Metadata["trace"].([]any)[0] = "changed"
	cloned.Parameters["temperature"] = 1

	if request.Messages[0].Content[0].(*message.HintBlock).Hint != "hint" {
		t.Fatalf("message clone mutated original request: %#v", request.Messages[0])
	}
	if request.Tools[0].Function.Parameters["type"] != "object" || request.ToolChoice.Tools[0] != "Read" {
		t.Fatalf("tool schema or choice clone mutated original: %#v", request)
	}
	if request.Metadata["trace"].([]any)[0] != "a" || request.Parameters["temperature"] != 0 {
		t.Fatalf("metadata or parameters clone mutated original: %#v", request)
	}
}

func TestChatUsageAndResponseNilClone(t *testing.T) {
	t.Parallel()

	if (*modelpkg.ChatUsage)(nil).Clone() != nil {
		t.Fatal("nil chat usage clone should return nil")
	}
	if (*modelpkg.ChatResponse)(nil).Clone() != nil {
		t.Fatal("nil chat response clone should return nil")
	}

	resp := modelpkg.NewChatResponse(
		nil,
		true,
		modelpkg.WithChatResponseID(""),
		modelpkg.WithChatResponseCreatedAt(""),
		modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{}),
		modelpkg.WithChatResponseMetadata(nil),
	)
	if resp.ID == "" || resp.CreatedAt == "" || resp.Usage.Type != modelpkg.UsageTypeChat || resp.Metadata == nil {
		t.Fatalf("blank response options should be defaulted: %#v", resp)
	}
}

func TestApproximateTokenCountCoversBlockVariants(t *testing.T) {
	t.Parallel()

	msg, err := message.NewAssistantMessage("Friday", []message.ContentBlock{
		message.NewThinkingBlock("thinking"),
		message.NewHintBlock("hint"),
		message.NewToolCallBlock("call-1", "Read", `{"path":"README.md"}`),
		message.NewToolResultBlock("call-1", "Read", message.ToolResultOutput{Raw: "done"}, message.ToolResultSuccess),
		message.NewDataBlock(message.NewURLSource("https://example.com/file.txt", "text/plain")),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	if got := modelpkg.ApproximateTokenCount([]*message.Message{nil, msg}, nil); got == 0 {
		t.Fatal("block variants should contribute tokens")
	}
	if got := modelpkg.ApproximateTokenCount(nil, []modelpkg.ToolSchema{{Function: modelpkg.FunctionSchema{Parameters: map[string]any{"bad": make(chan struct{})}}}}); got != 0 {
		t.Fatalf("unmarshalable tool schema should be skipped, got %d", got)
	}
	if got := modelpkg.ApproximateTokenCount([]*message.Message{{Content: message.ContentBlockList{&message.DataBlock{}}}}, nil); got != 0 {
		t.Fatalf("data block without source should not contribute tokens, got %d", got)
	}
}
