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
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// FunctionHandler handles a synchronous function tool call.
type FunctionHandler func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error)

// StreamFunctionHandler handles a streaming function tool call.
type StreamFunctionHandler func(context.Context, map[string]any, *asstate.AgentState) (<-chan ToolChunk, error)

// FunctionPermissionFunc allows a function tool to customize permission checks.
type FunctionPermissionFunc func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error)

// FunctionToolOption configures a function tool adapter.
type FunctionToolOption func(*FunctionTool)

// FunctionTool adapts Go functions to Tool.
type FunctionTool struct {
	name              string
	description       string
	inputSchema       map[string]any
	concurrencySafe   bool
	readOnly          bool
	externalTool      bool
	stateInjected     bool
	mcp               bool
	mcpName           string
	handler           StreamFunctionHandler
	checkPermissions  FunctionPermissionFunc
	suggestedRuleHint string
}

// WithFunctionReadOnly marks whether the function tool is read-only.
func WithFunctionReadOnly(readOnly bool) FunctionToolOption {
	return func(t *FunctionTool) {
		t.readOnly = readOnly
	}
}

// WithFunctionConcurrencySafe marks whether the function tool can run concurrently.
func WithFunctionConcurrencySafe(concurrencySafe bool) FunctionToolOption {
	return func(t *FunctionTool) {
		t.concurrencySafe = concurrencySafe
	}
}

// WithFunctionStateInjected marks whether the function tool requires AgentState.
func WithFunctionStateInjected(stateInjected bool) FunctionToolOption {
	return func(t *FunctionTool) {
		t.stateInjected = stateInjected
	}
}

// WithFunctionExternalTool marks whether the function tool runs in an external system.
func WithFunctionExternalTool(external bool) FunctionToolOption {
	return func(t *FunctionTool) {
		t.externalTool = external
	}
}

// WithFunctionMCP marks whether the function tool comes from an MCP service.
func WithFunctionMCP(mcpName string) FunctionToolOption {
	return func(t *FunctionTool) {
		t.mcp = true
		t.mcpName = mcpName
	}
}

// WithFunctionPermissionFunc sets the permission decision function.
func WithFunctionPermissionFunc(fn FunctionPermissionFunc) FunctionToolOption {
	return func(t *FunctionTool) {
		t.checkPermissions = fn
	}
}

// WithFunctionSuggestedRule sets the rule content used for suggested permissions.
func WithFunctionSuggestedRule(ruleContent string) FunctionToolOption {
	return func(t *FunctionTool) {
		t.suggestedRuleHint = ruleContent
	}
}

// NewFunctionTool creates a synchronous function tool. Custom tools ask by default.
func NewFunctionTool(
	name string,
	description string,
	inputSchema map[string]any,
	handler FunctionHandler,
	opts ...FunctionToolOption,
) (*FunctionTool, error) {
	if handler == nil {
		return nil, fmt.Errorf("tool: function handler is required")
	}
	streamHandler := func(ctx context.Context, input map[string]any, state *asstate.AgentState) (<-chan ToolChunk, error) {
		chunks := make(chan ToolChunk, 1)
		go func() {
			defer close(chunks)
			content, err := handler(ctx, input, state)
			if err != nil {
				chunks <- errorChunk("", err.Error())
				return
			}
			chunks <- *NewToolChunk(content, WithToolChunkState(message.ToolResultSuccess))
		}()
		return chunks, nil
	}
	return NewStreamFunctionTool(name, description, inputSchema, streamHandler, opts...)
}

// NewStreamFunctionTool creates a streaming function tool.
func NewStreamFunctionTool(
	name string,
	description string,
	inputSchema map[string]any,
	handler StreamFunctionHandler,
	opts ...FunctionToolOption,
) (*FunctionTool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tool: function tool name is required")
	}
	if handler == nil {
		return nil, fmt.Errorf("tool: stream function handler is required")
	}
	tool := &FunctionTool{
		name:            name,
		description:     strings.TrimSpace(description),
		inputSchema:     cloneSchemaOrObject(inputSchema),
		concurrencySafe: true,
		handler:         handler,
	}
	for _, opt := range opts {
		opt(tool)
	}
	return tool, nil
}

// Name returns the function tool name.
func (t *FunctionTool) Name() string {
	return t.name
}

// Description returns the function tool description.
func (t *FunctionTool) Description() string {
	return t.description
}

// InputSchema returns the function tool input JSON Schema.
func (t *FunctionTool) InputSchema() map[string]any {
	return utils.CloneAnyMap(t.inputSchema)
}

// IsConcurrencySafe reports whether the function tool can run concurrently.
func (t *FunctionTool) IsConcurrencySafe() bool {
	return t.concurrencySafe
}

// IsReadOnly reports whether the function tool is read-only.
func (t *FunctionTool) IsReadOnly() bool {
	return t.readOnly
}

// IsExternalTool reports whether the function tool runs in an external system.
func (t *FunctionTool) IsExternalTool() bool {
	return t.externalTool
}

// IsStateInjected reports whether the function tool requires AgentState.
func (t *FunctionTool) IsStateInjected() bool {
	return t.stateInjected
}

// IsMCP reports whether the function tool comes from an MCP service.
func (t *FunctionTool) IsMCP() bool {
	return t.mcp
}

// MCPName returns the MCP service name.
func (t *FunctionTool) MCPName() string {
	return t.mcpName
}

// CheckPermissions returns the function tool permission decision.
func (t *FunctionTool) CheckPermissions(ctx context.Context, input map[string]any, permCtx *permission.Context) (*permission.Decision, error) {
	if t.checkPermissions != nil {
		return t.checkPermissions(ctx, input, permCtx)
	}
	return &permission.Decision{
		Behavior:       permission.BehaviorAsk,
		Message:        fmt.Sprintf("Permission required for %s", t.name),
		DecisionReason: "Custom function tools must be explicitly allowed by the user.",
	}, nil
}

// MatchRule matches a function tool permission rule.
func (t *FunctionTool) MatchRule(ruleContent string, _ map[string]any) bool {
	return ruleContent == ""
}

// GenerateSuggestions generates suggested permission rules for the function tool.
func (t *FunctionTool) GenerateSuggestions(map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName:    t.name,
		RuleContent: t.suggestedRuleHint,
		Behavior:    permission.BehaviorAllow,
		Source:      "suggested",
	}}
}

// Execute runs the function tool.
func (t *FunctionTool) Execute(ctx context.Context, input map[string]any, state *asstate.AgentState) (<-chan ToolChunk, error) {
	if input == nil {
		input = map[string]any{}
	}
	return t.handler(ctx, input, state)
}

func cloneSchemaOrObject(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object"}
	}
	return utils.CloneAnyMap(schema)
}

func errorChunk(id, text string) ToolChunk {
	opts := []ToolChunkOption{WithToolChunkState(message.ToolResultError)}
	if id != "" {
		opts = append(opts, WithToolChunkID(id))
	}
	return *NewToolChunk(message.ContentBlockList{message.NewTextBlock(text)}, opts...)
}
