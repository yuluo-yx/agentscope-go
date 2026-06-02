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

	"github.com/yuluo-yx/agentscope-go/utils"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Message struct {
	Name       string           `json:"name"`
	Content    ContentBlockList `json:"content"`
	Role       Role             `json:"role"`
	ID         string           `json:"id"`
	Metadata   map[string]any   `json:"metadata,omitempty"`
	CreatedAt  string           `json:"created_at"`
	FinishedAt *string          `json:"finished_at,omitempty"`
	Usage      *Usage           `json:"usage,omitempty"`
}

type MessageOption func(*Message)

func WithMessageID(id string) MessageOption {
	return func(m *Message) { m.ID = id }
}

func WithMessageMetadata(metadata map[string]any) MessageOption {
	return func(m *Message) { m.Metadata = utils.CloneAnyMap(metadata) }
}

func WithMessageCreatedAt(createdAt string) MessageOption {
	return func(m *Message) { m.CreatedAt = createdAt }
}

func WithMessageFinishedAt(finishedAt string) MessageOption {
	return func(m *Message) { m.FinishedAt = &finishedAt }
}

func WithMessageUsage(usage Usage) MessageOption {
	return func(m *Message) {
		cp := usage
		m.Usage = &cp
	}
}

func NewMessage(name string, role Role, content []ContentBlock, opts ...MessageOption) (*Message, error) {
	msg := &Message{
		Name:      name,
		Content:   cloneContent(content),
		Role:      role,
		ID:        newID(),
		Metadata:  map[string]any{},
		CreatedAt: nowISO(),
	}
	for _, opt := range opts {
		opt(msg)
	}
	if msg.ID == "" {
		msg.ID = newID()
	}
	if msg.CreatedAt == "" {
		msg.CreatedAt = nowISO()
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]any{}
	}
	if err := msg.Validate(); err != nil {
		return nil, err
	}
	return msg, nil
}

func NewUserMessage(name string, content any, opts ...MessageOption) (*Message, error) {
	blocks, err := toBlocks(content)
	if err != nil {
		return nil, err
	}
	msg, err := NewMessage(name, RoleUser, blocks, opts...)
	if err != nil {
		return nil, err
	}
	if msg.FinishedAt == nil {
		finishedAt := msg.CreatedAt
		msg.FinishedAt = &finishedAt
	}
	return msg, nil
}

func NewAssistantMessage(name string, content any, opts ...MessageOption) (*Message, error) {
	blocks, err := toBlocks(content)
	if err != nil {
		return nil, err
	}
	msg, err := NewMessage(name, RoleAssistant, blocks, opts...)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

func NewSystemMessage(name string, content any, opts ...MessageOption) (*Message, error) {
	blocks, err := toBlocks(content)
	if err != nil {
		return nil, err
	}
	msg, err := NewMessage(name, RoleSystem, blocks, opts...)
	if err != nil {
		return nil, err
	}
	if msg.FinishedAt == nil {
		finishedAt := msg.CreatedAt
		msg.FinishedAt = &finishedAt
	}
	return msg, nil
}

func MustAssistantMessage(name string, content any, opts ...MessageOption) *Message {
	msg, err := NewAssistantMessage(name, content, opts...)
	if err != nil {
		panic(err)
	}
	return msg
}

func MustSystemMessage(name string, content any, opts ...MessageOption) *Message {
	msg, err := NewSystemMessage(name, content, opts...)
	if err != nil {
		panic(err)
	}
	return msg
}

func (m *Message) Validate() error {
	switch m.Role {
	case RoleUser:
		for _, block := range m.Content {
			switch block.BlockType() {
			case "text", "data":
			default:
				return fmt.Errorf("message: user message can only contain text or data blocks, got %s", block.BlockType())
			}
		}
	case RoleSystem:
		for _, block := range m.Content {
			if block.BlockType() != "text" {
				return fmt.Errorf("message: system message can only contain text blocks, got %s", block.BlockType())
			}
		}
	case RoleAssistant:
	default:
		return fmt.Errorf("message: unsupported role %q", m.Role)
	}
	return nil
}

func (m *Message) HasContentBlocks(types ...string) bool {
	if m == nil {
		return false
	}
	return m.Content.HasContentBlocks(types...)
}

func (m *Message) GetTextContent(separator ...string) *string {
	if m == nil {
		return nil
	}
	return m.Content.GetTextContent(separator...)
}

func (m *Message) GetContentBlocks(types ...string) []ContentBlock {
	if m == nil {
		return nil
	}
	return m.Content.GetContentBlocks(types...)
}

func (m *Message) FindBlock(blockType, blockID string) ContentBlock {
	if m == nil {
		return nil
	}
	return m.Content.FindBlock(blockType, blockID)
}

func (m *Message) Clone() *Message {
	cp := *m
	cp.Metadata = utils.CloneAnyMap(m.Metadata)
	cp.Content = cloneContent(m.Content)
	if m.FinishedAt != nil {
		finishedAt := *m.FinishedAt
		cp.FinishedAt = &finishedAt
	}
	if m.Usage != nil {
		usage := *m.Usage
		cp.Usage = &usage
	}
	return &cp
}

func toBlocks(content any) ([]ContentBlock, error) {
	switch value := content.(type) {
	case nil:
		return nil, nil
	case string:
		return []ContentBlock{NewTextBlock(value)}, nil
	case []ContentBlock:
		return value, nil
	case ContentBlockList:
		return []ContentBlock(value), nil
	case []*TextBlock:
		blocks := make([]ContentBlock, 0, len(value))
		for _, block := range value {
			blocks = append(blocks, block)
		}
		return blocks, nil
	default:
		return nil, fmt.Errorf("message: unsupported content type %T", content)
	}
}

func cloneContent(blocks []ContentBlock) ContentBlockList {
	if blocks == nil {
		return nil
	}
	out := make(ContentBlockList, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, block.Clone())
	}
	return out
}
