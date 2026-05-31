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

package message_test

import (
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
)

func TestApplyEventAccumulatesStreamingMessage(t *testing.T) {
	t.Parallel()

	msg, err := message.NewMessage("assistant", message.RoleAssistant, nil, message.WithMessageID("reply-1"))
	if err != nil {
		t.Fatalf("NewMessage returned error: %v", err)
	}

	events := []message.Event{
		message.NewTextBlockStartEvent("reply-1", "text-1"),
		message.NewTextBlockDeltaEvent("reply-1", "text-1", "Hello"),
		message.NewTextBlockDeltaEvent("reply-1", "text-1", " World"),
		message.NewTextBlockEndEvent("reply-1", "text-1"),
		message.NewDataBlockStartEvent("reply-1", "data-1", "image/png"),
		message.NewDataBlockDeltaEvent("reply-1", "data-1", "abc", "image/png"),
		message.NewDataBlockEndEvent("reply-1", "data-1"),
		message.NewThinkingBlockStartEvent("reply-1", "think-1"),
		message.NewThinkingBlockDeltaEvent("reply-1", "think-1", "Let me think"),
		message.NewThinkingBlockEndEvent("reply-1", "think-1"),
		message.NewToolCallStartEvent("reply-1", "call-1", "Search"),
		message.NewToolCallDeltaEvent("reply-1", "call-1", `{"query":"AgentScope"}`),
		message.NewToolCallEndEvent("reply-1", "call-1"),
		message.NewRequireUserConfirmEvent("reply-1", []*message.ToolCallBlock{
			message.NewToolCallBlock("call-1", "Search", `{"query":"AgentScope"}`),
		}),
		message.NewUserConfirmResultEvent("reply-1", []message.ConfirmResult{{
			Confirmed: true,
			ToolCall:  message.NewToolCallBlock("call-1", "Search", `{"query":"AgentScope"}`),
		}}),
		message.NewToolCallStartEvent("reply-1", "call-ext", "External"),
		message.NewToolCallDeltaEvent("reply-1", "call-ext", `{}`),
		message.NewToolResultStartEvent("reply-1", "call-1", "Search"),
		message.NewToolResultTextDeltaEvent("reply-1", "call-1", "result "),
		message.NewToolResultTextDeltaEvent("reply-1", "call-1", "ok"),
		message.NewToolResultDataDeltaEvent("reply-1", "call-1", "result-data-1", "image/png", "", "https://example.com/result.png"),
		message.NewToolResultEndEvent("reply-1", "call-1", message.ToolResultSuccess),
		message.NewRequireExternalExecutionEvent("reply-1", []*message.ToolCallBlock{
			message.NewToolCallBlock("call-ext", "External", `{}`),
		}),
		message.NewExternalExecutionResultEvent("reply-1", []*message.ToolResultBlock{
			message.NewToolResultBlock("external-1", "External", message.ToolResultOutput{Raw: "done"}, message.ToolResultSuccess),
		}),
		message.NewModelCallEndEvent("reply-1", 10, 5),
		message.NewModelCallEndEvent("reply-1", 3, 2),
		message.NewReplyEndEvent("session-1", "reply-1"),
		message.NewExceedMaxItersEvent("reply-1", "assistant"),
	}

	for _, event := range events {
		if err := msg.ApplyEvent(event); err != nil {
			t.Fatalf("ApplyEvent(%s) returned error: %v", event.GetType(), err)
		}
	}

	if got := msg.GetTextContent(""); got == nil || *got != "Hello World" {
		t.Fatalf("unexpected text content: %#v", got)
	}
	toolCalls := msg.GetContentBlocks("tool_call")
	if len(toolCalls) != 2 || toolCalls[0].(*message.ToolCallBlock).State != message.ToolCallAllowed {
		t.Fatalf("unexpected tool call state: %#v", toolCalls)
	}
	if toolCalls[1].(*message.ToolCallBlock).State != message.ToolCallSubmitted {
		t.Fatalf("external tool call should be submitted: %#v", toolCalls[1])
	}
	toolResults := msg.GetContentBlocks("tool_result")
	if len(toolResults) != 2 {
		t.Fatalf("expected two tool results, got %d", len(toolResults))
	}
	result := toolResults[0].(*message.ToolResultBlock)
	if result.State != message.ToolResultSuccess {
		t.Fatalf("unexpected tool result state: %q", result.State)
	}
	if result.Output.Blocks[0].(*message.TextBlock).Text != "result ok" {
		t.Fatalf("unexpected tool result output: %#v", result.Output)
	}
	if result.Output.Blocks[1].(*message.DataBlock).Source.SourceType() != "url" {
		t.Fatalf("tool result data block not appended: %#v", result.Output.Blocks[1])
	}
	if msg.Usage == nil || msg.Usage.InputTokens != 13 || msg.Usage.OutputTokens != 7 {
		t.Fatalf("usage not accumulated: %#v", msg.Usage)
	}
	if msg.FinishedAt == nil {
		t.Fatal("finished_at should be set by ReplyEndEvent")
	}

	if err := msg.ApplyEvent(message.NewTextBlockDeltaEvent("other-reply", "missing", "ignored")); err != nil {
		t.Fatalf("wrong reply id should be ignored, got error: %v", err)
	}
	if err := msg.ApplyEvent(nil); err == nil {
		t.Fatal("nil event should return an error")
	}
}

func TestEventJSONRoundTripUsesDiscriminator(t *testing.T) {
	t.Parallel()

	event := message.NewToolResultDataDeltaEvent("reply-1", "call-1", "data-1", "image/png", "abc", "")

	data, err := message.MarshalEvent(event)
	if err != nil {
		t.Fatalf("MarshalEvent returned error: %v", err)
	}
	decoded, err := message.UnmarshalEvent(data)
	if err != nil {
		t.Fatalf("UnmarshalEvent returned error: %v", err)
	}

	got, ok := decoded.(*message.ToolResultDataDeltaEvent)
	if !ok {
		t.Fatalf("unexpected event type: %T", decoded)
	}
	if got.Data != "abc" || got.URL != "" {
		t.Fatalf("unexpected decoded event: %#v", got)
	}
}

func TestApplyEventConvertsRawToolResultOutput(t *testing.T) {
	t.Parallel()

	msg, err := message.NewMessage("assistant", message.RoleAssistant, []message.ContentBlock{
		message.NewToolResultBlock("call-1", "Bash", message.ToolResultOutput{Raw: "raw "}, message.ToolResultRunning),
	}, message.WithMessageID("reply-1"))
	if err != nil {
		t.Fatalf("NewMessage returned error: %v", err)
	}

	if err := msg.ApplyEvent(message.NewToolResultTextDeltaEvent("reply-1", "call-1", "delta")); err != nil {
		t.Fatalf("ApplyEvent returned error: %v", err)
	}

	result := msg.GetContentBlocks("tool_result")[0].(*message.ToolResultBlock)
	if result.Output.Blocks[0].(*message.TextBlock).Text != "raw delta" || result.Output.Raw != "" {
		t.Fatalf("raw output was not converted and appended: %#v", result.Output)
	}
}

func TestUnmarshalEventRejectsUnknownType(t *testing.T) {
	t.Parallel()

	_, err := message.UnmarshalEvent([]byte(`{"type":"UNKNOWN","reply_id":"reply-1"}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported event error, got %v", err)
	}
}

func TestAllEventTypesJSONRoundTrip(t *testing.T) {
	t.Parallel()

	events := []message.Event{
		message.NewReplyStartEvent("session-1", "reply-1", "assistant"),
		message.NewReplyEndEvent("session-1", "reply-1"),
		message.NewModelCallStartEvent("reply-1", "gpt"),
		message.NewModelCallEndEvent("reply-1", 1, 2),
		message.NewTextBlockStartEvent("reply-1", "text-1"),
		message.NewTextBlockDeltaEvent("reply-1", "text-1", "a"),
		message.NewTextBlockEndEvent("reply-1", "text-1"),
		message.NewDataBlockStartEvent("reply-1", "data-1", "image/png"),
		message.NewDataBlockDeltaEvent("reply-1", "data-1", "abc", "image/png"),
		message.NewDataBlockEndEvent("reply-1", "data-1"),
		message.NewThinkingBlockStartEvent("reply-1", "think-1"),
		message.NewThinkingBlockDeltaEvent("reply-1", "think-1", "a"),
		message.NewThinkingBlockEndEvent("reply-1", "think-1"),
		message.NewToolCallStartEvent("reply-1", "call-1", "Bash"),
		message.NewToolCallDeltaEvent("reply-1", "call-1", "{}"),
		message.NewToolCallEndEvent("reply-1", "call-1"),
		message.NewToolResultStartEvent("reply-1", "call-1", "Bash"),
		message.NewToolResultTextDeltaEvent("reply-1", "call-1", "ok"),
		message.NewToolResultDataDeltaEvent("reply-1", "call-1", "data-1", "image/png", "abc", ""),
		message.NewToolResultEndEvent("reply-1", "call-1", message.ToolResultSuccess),
		message.NewExceedMaxItersEvent("reply-1", "assistant"),
		message.NewRequireUserConfirmEvent("reply-1", []*message.ToolCallBlock{message.NewToolCallBlock("call-1", "Bash", "{}")}),
		message.NewRequireExternalExecutionEvent("reply-1", []*message.ToolCallBlock{message.NewToolCallBlock("call-1", "Bash", "{}")}),
		message.NewUserConfirmResultEvent("reply-1", []message.ConfirmResult{{Confirmed: false, ToolCall: message.NewToolCallBlock("call-1", "Bash", "{}")}}),
		message.NewExternalExecutionResultEvent("reply-1", []*message.ToolResultBlock{
			message.NewToolResultBlock("call-1", "Bash", message.ToolResultOutput{Raw: "ok"}, message.ToolResultSuccess),
		}),
	}

	for _, event := range events {
		t.Run(string(event.GetType()), func(t *testing.T) {
			t.Parallel()

			data, err := message.MarshalEvent(event)
			if err != nil {
				t.Fatalf("MarshalEvent returned error: %v", err)
			}
			decoded, err := message.UnmarshalEvent(data)
			if err != nil {
				t.Fatalf("UnmarshalEvent returned error: %v", err)
			}
			if decoded.GetType() != event.GetType() {
				t.Fatalf("type mismatch: want %s, got %s", event.GetType(), decoded.GetType())
			}
			if decoded.GetID() == "" || decoded.ReplyID() != "reply-1" {
				t.Fatalf("decoded event lost base fields: %#v", decoded)
			}
		})
	}
}
