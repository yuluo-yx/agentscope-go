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
	"encoding/json"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
)

func TestContentBlockJSONRoundTrip(t *testing.T) {
	t.Parallel()

	input := message.ContentBlockList{
		message.NewTextBlock("hello"),
		message.NewThinkingBlock("thinking", message.WithExtra("signature", "sig-1")),
		message.NewHintBlock("hint"),
		message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
		message.NewToolCallBlock("call-1", "Bash", `{"command":"go test"}`,
			message.WithToolCallState(message.ToolCallAsking),
			message.WithSuggestedRules([]permission.Rule{{
				ToolName:    "Bash",
				RuleContent: "go test:*",
				Behavior:    permission.BehaviorAllow,
				Source:      "test",
			}}),
			message.WithToolCallExtra("call_id", "provider-call-1"),
		),
		message.NewToolResultBlock("call-1", "Bash", message.ToolResultOutput{Raw: "ok"}, message.ToolResultSuccess),
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded message.ContentBlockList
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v\njson: %s", err, data)
	}

	if len(decoded) != len(input) {
		t.Fatalf("decoded block count mismatch: want %d, got %d", len(input), len(decoded))
	}
	if got := decoded[1].(*message.ThinkingBlock).Extra["signature"]; got != "sig-1" {
		t.Fatalf("thinking extra not preserved: %#v", got)
	}
	if got := decoded[4].(*message.ToolCallBlock).Extra["call_id"]; got != "provider-call-1" {
		t.Fatalf("tool call extra not preserved: %#v", got)
	}
	if got := decoded[5].(*message.ToolResultBlock).Output.Raw; got != "ok" {
		t.Fatalf("tool result raw output mismatch: %q", got)
	}
}

func TestContentBlockListQueries(t *testing.T) {
	t.Parallel()

	textID := "text-1"
	callID := "call-1"
	blocks := message.ContentBlockList{
		message.NewTextBlock("hello", message.WithBlockID(textID)),
		message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
		message.NewToolCallBlock(callID, "Read", "{}"),
		message.NewTextBlock("world"),
	}

	text := blocks.GetTextContent(" ")
	if text == nil || *text != "hello world" {
		t.Fatalf("unexpected text content: %#v", text)
	}
	if !blocks.HasContentBlocks("text") || !blocks.HasContentBlocks("tool_call", "data") || !blocks.HasContentBlocks() {
		t.Fatalf("content block list should report existing blocks: %#v", blocks)
	}
	if blocks.HasContentBlocks("thinking") {
		t.Fatalf("content block list should not report missing thinking blocks: %#v", blocks)
	}
	if got := blocks.GetContentBlocks("text"); len(got) != 2 {
		t.Fatalf("unexpected text blocks: %#v", got)
	}
	if got := blocks.GetContentBlocks("tool_call")[0].(*message.ToolCallBlock); got.ID != callID {
		t.Fatalf("unexpected tool call block: %#v", got)
	}
	if got := blocks.FindBlock("text", textID); got == nil || got.BlockID() != textID {
		t.Fatalf("expected to find text block %q, got %#v", textID, got)
	}
	if got := blocks.FindBlock("tool_call", "missing"); got != nil {
		t.Fatalf("missing block lookup should return nil: %#v", got)
	}

	if got := (message.ContentBlockList{
		message.NewDataBlock(message.NewURLSource("https://example.com/image.png", "image/png")),
	}).GetTextContent(""); got != nil {
		t.Fatalf("data-only blocks should not return text content: %#v", got)
	}
}

func TestUnknownDiscriminatorsReturnErrors(t *testing.T) {
	t.Parallel()

	if _, err := message.UnmarshalContentBlock([]byte(`{"type":"unknown"}`)); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported block error, got %v", err)
	}
	if _, err := message.UnmarshalDataSource([]byte(`{"type":"unknown"}`)); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported source error, got %v", err)
	}
}

func TestToolResultOutputSupportsBlockList(t *testing.T) {
	t.Parallel()

	output := message.ToolResultOutput{Blocks: message.ContentBlockList{
		message.NewTextBlock("hello"),
		message.NewDataBlock(message.NewBase64Source("abc", "image/png")),
	}}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded message.ToolResultOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if text := decoded.Blocks.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("decoded text block mismatch: %#v", decoded.Blocks[0])
	}
	if decoded.Blocks[1].(*message.DataBlock).Source.SourceType() != "base64" {
		t.Fatalf("decoded data source mismatch: %#v", decoded.Blocks[1])
	}
}

func TestBlockConstructorsOptionsAndClone(t *testing.T) {
	t.Parallel()

	text := message.NewTextBlock("hello", message.WithBlockID("text-1"))
	thinking := message.NewThinkingBlock("think", message.WithThinkingBlockID("think-1"))
	hint := message.NewHintBlock("hint", message.WithHintBlockID("hint-1"))
	data := message.NewDataBlock(
		message.NewBase64Source("abc", "image/png"),
		message.WithDataBlockID("data-1"),
		message.WithDataBlockName("image"),
	)
	call := message.NewToolCallBlock("call-1", "Bash", "{}", message.WithToolCallExtra("x", map[string]any{"nested": "y"}))
	result := message.NewToolResultBlock("call-1", "Bash", message.ToolResultOutput{Blocks: message.ContentBlockList{text}}, "")

	blocks := []message.ContentBlock{text, thinking, hint, data, call, result}
	for _, block := range blocks {
		if block.BlockType() == "" || block.BlockID() == "" {
			t.Fatalf("block should expose type and id: %#v", block)
		}
	}

	if data.Source.SourceType() != "base64" || data.Source.Clone().SourceType() != "base64" {
		t.Fatalf("unexpected data source: %#v", data.Source)
	}
	if data.Name == nil || *data.Name != "image" {
		t.Fatalf("data block name not set: %#v", data.Name)
	}
	if result.State != message.ToolResultRunning {
		t.Fatalf("empty tool result state should default to running, got %q", result.State)
	}

	clonedCall := call.Clone().(*message.ToolCallBlock)
	clonedCall.Extra["x"].(map[string]any)["nested"] = "changed"
	if call.Extra["x"].(map[string]any)["nested"] != "y" {
		t.Fatalf("tool call clone mutated original: %#v", call.Extra)
	}
	clonedData := data.Clone().(*message.DataBlock)
	*clonedData.Name = "changed"
	if *data.Name != "image" {
		t.Fatalf("data clone mutated original name: %q", *data.Name)
	}
}
