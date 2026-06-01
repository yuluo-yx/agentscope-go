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
	"strings"

	"github.com/yuluo-yx/agentscope-go/permission"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// ContentBlock is the sealed interface for all message content blocks.
type ContentBlock interface {
	// BlockType returns the discriminator used for JSON encoding, decoding, and event replay.
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

// HasContentBlocks 判断内容块列表是否包含指定类型的内容块；未传类型时判断列表是否非空。
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

// GetTextContent 返回内容块列表中的文本块内容；不存在文本块时返回 nil。
func (l ContentBlockList) GetTextContent(separator string) *string {
	var builder strings.Builder
	found := false
	for _, block := range l {
		text, ok := block.(*TextBlock)
		if !ok || text == nil {
			continue
		}
		if found {
			builder.WriteString(separator)
		}
		builder.WriteString(text.Text)
		found = true
	}
	if !found {
		return nil
	}
	out := builder.String()
	return &out
}

// GetContentBlocks 返回匹配类型的内容块；未传类型时返回所有内容块的浅拷贝。
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

// FindBlock 按内容块类型和 ID 查找内容块；未找到时返回 nil。
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

type TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	ID   string `json:"id"`
}

type ThinkingBlock struct {
	Type     string         `json:"type"`
	Thinking string         `json:"thinking"`
	ID       string         `json:"id"`
	Extra    map[string]any `json:"-"`
}

type HintBlock struct {
	Type string `json:"type"`
	Hint string `json:"hint"`
	ID   string `json:"id"`
}

type ToolCallState string

const (
	ToolCallPending   ToolCallState = "pending"
	ToolCallAsking    ToolCallState = "asking"
	ToolCallAllowed   ToolCallState = "allowed"
	ToolCallSubmitted ToolCallState = "submitted"
	ToolCallFinished  ToolCallState = "finished"
)

type ToolCallBlock struct {
	Type           string            `json:"type"`
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Input          string            `json:"input"`
	State          ToolCallState     `json:"state"`
	SuggestedRules []permission.Rule `json:"suggested_rules"`
	Extra          map[string]any    `json:"-"`
}

type ToolResultState string

const (
	ToolResultSuccess     ToolResultState = "success"
	ToolResultError       ToolResultState = "error"
	ToolResultInterrupted ToolResultState = "interrupted"
	ToolResultDenied      ToolResultState = "denied"
	ToolResultRunning     ToolResultState = "running"
)

type ToolResultBlock struct {
	Type   string           `json:"type"`
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Output ToolResultOutput `json:"output"`
	State  ToolResultState  `json:"state"`
}

type ToolResultOutput struct {
	Raw    string
	Blocks ContentBlockList
}

type DataBlock struct {
	Type   string     `json:"type"`
	ID     string     `json:"id"`
	Source DataSource `json:"source"`
	Name   *string    `json:"name"`
}

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

func NewHintBlock(hint string, opts ...HintBlockOption) *HintBlock {
	block := &HintBlock{Type: "hint", Hint: hint, ID: newID()}
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

func NewToolResultBlock(id, name string, output ToolResultOutput, state ToolResultState) *ToolResultBlock {
	if state == "" {
		state = ToolResultRunning
	}
	return &ToolResultBlock{
		Type:   "tool_result",
		ID:     id,
		Name:   name,
		Output: output,
		State:  state,
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
