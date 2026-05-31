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

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// GroupInfo is the minimal group metadata required by reset_tools.
type GroupInfo struct {
	Name         string
	Description  string
	Instructions string
}

// ResetTools resets ToolContext.ActivatedGroups from boolean inputs.
type ResetTools struct {
	groups []GroupInfo
	schema map[string]any
}

// NewResetTools creates the tool group activation control tool.
func NewResetTools(groups []GroupInfo) *ResetTools {
	copied := make([]GroupInfo, 0, len(groups))
	properties := map[string]any{}
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		copied = append(copied, GroupInfo{
			Name:         name,
			Description:  strings.TrimSpace(group.Description),
			Instructions: strings.TrimSpace(group.Instructions),
		})
		properties[name] = map[string]any{
			"type":        "boolean",
			"description": strings.TrimSpace(group.Description),
		}
	}
	return &ResetTools{
		groups: copied,
		schema: map[string]any{
			"type":                 "object",
			"properties":           properties,
			"additionalProperties": false,
		},
	}
}

// Name returns the tool name.
func (t *ResetTools) Name() string {
	return "reset_tools"
}

// Description returns the tool description.
func (t *ResetTools) Description() string {
	return "Activate or deactivate optional tool groups for the next tool calls."
}

// InputSchema returns the reset_tools input JSON Schema.
func (t *ResetTools) InputSchema() map[string]any {
	return utils.CloneAnyMap(t.schema)
}

// IsConcurrencySafe reports whether reset_tools can run concurrently.
func (t *ResetTools) IsConcurrencySafe() bool {
	return false
}

// IsReadOnly reports whether reset_tools is read-only.
func (t *ResetTools) IsReadOnly() bool {
	return false
}

// IsExternalTool reports whether reset_tools runs in an external system.
func (t *ResetTools) IsExternalTool() bool {
	return false
}

// IsStateInjected reports whether reset_tools requires AgentState.
func (t *ResetTools) IsStateInjected() bool {
	return true
}

// IsMCP reports whether reset_tools comes from an MCP service.
func (t *ResetTools) IsMCP() bool {
	return false
}

// MCPName returns the MCP service name.
func (t *ResetTools) MCPName() string {
	return ""
}

// CheckPermissions allows reset_tools to update local tool group state.
func (t *ResetTools) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	return &permission.Decision{
		Behavior:       permission.BehaviorAllow,
		Message:        "Permission granted for reset_tools",
		DecisionReason: "Meta tool only updates local tool group activation state.",
	}, nil
}

// MatchRule matches reset_tools permission rules.
func (t *ResetTools) MatchRule(ruleContent string, _ map[string]any) bool {
	return ruleContent == ""
}

// GenerateSuggestions generates suggested permission rules for reset_tools.
func (t *ResetTools) GenerateSuggestions(map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName: t.Name(),
		Behavior: permission.BehaviorAllow,
		Source:   "suggested",
	}}
}

// Execute updates AgentState.ToolContext.ActivatedGroups.
func (t *ResetTools) Execute(_ context.Context, input map[string]any, state *asstate.AgentState) (<-chan ToolChunk, error) {
	if state == nil {
		state = asstate.NewAgentState()
	}
	if state.ToolContext == nil {
		state.ToolContext = asstate.NewToolContext()
	}
	activated := make([]string, 0, len(t.groups))
	lines := make([]string, 0, len(t.groups)+1)
	for _, group := range t.groups {
		enabled, _ := input[group.Name].(bool)
		if !enabled {
			continue
		}
		activated = append(activated, group.Name)
		if group.Instructions != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", group.Name, group.Instructions))
		}
	}
	state.ToolContext.ActivatedGroups = activated
	text := "No optional tool groups activated."
	if len(activated) > 0 {
		text = "Activated tool groups: " + strings.Join(activated, ", ")
		if len(lines) > 0 {
			text += "\n" + strings.Join(lines, "\n")
		}
	}
	chunks := make(chan ToolChunk, 1)
	chunks <- *NewToolChunk(
		"",
		message.ContentBlockList{message.NewTextBlock(text)},
		WithToolChunkState(message.ToolResultSuccess),
	)
	close(chunks)
	return chunks, nil
}
