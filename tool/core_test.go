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

package tool_test

import (
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	toolpkg "github.com/yuluo-yx/agentscope-go/tool"
)

func TestToolResponseAppendChunkMergesTextAndBase64Data(t *testing.T) {
	t.Parallel()

	response := toolpkg.NewToolResponse("tool-call-1")
	first := toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{
			message.NewTextBlock("hello ", message.WithBlockID("text-1")),
			message.NewDataBlock(message.NewBase64Source("abc", "image/png"), message.WithDataBlockID("data-1")),
		},
		toolpkg.WithToolChunkMetadata(map[string]any{"a": 1}),
	)
	second := toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{
			message.NewTextBlock("world", message.WithBlockID("text-1")),
			message.NewDataBlock(message.NewBase64Source("def", "image/png"), message.WithDataBlockID("data-1"), message.WithDataBlockName("image")),
		},
		toolpkg.WithToolChunkState(message.ToolResultError),
		toolpkg.WithToolChunkMetadata(map[string]any{"b": 2}),
	)

	if err := response.AppendChunk(first); err != nil {
		t.Fatalf("AppendChunk first returned error: %v", err)
	}
	if err := response.AppendChunk(second); err != nil {
		t.Fatalf("AppendChunk second returned error: %v", err)
	}

	if response.State != message.ToolResultError {
		t.Fatalf("error state should be preserved, got %q", response.State)
	}
	if len(response.Content) != 2 {
		t.Fatalf("expected merged text and data blocks, got %d: %#v", len(response.Content), response.Content)
	}
	if got := response.GetTextContent(""); got == nil || *got != "hello world" {
		t.Fatalf("unexpected response text content: %#v", got)
	}
	data := response.Content[1].(*message.DataBlock)
	if got := data.Source.(*message.Base64Source).Data; got != "abcdef" {
		t.Fatalf("base64 chunks not merged: %q", got)
	}
	if data.Name == nil || *data.Name != "image" {
		t.Fatalf("latest data block name not preserved: %#v", data.Name)
	}
	if response.Metadata["a"] != 1 || response.Metadata["b"] != 2 {
		t.Fatalf("metadata not merged: %#v", response.Metadata)
	}
}

func TestToolResponseRejectsURLDataAppendWithSameID(t *testing.T) {
	t.Parallel()

	response := toolpkg.NewToolResponse("tool-call-1")
	if err := response.AppendChunk(toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{message.NewDataBlock(message.NewURLSource("https://example.com/a", "image/png"), message.WithDataBlockID("data-1"))},
	)); err != nil {
		t.Fatalf("AppendChunk returned error: %v", err)
	}

	err := response.AppendChunk(toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{message.NewDataBlock(message.NewURLSource("https://example.com/b", "image/png"), message.WithDataBlockID("data-1"))},
	))
	if err == nil {
		t.Fatal("URL data blocks with the same id should not be appended")
	}
}

func TestToolChunkAndResponseClone(t *testing.T) {
	t.Parallel()

	chunk := toolpkg.NewToolChunk(
		"",
		message.ContentBlockList{message.NewTextBlock("hello")},
		toolpkg.WithToolChunkIsLast(false),
	)
	if chunk.ID == "" || chunk.IsLast {
		t.Fatalf("chunk defaults/options not applied: %#v", chunk)
	}
	clonedChunk := chunk.Clone()
	clonedChunk.Content[0].(*message.TextBlock).Text = "changed"
	if text := chunk.Content.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("chunk clone mutated original: %#v", chunk)
	}
	if (*toolpkg.ToolChunk)(nil).Clone() != nil {
		t.Fatal("nil chunk clone should return nil")
	}

	response := toolpkg.NewToolResponse("")
	if response.ID == "" {
		t.Fatalf("empty response id should be generated: %#v", response)
	}
	if err := response.AppendChunk(nil); err != nil {
		t.Fatalf("nil chunk should be ignored: %v", err)
	}
	if (*toolpkg.ToolResponse)(nil).Clone() != nil {
		t.Fatal("nil response clone should return nil")
	}
}

func TestToolResponseAppendChunkHandlesConflictingBlockTypes(t *testing.T) {
	t.Parallel()

	response := toolpkg.NewToolResponse("tool-call-1")
	if err := response.AppendChunk(toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{message.NewTextBlock("hello", message.WithBlockID("same"))},
	)); err != nil {
		t.Fatalf("AppendChunk text returned error: %v", err)
	}
	if err := response.AppendChunk(toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{message.NewDataBlock(message.NewBase64Source("abc", "image/png"), message.WithDataBlockID("same"))},
		toolpkg.WithToolChunkState(message.ToolResultDenied),
	)); err != nil {
		t.Fatalf("AppendChunk data returned error: %v", err)
	}
	if len(response.Content) != 2 {
		t.Fatalf("conflicting block types should append a new block, got %#v", response.Content)
	}
	if response.Content[1].BlockID() == "same" {
		t.Fatalf("conflicting appended block should get a new id: %#v", response.Content[1])
	}
	if response.State != message.ToolResultDenied {
		t.Fatalf("denied state should be retained, got %q", response.State)
	}
	cloned := response.Clone()
	cloned.Content[0].(*message.TextBlock).Text = "changed"
	if text := response.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("response clone mutated original: %#v", response)
	}
}

func TestToolResponseAppendChunkCoversStateAndSourceEdges(t *testing.T) {
	t.Parallel()

	chunk := toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{message.NewTextBlock("hello")},
		toolpkg.WithToolChunkState(""),
	)
	if chunk.State != message.ToolResultRunning {
		t.Fatalf("empty chunk state should default to running, got %q", chunk.State)
	}

	response := toolpkg.NewToolResponse("tool-call-1")
	if err := response.AppendChunk(toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{message.NewDataBlock(message.NewBase64Source("abc", "image/png"), message.WithDataBlockID("data-1"))},
	)); err != nil {
		t.Fatalf("AppendChunk base64 returned error: %v", err)
	}
	err := response.AppendChunk(toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{message.NewDataBlock(message.NewURLSource("https://example.com/a", "image/png"), message.WithDataBlockID("data-1"))},
		toolpkg.WithToolChunkState(message.ToolResultSuccess),
	))
	if err == nil {
		t.Fatal("base64 target should reject URL chunk with the same id")
	}

	response = toolpkg.NewToolResponse("tool-call-2")
	if err := response.AppendChunk(toolpkg.NewToolChunk(
		"tool-call-2",
		message.ContentBlockList{
			message.NewDataBlock(message.NewBase64Source("abc", "image/png"), message.WithDataBlockID("data-1")),
			message.NewTextBlock(" tail", message.WithBlockID("text-1")),
		},
		toolpkg.WithToolChunkState(message.ToolResultInterrupted),
	)); err != nil {
		t.Fatalf("AppendChunk returned error: %v", err)
	}
	if err := response.AppendChunk(toolpkg.NewToolChunk("tool-call-2", nil, toolpkg.WithToolChunkState(message.ToolResultSuccess))); err != nil {
		t.Fatalf("success chunk should not lower interrupted state: %v", err)
	}
	if response.State != message.ToolResultInterrupted {
		t.Fatalf("interrupted state should not be lowered by success, got %q", response.State)
	}
}

func TestToolResponseAssignsNewIDsForAllConflictingBlockKinds(t *testing.T) {
	t.Parallel()

	response := toolpkg.NewToolResponse("tool-call-1")
	if err := response.AppendChunk(toolpkg.NewToolChunk(
		"tool-call-1",
		message.ContentBlockList{message.NewTextBlock("base", message.WithBlockID("same"))},
	)); err != nil {
		t.Fatalf("AppendChunk base returned error: %v", err)
	}

	conflicts := message.ContentBlockList{
		message.NewThinkingBlock("think", message.WithThinkingBlockID("same")),
		message.NewHintBlock("hint", message.WithHintBlockID("same")),
		message.NewToolCallBlock("same", "Read", "{}"),
		message.NewToolResultBlock("same", "Read", message.ToolResultOutput{Raw: "ok"}, message.ToolResultSuccess),
	}
	for _, block := range conflicts {
		if err := response.AppendChunk(toolpkg.NewToolChunk("tool-call-1", message.ContentBlockList{block})); err != nil {
			t.Fatalf("AppendChunk conflict returned error: %v", err)
		}
		if response.Content[len(response.Content)-1].BlockID() == "same" {
			t.Fatalf("conflicting block should get new id: %#v", response.Content[len(response.Content)-1])
		}
	}
}

func TestNilToolResponseAppendChunkReturnsError(t *testing.T) {
	t.Parallel()

	var response *toolpkg.ToolResponse
	if err := response.AppendChunk(toolpkg.NewToolChunk("id", nil)); err == nil {
		t.Fatal("nil response should return error")
	}
}
