// Copyright 20\d\d AgentScope Go
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
	stderrors "errors"
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/internal/jsonutil"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/types"
)

// RegisteredTool stores a tool with its owning group.
type RegisteredTool struct {
	Tool  Tool
	Group string
}

// Toolkit registers tools, manages active groups, exposes schemas, and runs tools.
type Toolkit struct {
	basicTools []Tool
	groups     []*ToolGroup
	toolGroups map[string]string
	resetTool  Tool
}

// NewToolkit creates a toolkit with only basic tools.
func NewToolkit(tools ...Tool) (*Toolkit, error) {
	return NewToolkitWithGroups(tools)
}

// NewToolkitWithGroups creates a toolkit with basic tools and optional groups.
func NewToolkitWithGroups(tools []Tool, groups ...*ToolGroup) (*Toolkit, error) {
	kit := &Toolkit{
		basicTools: append([]Tool(nil), tools...),
		groups:     append([]*ToolGroup(nil), groups...),
		toolGroups: map[string]string{},
	}
	if len(groups) > 0 {
		infos := make([]GroupInfo, 0, len(groups))
		for _, group := range groups {
			if group == nil {
				continue
			}
			infos = append(infos, GroupInfo{
				Name:         group.Name(),
				Description:  group.Description(),
				Instructions: group.Instructions(),
			})
		}
		kit.resetTool = NewResetTools(infos)
	}
	if err := kit.validate(); err != nil {
		return nil, err
	}
	return kit, nil
}

// ToolSchemas returns model tool schemas visible to the active groups.
func (t *Toolkit) ToolSchemas(activeGroups ...string) ([]asmodel.ToolSchema, error) {
	tools := t.availableTools(activeGroups)
	schemas := make([]asmodel.ToolSchema, 0, len(tools))
	for _, registered := range tools {
		schema, err := schemaForTool(registered.Tool)
		if err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}
	return schemas, nil
}

// AvailableTools returns tools callable by the active groups.
func (t *Toolkit) AvailableTools(activeGroups ...string) []RegisteredTool {
	return append([]RegisteredTool(nil), t.availableTools(activeGroups)...)
}

// FindTool looks up a tool visible to the active groups by name.
func (t *Toolkit) FindTool(name string, activeGroups ...string) (Tool, bool) {
	return t.lookup(name, activeGroups)
}

// RunTool executes one tool call and accumulates a ToolResponse.
func (t *Toolkit) RunTool(ctx context.Context, call *message.ToolCallBlock, state *asstate.AgentState) (*ToolResponse, error) {
	if call == nil {
		return nil, fmt.Errorf("tool: nil tool call")
	}
	chunks, err := t.CallTool(ctx, call, state)
	if err != nil {
		return nil, err
	}
	response := NewToolResponse(call.ID)
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			return nil, err
		}
	}
	return response, nil
}

// CallTool executes one tool call and returns call-level errors as error chunks.
func (t *Toolkit) CallTool(ctx context.Context, call *message.ToolCallBlock, state *asstate.AgentState) (<-chan ToolChunk, error) {
	if call == nil {
		return nil, fmt.Errorf("tool: nil tool call")
	}
	tool, ok := t.lookup(call.Name, activeGroupsFromState(state))
	if !ok {
		return singleChunk(errorChunk(call.ID, t.toolLookupError(call.Name))), nil
	}
	input, err := jsonutil.LoadObject(call.Input)
	if err != nil {
		return singleChunk(errorChunk(call.ID, err.Error())), nil
	}
	chunks, err := tool.Execute(ctx, input, state)
	if err != nil {
		if stderrors.Is(err, context.Canceled) {
			return singleChunk(interruptedChunk(call.ID, err.Error())), nil
		}
		return singleChunk(errorChunk(call.ID, err.Error())), nil
	}
	if chunks == nil {
		return singleChunk(errorChunk(call.ID, fmt.Sprintf("tool %s returned nil chunk stream", tool.Name()))), nil
	}
	return chunks, nil
}

func (t *Toolkit) validate() error {
	seen := map[string]string{}
	for _, tool := range t.basicTools {
		if err := registerTool(seen, t.toolGroups, tool, basicGroupName); err != nil {
			return err
		}
	}
	for _, group := range t.groups {
		if group == nil {
			return fmt.Errorf("tool: nil tool group")
		}
		if group.Name() == basicGroupName {
			return fmt.Errorf("tool: basic group is reserved")
		}
		for _, groupTool := range group.Tools() {
			if err := registerTool(seen, t.toolGroups, groupTool, group.Name()); err != nil {
				return err
			}
		}
	}
	if t.resetTool != nil {
		if err := registerTool(seen, t.toolGroups, t.resetTool, basicGroupName); err != nil {
			return err
		}
	}
	return nil
}

func registerTool(seen, toolGroups map[string]string, tool Tool, group string) error {
	if tool == nil {
		return fmt.Errorf("tool: nil tool in group %q", group)
	}
	name := strings.TrimSpace(tool.Name())
	if name == "" {
		return fmt.Errorf("tool: tool in group %q has empty name", group)
	}
	if previous, exists := seen[name]; exists {
		return fmt.Errorf("tool: duplicate tool %q in groups %q and %q", name, previous, group)
	}
	seen[name] = group
	toolGroups[name] = group
	return nil
}

func (t *Toolkit) availableTools(activeGroups []string) []RegisteredTool {
	active := activeSet(activeGroups)
	tools := make([]RegisteredTool, 0, len(t.basicTools)+len(t.groups)+1)
	for _, tool := range t.basicTools {
		tools = append(tools, RegisteredTool{Tool: tool, Group: basicGroupName})
	}
	for _, group := range t.groups {
		if group == nil || !active[group.Name()] {
			continue
		}
		for _, groupTool := range group.Tools() {
			tools = append(tools, RegisteredTool{Tool: groupTool, Group: group.Name()})
		}
	}
	if t.resetTool != nil {
		tools = append(tools, RegisteredTool{Tool: t.resetTool, Group: basicGroupName})
	}
	return tools
}

func (t *Toolkit) lookup(name string, activeGroups []string) (Tool, bool) {
	for _, registered := range t.availableTools(activeGroups) {
		if registered.Tool.Name() == name {
			return registered.Tool, true
		}
	}
	return nil, false
}

func (t *Toolkit) toolLookupError(name string) string {
	group, exists := t.toolGroups[name]
	if !exists || group == basicGroupName {
		return fmt.Sprintf("tool %s not found", name)
	}
	return fmt.Sprintf("tool %s belongs to inactive group %s", name, group)
}

func schemaForTool(tool Tool) (asmodel.ToolSchema, error) {
	if tool == nil {
		return asmodel.ToolSchema{}, fmt.Errorf("tool: nil tool")
	}
	name := strings.TrimSpace(tool.Name())
	if name == "" {
		return asmodel.ToolSchema{}, fmt.Errorf("tool: schema tool name is required")
	}
	inputSchema := cloneSchemaOrObject(tool.InputSchema())
	if schemaType, ok := inputSchema["type"].(string); ok && schemaType != "object" {
		return asmodel.ToolSchema{}, fmt.Errorf("tool: input schema for %s must be an object, got %q", name, schemaType)
	}
	if _, ok := inputSchema["type"]; !ok {
		inputSchema["type"] = "object"
	}
	return asmodel.ToolSchema{
		Type: "function",
		Function: asmodel.FunctionSchema{
			Name:        name,
			Description: tool.Description(),
			Parameters:  types.JSONSchema(inputSchema),
		},
		Metadata: map[string]any{},
	}, nil
}

func activeGroupsFromState(state *asstate.AgentState) []string {
	if state == nil || state.ToolContext == nil {
		return nil
	}
	return append([]string(nil), state.ToolContext.ActivatedGroups...)
}

func activeSet(groups []string) map[string]bool {
	active := make(map[string]bool, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group != "" {
			active[group] = true
		}
	}
	return active
}

func interruptedChunk(id, text string) ToolChunk {
	return *NewToolChunk(
		id,
		message.ContentBlockList{message.NewTextBlock(text)},
		WithToolChunkState(message.ToolResultInterrupted),
	)
}

func singleChunk(chunk ToolChunk) <-chan ToolChunk {
	chunks := make(chan ToolChunk, 1)
	chunks <- chunk
	close(chunks)
	return chunks
}
