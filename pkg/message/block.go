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
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// ContentBlock is a core abstraction in the messaging system.
// It serves three responsibilities:
//  1. Enforcing type safety.
//  2. Defining the Type method used as a discriminator for JSON
//     serialization and deserialization.
//  3. Ensuring block updates in streaming events correctly
//     identify the target block.
type ContentBlock interface {
	// BlockType returns the discriminator used for JSON encoding, decoding, and event replay.
	// text, thinking, hint, tool_call, tool_result, data, etc.
	BlockType() string
	// BlockID returns the stable block identifier used by streaming events to update this block.
	BlockID() string
	// Clone returns a deep copy so messages can be reused across state, model, and tool layers safely.
	Clone() ContentBlock
	// contentBlock seals the interface to the message package's known block implementations.
	contentBlock()
}

// ContentBlockList is a discriminated list decoded by the type field.
type ContentBlockList []ContentBlock

// Clone returns a deep copy of the content block list.
func (l ContentBlockList) Clone() ContentBlockList {
	if l == nil {
		return nil
	}
	out := make(ContentBlockList, 0, len(l))
	for _, block := range l {
		if block == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, block.Clone())
	}

	return out
}

// HasContentBlocks reports whether the list contains any block of the requested types.
func (l ContentBlockList) HasContentBlocks(types ...string) bool {
	if len(types) == 0 {
		return len(l) > 0
	}
	for _, block := range l {
		if block == nil {
			continue
		}
		for _, typ := range types {
			if block.BlockType() == typ {
				return true
			}
		}
	}

	return false
}

// GetTextContent returns concatenated TextBlock content, using "\n" as the default separator.
func (l ContentBlockList) GetTextContent(separator ...string) *string {
	// DefaultDelimiter is the default message delimiter used to
	// assemble fragmented LLM responses into a complete output.
	const defaultSep = "\n"

	var (
		sep     = defaultSep
		builder strings.Builder
		found   bool
		out     string
	)

	if len(separator) > 0 {
		sep = separator[0]
	}

	for _, block := range l {
		text, ok := block.(*TextBlock)
		if !ok || text == nil {
			continue
		}
		if found {
			builder.WriteString(sep)
		}
		builder.WriteString(text.Text)
		found = true
	}
	if !found {
		return nil
	}
	out = builder.String()

	return &out
}

// GetContentBlocks filters the message's content blocks by type.
// When called with no arguments, it returns a copy of all blocks.
// Supported types: tool_call, text, thinking, etc.
func (l ContentBlockList) GetContentBlocks(types ...string) []ContentBlock {
	if len(types) == 0 {
		return append([]ContentBlock(nil), l...)
	}

	var out []ContentBlock

	for _, block := range l {
		if block == nil {
			continue
		}
		for _, typ := range types {
			if block.BlockType() == typ {
				out = append(out, block)
				break
			}
		}
	}

	return out
}

// FindBlock returns the block matching the given type and ID, or nil when no block matches.
func (l ContentBlockList) FindBlock(blockType, blockID string) ContentBlock {
	for _, block := range l {
		if block == nil {
			continue
		}
		if block.BlockType() == blockType && block.BlockID() == blockID {
			return block
		}
	}

	return nil
}

// DataSource defines the interface for multimodal data sources
// within a DataBlock. It has two implementations:
//  1. Base64-encoded binary data.
//  2. URL-based resources.
type DataSource interface {
	// SourceType returns the discriminator for the concrete data source representation.
	SourceType() string
	// Clone returns a deep copy of the source payload or reference metadata.
	Clone() DataSource
	// dataSource seals the interface to the message package's known data source implementations.
	dataSource()
}

type Base64Source struct {
	Type      string `json:"type"`
	Data      string `json:"data"`
	MediaType string `json:"media_type"`
}

type URLSource struct {
	Type      string `json:"type"`
	URL       string `json:"url"`
	MediaType string `json:"media_type"`
}

type (
	// TextBlockOption configures optional TextBlock fields.
	TextBlockOption func(*TextBlock)
	// ThinkingBlockOption configures optional ThinkingBlock fields.
	ThinkingBlockOption func(*ThinkingBlock)
	// HintBlockOption configures optional HintBlock fields.
	HintBlockOption func(*HintBlock)
	// DataBlockOption configures optional DataBlock fields.
	DataBlockOption func(*DataBlock)
	// ToolCallBlockOption configures optional ToolCallBlock fields.
	ToolCallBlockOption func(*ToolCallBlock)
)

func WithBlockID(id string) TextBlockOption {
	return func(b *TextBlock) { b.ID = id }
}

func WithThinkingBlockID(id string) ThinkingBlockOption {
	return func(b *ThinkingBlock) { b.ID = id }
}

func WithHintBlockID(id string) HintBlockOption {
	return func(b *HintBlock) { b.ID = id }
}

func WithHintSource(source string) HintBlockOption {
	return func(b *HintBlock) { b.Source = cloneString(source) }
}

func WithDataBlockID(id string) DataBlockOption {
	return func(b *DataBlock) { b.ID = id }
}

func WithDataBlockName(name string) DataBlockOption {
	return func(b *DataBlock) { b.Name = &name }
}

func WithExtra(key string, value any) ThinkingBlockOption {
	return func(b *ThinkingBlock) {
		if b.Extra == nil {
			b.Extra = map[string]any{}
		}
		b.Extra[key] = value
	}
}

func WithToolCallState(state ToolCallState) ToolCallBlockOption {
	return func(b *ToolCallBlock) { b.State = state }
}

func WithSuggestedRules(rules []permission.Rule) ToolCallBlockOption {
	return func(b *ToolCallBlock) {
		b.SuggestedRules = append([]permission.Rule(nil), rules...)
	}
}

func WithToolCallExtra(key string, value any) ToolCallBlockOption {
	return func(b *ToolCallBlock) {
		if b.Extra == nil {
			b.Extra = map[string]any{}
		}
		b.Extra[key] = value
	}
}

func NewTextBlock(text string, opts ...TextBlockOption) *TextBlock {
	block := &TextBlock{Type: "text", Text: text, ID: newID()}
	for _, opt := range opts {
		opt(block)
	}
	return block
}

func NewThinkingBlock(thinking string, opts ...ThinkingBlockOption) *ThinkingBlock {
	block := &ThinkingBlock{Type: "thinking", Thinking: thinking, ID: newID()}
	for _, opt := range opts {
		opt(block)
	}
	return block
}

func NewHintBlock(hint any, opts ...HintBlockOption) *HintBlock {
	text, blocks := normalizeHintContent(hint)
	block := &HintBlock{Type: "hint", Hint: text, Blocks: blocks, ID: newID()}
	for _, opt := range opts {
		opt(block)
	}
	return block
}

func NewToolCallBlock(id, name, input string, opts ...ToolCallBlockOption) *ToolCallBlock {
	block := &ToolCallBlock{
		Type:           "tool_call",
		ID:             id,
		Name:           name,
		Input:          input,
		State:          ToolCallPending,
		SuggestedRules: []permission.Rule{},
	}
	for _, opt := range opts {
		opt(block)
	}
	return block
}

func NewToolResultBlock(id, name string, output ToolResultOutput, state ...ToolResultState) *ToolResultBlock {
	resultState := ToolResultRunning
	if len(state) > 0 && state[0] != "" {
		resultState = state[0]
	}
	return &ToolResultBlock{
		Type:   "tool_result",
		ID:     id,
		Name:   name,
		Output: output,
		State:  resultState,
	}
}

func NewDataBlock(source DataSource, opts ...DataBlockOption) *DataBlock {
	block := &DataBlock{Type: "data", ID: newID(), Source: source}
	for _, opt := range opts {
		opt(block)
	}
	return block
}

func NewBase64Source(data, mediaType string) *Base64Source {
	return &Base64Source{Type: "base64", Data: data, MediaType: mediaType}
}

func NewURLSource(url, mediaType string) *URLSource {
	return &URLSource{Type: "url", URL: url, MediaType: mediaType}
}

func (*TextBlock) contentBlock()       {}
func (*ThinkingBlock) contentBlock()   {}
func (*HintBlock) contentBlock()       {}
func (*ToolCallBlock) contentBlock()   {}
func (*ToolResultBlock) contentBlock() {}
func (*DataBlock) contentBlock()       {}

func (b *TextBlock) BlockType() string       { return "text" }
func (b *ThinkingBlock) BlockType() string   { return "thinking" }
func (b *HintBlock) BlockType() string       { return "hint" }
func (b *ToolCallBlock) BlockType() string   { return "tool_call" }
func (b *ToolResultBlock) BlockType() string { return "tool_result" }
func (b *DataBlock) BlockType() string       { return "data" }

func (b *TextBlock) BlockID() string       { return b.ID }
func (b *ThinkingBlock) BlockID() string   { return b.ID }
func (b *HintBlock) BlockID() string       { return b.ID }
func (b *ToolCallBlock) BlockID() string   { return b.ID }
func (b *ToolResultBlock) BlockID() string { return b.ID }
func (b *DataBlock) BlockID() string       { return b.ID }

func (b *TextBlock) Clone() ContentBlock {
	cp := *b
	return &cp
}

func (b *ThinkingBlock) Clone() ContentBlock {
	cp := *b
	cp.Extra = utils.CloneAnyMap(b.Extra)
	return &cp
}

func (b *HintBlock) Clone() ContentBlock {
	cp := *b
	cp.Blocks = b.Blocks.Clone()
	if b.Source != nil {
		cp.Source = cloneString(*b.Source)
	}
	return &cp
}

func (b *ToolCallBlock) Clone() ContentBlock {
	cp := *b
	cp.SuggestedRules = append([]permission.Rule(nil), b.SuggestedRules...)
	cp.Extra = utils.CloneAnyMap(b.Extra)
	return &cp
}

func (b *ToolResultBlock) Clone() ContentBlock {
	cp := *b
	cp.Output = b.Output.Clone()
	return &cp
}

func (b *DataBlock) Clone() ContentBlock {
	cp := *b
	if b.Name != nil {
		name := *b.Name
		cp.Name = &name
	}
	if b.Source != nil {
		cp.Source = b.Source.Clone()
	}
	return &cp
}

func (*Base64Source) dataSource() {}
func (*URLSource) dataSource()    {}

func (s *Base64Source) SourceType() string { return "base64" }
func (s *URLSource) SourceType() string    { return "url" }

func (s *Base64Source) Clone() DataSource {
	cp := *s
	return &cp
}

func (s *URLSource) Clone() DataSource {
	cp := *s
	return &cp
}

func cloneString(value string) *string {
	cp := value
	return &cp
}

func normalizeHintContent(hint any) (string, ContentBlockList) {
	switch typed := hint.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case ContentBlockList:
		return "", typed.Clone()
	case []ContentBlock:
		return "", ContentBlockList(typed).Clone()
	case *TextBlock:
		return "", ContentBlockList{typed.Clone()}
	case *DataBlock:
		return "", ContentBlockList{typed.Clone()}
	default:
		return fmt.Sprint(typed), nil
	}
}

func (o ToolResultOutput) Clone() ToolResultOutput {
	cp := ToolResultOutput{Raw: o.Raw}
	if o.Blocks != nil {
		cp.Blocks = make(ContentBlockList, 0, len(o.Blocks))
		for _, block := range o.Blocks {
			cp.Blocks = append(cp.Blocks, block.Clone())
		}
	}
	return cp
}
