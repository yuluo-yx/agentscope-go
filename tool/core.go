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

package tool

import (
	"context"
	"fmt"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// Tool is the common interface exposed to Agents, permission checks, and schemas.
type Tool interface {
	// Name returns the stable tool name used in model tool schemas, ToolCallBlock.Name, and permission rules.
	Name() string
	// Description returns model-facing instructions that explain when and how the tool should be used.
	Description() string
	// InputSchema returns a JSON Schema object for validating model-generated tool input.
	// Callers must treat the returned map as owned by the tool unless the implementation documents otherwise.
	InputSchema() map[string]any
	// IsConcurrencySafe reports whether the Agent may execute this tool concurrently with other safe tools.
	// Tools that mutate AgentState, files, remote resources, or shared process state should return false.
	IsConcurrencySafe() bool
	// IsReadOnly reports whether the tool only reads data and has no side effects.
	// The permission engine uses this to allow read-only tools in explore mode.
	IsReadOnly() bool
	// IsExternalTool reports whether execution must happen outside the current Go process.
	// Agents emit RequireExternalExecutionEvent instead of calling Execute for external tools.
	IsExternalTool() bool
	// IsStateInjected reports whether Execute expects a non-nil AgentState to read or mutate agent state.
	// Stateful tools should validate the provided state and return a clear error when it is missing.
	IsStateInjected() bool
	// IsMCP reports whether this tool wraps a Model Context Protocol tool.
	IsMCP() bool
	// MCPName returns the source MCP server name for MCP tools, or an empty string for local tools.
	MCPName() string
	// CheckPermissions lets the tool enforce tool-specific safety rules after global deny/ask rules.
	// The input map has already been parsed from the ToolCallBlock JSON. A decision may include
	// UpdatedInput to normalize or repair arguments before Execute receives them.
	CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error)
	// MatchRule reports whether one permission rule value matches the current input.
	// Implementations should align this with InputSchema so user-approved rules remain predictable.
	MatchRule(string, map[string]any) bool
	// GenerateSuggestions returns candidate permission rules for the current input.
	// Agents attach these suggestions to confirmation events when a tool call needs user approval.
	GenerateSuggestions(map[string]any) []permission.Rule
	// Execute runs the tool with validated input and optional AgentState.
	// It returns a stream of ToolChunk values so long-running tools can emit incremental content.
	// The channel must eventually close; the final chunk state, or success by default, becomes the ToolResultBlock state.
	// If execution fails after the channel has been returned, emit a ToolChunk with ToolResultError or ToolResultInterrupted.
	Execute(context.Context, map[string]any, *asstate.AgentState) (<-chan ToolChunk, error)
}

// ToolChunk is an incremental result from streaming tool execution.
type ToolChunk struct {
	Content  message.ContentBlockList `json:"content"`
	State    message.ToolResultState  `json:"state"`
	IsLast   bool                     `json:"is_last"`
	Metadata map[string]any           `json:"metadata,omitempty"`
	ID       string                   `json:"id"`
}

// ToolChunkOption configures a tool chunk.
type ToolChunkOption func(*ToolChunk)

// WithToolChunkID sets the tool chunk identifier.
func WithToolChunkID(id string) ToolChunkOption {
	return func(chunk *ToolChunk) {
		chunk.ID = id
	}
}

// WithToolChunkState sets the tool chunk state.
func WithToolChunkState(state message.ToolResultState) ToolChunkOption {
	return func(chunk *ToolChunk) {
		chunk.State = state
	}
}

// WithToolChunkIsLast sets whether the tool chunk is the final chunk.
func WithToolChunkIsLast(isLast bool) ToolChunkOption {
	return func(chunk *ToolChunk) {
		chunk.IsLast = isLast
	}
}

// WithToolChunkMetadata sets tool chunk metadata.
func WithToolChunkMetadata(metadata map[string]any) ToolChunkOption {
	return func(chunk *ToolChunk) {
		chunk.Metadata = utils.CloneAnyMap(metadata)
	}
}

// NewToolChunk creates a tool chunk.
func NewToolChunk(content message.ContentBlockList, opts ...ToolChunkOption) *ToolChunk {
	chunk := &ToolChunk{
		Content:  content.Clone(),
		State:    message.ToolResultRunning,
		IsLast:   true,
		Metadata: map[string]any{},
		ID:       utils.NewID(),
	}
	for _, opt := range opts {
		opt(chunk)
	}
	if chunk.ID == "" {
		chunk.ID = utils.NewID()
	}
	if chunk.State == "" {
		chunk.State = message.ToolResultRunning
	}
	if chunk.Metadata == nil {
		chunk.Metadata = map[string]any{}
	}
	return chunk
}

// Clone returns a deep copy of the tool chunk.
func (c *ToolChunk) Clone() *ToolChunk {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Content = c.Content.Clone()
	cp.Metadata = utils.CloneAnyMap(c.Metadata)
	return &cp
}

// ToolResponse is the accumulated result after one tool execution.
type ToolResponse struct {
	Content  message.ContentBlockList `json:"content"`
	State    message.ToolResultState  `json:"state"`
	Metadata map[string]any           `json:"metadata,omitempty"`
	ID       string                   `json:"id"`
}

// ToolResponseOption configures an accumulated tool response.
type ToolResponseOption func(*ToolResponse)

// WithToolResponseID sets the tool response identifier.
func WithToolResponseID(id string) ToolResponseOption {
	return func(response *ToolResponse) {
		response.ID = id
	}
}

// NewToolResponse creates an accumulated tool response.
func NewToolResponse(opts ...ToolResponseOption) *ToolResponse {
	response := &ToolResponse{
		Content:  message.ContentBlockList{},
		State:    message.ToolResultSuccess,
		Metadata: map[string]any{},
		ID:       utils.NewID(),
	}
	for _, opt := range opts {
		opt(response)
	}
	if response.ID == "" {
		response.ID = utils.NewID()
	}
	return response
}

// AppendChunk accumulates a tool chunk and merges text/base64 blocks by block ID.
func (r *ToolResponse) AppendChunk(chunk *ToolChunk) error {
	if r == nil {
		return fmt.Errorf("agentscope: nil tool response")
	}
	if chunk == nil {
		return nil
	}
	currentIDs := make(map[string]int, len(r.Content))
	for index, block := range r.Content {
		if block == nil || block.BlockID() == "" {
			continue
		}
		currentIDs[block.BlockID()] = index
	}
	for _, block := range chunk.Content {
		if block == nil {
			continue
		}
		index, ok := currentIDs[block.BlockID()]
		if !ok || block.BlockID() == "" {
			r.Content = append(r.Content, block.Clone())
			currentIDs[block.BlockID()] = len(r.Content) - 1
			continue
		}
		if err := appendContentBlock(r, index, block); err != nil {
			return err
		}
	}
	if stateRank(chunk.State) > stateRank(r.State) {
		r.State = chunk.State
	}
	if r.Metadata == nil {
		r.Metadata = map[string]any{}
	}
	for key, value := range chunk.Metadata {
		r.Metadata[key] = utils.CloneAny(value)
	}
	r.Content = mergeConsecutiveTextBlocks(r.Content)
	return nil
}

// Clone returns a deep copy of the tool response.
func (r *ToolResponse) Clone() *ToolResponse {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Content = r.Content.Clone()
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	return &cp
}

// GetTextContent returns concatenated text blocks from the tool response content.
func (r *ToolResponse) GetTextContent(separator ...string) *string {
	if r == nil {
		return nil
	}
	return r.Content.GetTextContent(separator...)
}

func appendContentBlock(response *ToolResponse, index int, chunkBlock message.ContentBlock) error {
	target := response.Content[index]
	switch targetBlock := target.(type) {
	case *message.TextBlock:
		if chunkText, ok := chunkBlock.(*message.TextBlock); ok {
			targetBlock.Text += chunkText.Text
			return nil
		}
	case *message.DataBlock:
		chunkData, ok := chunkBlock.(*message.DataBlock)
		if !ok {
			break
		}
		targetSource, ok := targetBlock.Source.(*message.Base64Source)
		if !ok {
			return fmt.Errorf("agentscope: cannot append data block with non-base64 source id %q", targetBlock.ID)
		}
		chunkSource, ok := chunkData.Source.(*message.Base64Source)
		if !ok {
			return fmt.Errorf("agentscope: cannot append data block with different source type id %q", targetBlock.ID)
		}
		targetSource.Data += chunkSource.Data
		if chunkSource.MediaType != "" {
			targetSource.MediaType = chunkSource.MediaType
		}
		if chunkData.Name != nil {
			name := *chunkData.Name
			targetBlock.Name = &name
		}
		return nil
	}
	copied := chunkBlock.Clone()
	assignNewBlockID(copied)
	response.Content = append(response.Content, copied)
	return nil
}

func assignNewBlockID(block message.ContentBlock) {
	id := utils.NewID()
	switch typed := block.(type) {
	case *message.TextBlock:
		typed.ID = id
	case *message.ThinkingBlock:
		typed.ID = id
	case *message.HintBlock:
		typed.ID = id
	case *message.ToolCallBlock:
		typed.ID = id
	case *message.ToolResultBlock:
		typed.ID = id
	case *message.DataBlock:
		typed.ID = id
	}
}

func stateRank(state message.ToolResultState) int {
	switch state {
	case message.ToolResultError:
		return 4
	case message.ToolResultInterrupted:
		return 3
	case message.ToolResultDenied:
		return 2
	case message.ToolResultSuccess:
		return 1
	default:
		return 0
	}
}

func mergeConsecutiveTextBlocks(blocks message.ContentBlockList) message.ContentBlockList {
	if len(blocks) < 2 {
		return blocks
	}
	merged := make(message.ContentBlockList, 0, len(blocks))
	for _, block := range blocks {
		text, ok := block.(*message.TextBlock)
		if !ok || len(merged) == 0 {
			merged = append(merged, block)
			continue
		}
		lastText, ok := merged[len(merged)-1].(*message.TextBlock)
		if !ok {
			merged = append(merged, block)
			continue
		}
		lastText.Text += text.Text
	}
	return merged
}
