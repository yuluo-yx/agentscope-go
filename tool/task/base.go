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

package task

import (
	"context"
	"fmt"

	"github.com/yuluo-yx/agentscope-go/permission"
	"github.com/yuluo-yx/agentscope-go/utils"
)

type baseTool struct {
	name            string
	description     string
	schema          map[string]any
	concurrencySafe bool
	readOnly        bool
}

// Name returns the model-facing tool name.
func (b baseTool) Name() string {
	return b.name
}

// Description returns the model-facing tool description.
func (b baseTool) Description() string {
	return b.description
}

// InputSchema returns the tool input JSON Schema.
func (b baseTool) InputSchema() map[string]any {
	return utils.CloneAnyMap(b.schema)
}

// IsConcurrencySafe reports whether the tool can run concurrently.
func (b baseTool) IsConcurrencySafe() bool {
	return b.concurrencySafe
}

// IsReadOnly reports whether the tool only reads state.
func (b baseTool) IsReadOnly() bool {
	return b.readOnly
}

// IsExternalTool reports whether the tool must run externally.
func (b baseTool) IsExternalTool() bool {
	return false
}

// IsStateInjected reports whether the tool requires AgentState.
func (b baseTool) IsStateInjected() bool {
	return true
}

// IsMCP reports whether the tool comes from an MCP service.
func (b baseTool) IsMCP() bool {
	return false
}

// MCPName returns the MCP service name.
func (b baseTool) MCPName() string {
	return ""
}

// CheckPermissions always allows task tools.
func (b baseTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	return &permission.Decision{
		Behavior:       permission.BehaviorAllow,
		Message:        fmt.Sprintf("%s is always allowed to be called.", b.name),
		DecisionReason: "Task tools only mutate the local AgentState task context.",
	}, nil
}

// MatchRule matches empty allow rules for task tools.
func (b baseTool) MatchRule(ruleContent string, _ map[string]any) bool {
	return ruleContent == ""
}

// GenerateSuggestions returns an allow rule suggestion for the task tool.
func (b baseTool) GenerateSuggestions(map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName:    b.name,
		RuleContent: "",
		Behavior:    permission.BehaviorAllow,
		Source:      "suggested",
	}}
}
