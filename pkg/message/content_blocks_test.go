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

package message

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestContentBlockMarkersAndHintNormalization(t *testing.T) {
	t.Parallel()

	text := NewTextBlock("hello")
	thinking := NewThinkingBlock("think")
	hint := NewHintBlock("hint")
	call := NewToolCallBlock("call-1", "Bash", "{}")
	result := NewToolResultBlock("call-1", "Bash", ToolResultOutput{Raw: "ok"})
	data := NewDataBlock(NewBase64Source("YWJj", "image/png"))
	url := NewURLSource("https://example.test/image.png", "image/png")

	text.contentBlock()
	thinking.contentBlock()
	hint.contentBlock()
	call.contentBlock()
	result.contentBlock()
	data.contentBlock()
	data.Source.(*Base64Source).dataSource()
	url.dataSource()
	eventMarker{}.event()

	if normalized, blocks := normalizeHintContent(nil); normalized != "" || blocks != nil {
		t.Fatalf("nil hint normalization = %q %#v", normalized, blocks)
	}
	if normalized, blocks := normalizeHintContent("plain hint"); normalized != "plain hint" || blocks != nil {
		t.Fatalf("string hint normalization = %q %#v", normalized, blocks)
	}
	if normalized, blocks := normalizeHintContent(ContentBlockList{text, data}); normalized != "" || len(blocks) != 2 {
		t.Fatalf("block list hint normalization = %q %#v", normalized, blocks)
	}
	if normalized, blocks := normalizeHintContent([]ContentBlock{text, data}); normalized != "" || len(blocks) != 2 {
		t.Fatalf("slice hint normalization = %q %#v", normalized, blocks)
	}
	if normalized, blocks := normalizeHintContent(text); normalized != "" || len(blocks) != 1 || blocks[0].BlockType() != "text" {
		t.Fatalf("text block hint normalization = %q %#v", normalized, blocks)
	}
	if normalized, blocks := normalizeHintContent(data); normalized != "" || len(blocks) != 1 || blocks[0].BlockType() != "data" {
		t.Fatalf("data block hint normalization = %q %#v", normalized, blocks)
	}
	if normalized, blocks := normalizeHintContent(42); normalized != "42" || blocks != nil {
		t.Fatalf("fallback hint normalization = %q %#v", normalized, blocks)
	}

	rawClone := (ToolResultOutput{Raw: "raw"}).Clone()
	if rawClone.Raw != "raw" || rawClone.Blocks != nil {
		t.Fatalf("raw tool result clone mismatch: %#v", rawClone)
	}
	blockClone := (ToolResultOutput{Blocks: ContentBlockList{text}}).Clone()
	blockClone.Blocks[0].(*TextBlock).Text = "changed"
	if text.Text != "hello" {
		t.Fatalf("tool result output clone mutated original text: %q", text.Text)
	}
}

func TestContentBlockJSONErrorBranches(t *testing.T) {
	t.Parallel()

	if _, err := json.Marshal(ContentBlockList{badJSONBlock{}}); !errors.Is(err, errBadJSONBlock) {
		t.Fatalf("ContentBlockList MarshalJSON error = %v", err)
	}

	var blocks ContentBlockList
	if err := json.Unmarshal([]byte(`{}`), &blocks); err == nil {
		t.Fatalf("ContentBlockList should reject non-array JSON")
	}
	if err := json.Unmarshal([]byte(`[{"type":"text","text":7}]`), &blocks); err == nil {
		t.Fatalf("ContentBlockList should propagate block decode errors")
	}
	if _, err := UnmarshalContentBlock([]byte(`{bad`)); err == nil {
		t.Fatalf("UnmarshalContentBlock should reject invalid JSON")
	}

	var thinking ThinkingBlock
	if err := json.Unmarshal([]byte(`{"type":"thinking","thinking":7}`), &thinking); err == nil {
		t.Fatalf("ThinkingBlock should reject invalid field types")
	}
	if _, err := json.Marshal(ThinkingBlock{Type: "thinking", Thinking: "x", Extra: map[string]any{"bad": make(chan int)}}); err == nil {
		t.Fatalf("ThinkingBlock should reject unmarshalable extra values")
	}

	var hint HintBlock
	if err := json.Unmarshal([]byte(`{"type":"hint","id":7,"hint":"x"}`), &hint); err == nil {
		t.Fatalf("HintBlock should reject invalid raw field types")
	}
	if err := json.Unmarshal([]byte(`{"type":"hint","hint":[{"type":"unknown"}]}`), &hint); err == nil {
		t.Fatalf("HintBlock should reject unsupported nested blocks")
	}
	if err := json.Unmarshal([]byte(`{"type":"hint","hint":7}`), &hint); err == nil {
		t.Fatalf("HintBlock should reject non-string hint scalars")
	}

	var call ToolCallBlock
	if err := json.Unmarshal([]byte(`{"type":"tool_call","id":7}`), &call); err == nil {
		t.Fatalf("ToolCallBlock should reject invalid field types")
	}
	if _, err := json.Marshal(ToolCallBlock{Type: "tool_call", ID: "call", Name: "Bad", Extra: map[string]any{"bad": make(chan int)}}); err == nil {
		t.Fatalf("ToolCallBlock should reject unmarshalable extra values")
	}
	if err := json.Unmarshal([]byte(`{"type":"tool_call","id":"call","name":"Bash","input":"{}"}`), &call); err != nil || call.State != ToolCallPending {
		t.Fatalf("ToolCallBlock should default missing state to pending: %#v err=%v", call, err)
	}

	var data DataBlock
	if err := json.Unmarshal([]byte(`{"type":"data","id":7,"source":{"type":"url"}}`), &data); err == nil {
		t.Fatalf("DataBlock should reject invalid raw field types")
	}
	if err := json.Unmarshal([]byte(`{"type":"data","source":{"type":"unknown"}}`), &data); err == nil {
		t.Fatalf("DataBlock should reject unsupported sources")
	}
	if _, err := UnmarshalDataSource([]byte(`{bad`)); err == nil {
		t.Fatalf("UnmarshalDataSource should reject invalid JSON")
	}

	var output ToolResultOutput
	if err := json.Unmarshal([]byte(`[{"type":"unknown"}]`), &output); err == nil {
		t.Fatalf("ToolResultOutput should reject invalid block arrays")
	}

	if _, _, err := decodeHintContent(json.RawMessage(`[{"type":"unknown"}]`)); err == nil {
		t.Fatalf("decodeHintContent should reject unsupported block arrays")
	}
	if hint, blocks, err := decodeHintContent(nil); err != nil || hint != "" || blocks != nil {
		t.Fatalf("decodeHintContent nil = %q %#v %v", hint, blocks, err)
	}
	if hint, blocks, err := decodeHintContent(json.RawMessage(` null `)); err != nil || hint != "" || blocks != nil {
		t.Fatalf("decodeHintContent null = %q %#v %v", hint, blocks, err)
	}
	if _, _, err := decodeHintContent(json.RawMessage(`7`)); err == nil {
		t.Fatalf("decodeHintContent should reject non-string scalars")
	}
	if _, err := marshalWithExtra(make(chan int), nil); err == nil {
		t.Fatalf("marshalWithExtra should reject unmarshalable values")
	}
	if _, err := marshalWithExtra([]string{"not-object"}, nil); err == nil {
		t.Fatalf("marshalWithExtra should require object-shaped values")
	}
}

var errBadJSONBlock = errors.New("bad json block")

type badJSONBlock struct{}

func (badJSONBlock) BlockType() string { return "bad" }

func (badJSONBlock) BlockID() string { return "bad" }

func (badJSONBlock) Clone() ContentBlock { return badJSONBlock{} }

func (badJSONBlock) contentBlock() {}

func (badJSONBlock) MarshalJSON() ([]byte, error) { return nil, errBadJSONBlock }
