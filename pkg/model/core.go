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

package model

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// ChatResponseKind represents the model response type.
type ChatResponseKind string

const (
	// ChatResponseType is the fixed type for regular chat responses.
	ChatResponseType ChatResponseKind = "chat_response"
	// StructuredResponseType is the fixed type for structured responses.
	StructuredResponseType ChatResponseKind = "structured_response"
)

// UsageType represents the model usage record type.
type UsageType string

const (
	// UsageTypeChat is the usage type for chat model calls.
	UsageTypeChat UsageType = "chat"
)

// ChatUsage records token counts and duration for one chat model call.
type ChatUsage struct {
	InputTokens              int            `json:"input_tokens"`
	OutputTokens             int            `json:"output_tokens"`
	Time                     time.Duration  `json:"time"`
	CacheCreationInputTokens int            `json:"cache_creation_input_tokens"`
	CacheInputTokens         int            `json:"cache_input_tokens"`
	Type                     UsageType      `json:"type"`
	Metadata                 map[string]any `json:"metadata,omitempty"`
}

// Clone returns a deep copy of usage information.
func (u *ChatUsage) Clone() *ChatUsage {
	if u == nil {
		return nil
	}
	cp := *u
	if cp.Type == "" {
		cp.Type = UsageTypeChat
	}
	cp.Metadata = utils.CloneAnyMap(u.Metadata)
	return &cp
}

// ChatResponse is the unified model response made of message content blocks.
type ChatResponse struct {
	Content   message.ContentBlockList `json:"content"`
	IsLast    bool                     `json:"is_last"`
	Error     error                    `json:"-"`
	ID        string                   `json:"id"`
	CreatedAt string                   `json:"created_at"`
	Type      ChatResponseKind         `json:"type"`
	Usage     *ChatUsage               `json:"usage,omitempty"`
	Metadata  map[string]any           `json:"metadata,omitempty"`
}

// ChatResponseOption configures optional chat response fields.
type ChatResponseOption func(*ChatResponse)

// WithChatResponseID sets the model response ID.
func WithChatResponseID(id string) ChatResponseOption {
	return func(resp *ChatResponse) {
		resp.ID = id
	}
}

// WithChatResponseCreatedAt sets the model response creation time.
func WithChatResponseCreatedAt(createdAt string) ChatResponseOption {
	return func(resp *ChatResponse) {
		resp.CreatedAt = createdAt
	}
}

// WithChatResponseUsage sets model response usage.
func WithChatResponseUsage(usage *ChatUsage) ChatResponseOption {
	return func(resp *ChatResponse) {
		resp.Usage = usage.Clone()
	}
}

// WithChatResponseMetadata sets model response metadata.
func WithChatResponseMetadata(metadata map[string]any) ChatResponseOption {
	return func(resp *ChatResponse) {
		resp.Metadata = utils.CloneAnyMap(metadata)
	}
}

// WithChatResponseError sets an asynchronous stream error carried by a terminal chunk.
func WithChatResponseError(err error) ChatResponseOption {
	return func(resp *ChatResponse) {
		resp.Error = err
	}
}

// NewChatResponse creates a model response with default ID, time, and type.
func NewChatResponse(content message.ContentBlockList, isLast bool, opts ...ChatResponseOption) *ChatResponse {
	resp := &ChatResponse{
		Content:   content.Clone(),
		IsLast:    isLast,
		ID:        utils.NewID(),
		CreatedAt: nowRFC3339Nano(),
		Type:      ChatResponseType,
		Metadata:  map[string]any{},
	}
	for _, opt := range opts {
		opt(resp)
	}
	if resp.ID == "" {
		resp.ID = utils.NewID()
	}
	if resp.CreatedAt == "" {
		resp.CreatedAt = nowRFC3339Nano()
	}
	if resp.Type == "" {
		resp.Type = ChatResponseType
	}
	if resp.Metadata == nil {
		resp.Metadata = map[string]any{}
	}
	if resp.Usage != nil && resp.Usage.Type == "" {
		resp.Usage.Type = UsageTypeChat
	}
	return resp
}

// Clone returns a deep copy of the model response.
func (r *ChatResponse) Clone() *ChatResponse {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Content = r.Content.Clone()
	cp.Usage = r.Usage.Clone()
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	return &cp
}

// GetTextContent returns concatenated text blocks from the response content.
func (r *ChatResponse) GetTextContent(separator ...string) *string {
	if r == nil {
		return nil
	}
	return r.Content.GetTextContent(separator...)
}

// HasContentBlocks reports whether the response contains any block of the requested types.
func (r *ChatResponse) HasContentBlocks(types ...string) bool {
	if r == nil {
		return false
	}
	return r.Content.HasContentBlocks(types...)
}

// GetContentBlocks returns matching response blocks, or all response blocks when no type is provided.
func (r *ChatResponse) GetContentBlocks(types ...string) []message.ContentBlock {
	if r == nil {
		return nil
	}
	return r.Content.GetContentBlocks(types...)
}

// FindBlock returns the response block matching the given type and ID.
func (r *ChatResponse) FindBlock(blockType, blockID string) message.ContentBlock {
	if r == nil {
		return nil
	}
	return r.Content.FindBlock(blockType, blockID)
}

// StructuredResponse is a structured-output model response.
type StructuredResponse struct {
	Content   map[string]any   `json:"content"`
	ID        string           `json:"id"`
	CreatedAt string           `json:"created_at"`
	Type      ChatResponseKind `json:"type"`
	Usage     *ChatUsage       `json:"usage,omitempty"`
	Metadata  map[string]any   `json:"metadata,omitempty"`
}

// CallRequest is the unified input for one model call.
type CallRequest struct {
	Messages   []*message.Message `json:"messages"`
	Tools      []ToolSchema       `json:"tools,omitempty"`
	ToolChoice *types.ToolChoice  `json:"tool_choice,omitempty"`
	Stream     bool               `json:"stream,omitempty"`
	Metadata   map[string]any     `json:"metadata,omitempty"`
	Parameters map[string]any     `json:"parameters,omitempty"`
}

// Clone returns a deep copy of the model call request.
func (r CallRequest) Clone() CallRequest {
	cp := r
	if r.Messages != nil {
		cp.Messages = make([]*message.Message, 0, len(r.Messages))
		for _, msg := range r.Messages {
			if msg == nil {
				cp.Messages = append(cp.Messages, nil)
				continue
			}
			cp.Messages = append(cp.Messages, msg.Clone())
		}
	}
	cp.Tools = cloneToolSchemas(r.Tools)
	cp.ToolChoice = r.ToolChoice.Clone()
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	cp.Parameters = utils.CloneAnyMap(r.Parameters)
	return cp
}

// ChatModel is the core interface implemented by all chat model providers.
type ChatModel interface {
	// Name returns a provider-qualified model name for logs, events, and diagnostics.
	Name() string
	// Call performs a non-streaming model request and returns a complete response.
	// Implementations should respect context cancellation and normalize provider errors.
	Call(context.Context, CallRequest) (*ChatResponse, error)
	// Stream performs a streaming model request and returns response chunks.
	// Implementations should emit zero or more IsLast=false delta chunks, then one IsLast=true
	// complete response containing the accumulated content and usage when available. If the
	// provider stream fails after the channel has been returned, implementations should emit
	// one IsLast=true response with Error set and avoid sending a successful final response.
	Stream(context.Context, CallRequest) (<-chan ChatResponse, error)
	// CountTokens estimates or calculates the token count for messages and tool schemas.
	// Providers may use an SDK tokenizer when available, or a documented approximation otherwise.
	CountTokens(CallRequest) (int, error)
}

// FunctionSchema is the core OpenAI-compatible function tool schema.
type FunctionSchema struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Parameters  types.JSONSchema `json:"parameters,omitempty"`
}

// ToolSchema is the tool schema passed to model providers.
type ToolSchema struct {
	Type     string         `json:"type"`
	Function FunctionSchema `json:"function"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ApproximateTokenCount estimates message and tool schema tokens using Python-compatible rough rules.
func ApproximateTokenCount(messages []*message.Message, tools []ToolSchema) int {
	total := 0
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		for _, block := range msg.Content {
			total += approximateBlockTokens(block)
		}
	}
	for _, tool := range tools {
		data, err := json.Marshal(tool)
		if err != nil {
			continue
		}
		total += approximateBytes(len(data))
	}
	return total
}

func approximateBlockTokens(block message.ContentBlock) int {
	switch typed := block.(type) {
	case *message.TextBlock:
		return approximateBytes(len([]byte(typed.Text)))
	case *message.ThinkingBlock:
		return approximateBytes(len([]byte(typed.Thinking)))
	case *message.HintBlock:
		if typed.Blocks != nil {
			total := 0
			for _, block := range typed.Blocks {
				total += approximateBlockTokens(block)
			}
			return total
		}
		return approximateBytes(len([]byte(typed.Hint)))
	case *message.ToolCallBlock:
		return approximateBytes(len([]byte(typed.Name)) + len([]byte(typed.Input)))
	case *message.ToolResultBlock:
		return approximateBytes(len([]byte(typed.Output.Raw)))
	case *message.DataBlock:
		if typed.Source == nil {
			return 0
		}
		switch source := typed.Source.(type) {
		case *message.Base64Source:
			return approximateBytes(len(source.Data))
		case *message.URLSource:
			return approximateBytes(len(source.URL))
		default:
			return 0
		}
	default:
		return 0
	}
}

func approximateBytes(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return int(math.Ceil(float64(bytes) / 4))
}

func cloneToolSchemas(in []ToolSchema) []ToolSchema {
	if in == nil {
		return nil
	}
	out := make([]ToolSchema, len(in))
	for index, schema := range in {
		out[index] = schema
		out[index].Function.Parameters = utils.CloneAnyMap(schema.Function.Parameters)
		out[index].Metadata = utils.CloneAnyMap(schema.Metadata)
	}
	return out
}

func nowRFC3339Nano() string {
	return time.Now().Format(time.RFC3339Nano)
}
