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

// Package tool provides tool registration, grouping, and function adapters.
package tool

import (
	"fmt"
	"strings"
)

const basicGroupName = "basic"

// ToolGroup is a set of tools enabled on demand by ToolContext.ActivatedGroups.
type ToolGroup struct {
	name         string
	description  string
	instructions string
	tools        []Tool
}

// GroupOption configures a tool group.
type GroupOption func(*ToolGroup)

// WithGroupDescription sets the group description. Non-basic groups require it.
func WithGroupDescription(description string) GroupOption {
	return func(group *ToolGroup) {
		group.description = strings.TrimSpace(description)
	}
}

// WithGroupInstructions sets system-prompt instructions used after activation.
func WithGroupInstructions(instructions string) GroupOption {
	return func(group *ToolGroup) {
		group.instructions = strings.TrimSpace(instructions)
	}
}

// WithGroupTools sets the tools in the group.
func WithGroupTools(tools ...Tool) GroupOption {
	return func(group *ToolGroup) {
		group.tools = append([]Tool(nil), tools...)
	}
}

// NewGroup creates a tool group. Toolkit owns the basic group internally.
func NewGroup(name string, opts ...GroupOption) (*ToolGroup, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tool: group name is required")
	}
	group := &ToolGroup{name: name}
	for _, opt := range opts {
		opt(group)
	}
	if group.name != basicGroupName && group.description == "" {
		return nil, fmt.Errorf("tool: group %q requires a description", group.name)
	}
	return group, nil
}

// Name returns the group name.
func (g *ToolGroup) Name() string {
	if g == nil {
		return ""
	}
	return g.name
}

// Description returns the group description.
func (g *ToolGroup) Description() string {
	if g == nil {
		return ""
	}
	return g.description
}

// Instructions returns prompt instructions used after group activation.
func (g *ToolGroup) Instructions() string {
	if g == nil {
		return ""
	}
	return g.instructions
}

// Tools returns a copy of the tools in the group.
func (g *ToolGroup) Tools() []Tool {
	if g == nil {
		return nil
	}
	return append([]Tool(nil), g.tools...)
}
