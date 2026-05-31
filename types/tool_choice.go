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

package types

import "fmt"

// ToolChoiceMode represents the model tool-choice mode.
type ToolChoiceMode string

const (
	// ToolChoiceAuto lets the model decide whether to call tools.
	ToolChoiceAuto ToolChoiceMode = "auto"
	// ToolChoiceNone prevents tool calls in this model call.
	ToolChoiceNone ToolChoiceMode = "none"
	// ToolChoiceRequired requires at least one tool call in this model call.
	ToolChoiceRequired ToolChoiceMode = "required"
)

// ToolChoice describes tool visibility and forced-call policy for model calls.
type ToolChoice struct {
	Mode  string   `json:"mode"`
	Tools []string `json:"tools,omitempty"`
}

// NewToolChoice creates tool-choice config and validates mode/tool filters.
func NewToolChoice(mode string, tools ...string) (*ToolChoice, error) {
	if mode == "" {
		mode = string(ToolChoiceAuto)
	}
	choice := &ToolChoice{Mode: mode, Tools: append([]string(nil), tools...)}
	if len(choice.Tools) > 0 && !choice.isBuiltInMode() && !containsString(choice.Tools, choice.Mode) {
		return nil, fmt.Errorf("agentscope/types: forced tool %q is not included in tools filter", choice.Mode)
	}
	return choice, nil
}

// Validate checks whether the tool choice can be satisfied by available tools.
func (c *ToolChoice) Validate(availableTools []string) error {
	if c == nil {
		return nil
	}
	if c.Mode == "" {
		return fmt.Errorf("agentscope/types: tool choice mode is empty")
	}
	available := make(map[string]struct{}, len(availableTools))
	for _, name := range availableTools {
		available[name] = struct{}{}
	}
	for _, name := range c.Tools {
		if len(available) > 0 {
			if _, ok := available[name]; !ok {
				return fmt.Errorf("agentscope/types: tool %q in tools filter is not available", name)
			}
		}
	}
	if c.isBuiltInMode() {
		return nil
	}
	if len(c.Tools) > 0 && !containsString(c.Tools, c.Mode) {
		return fmt.Errorf("agentscope/types: forced tool %q is not included in tools filter", c.Mode)
	}
	if len(available) > 0 {
		if _, ok := available[c.Mode]; !ok {
			return fmt.Errorf("agentscope/types: forced tool %q is not available", c.Mode)
		}
	}
	return nil
}

// Clone returns a deep copy of tool-choice config.
func (c *ToolChoice) Clone() *ToolChoice {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Tools = append([]string(nil), c.Tools...)
	return &cp
}

func (c *ToolChoice) isBuiltInMode() bool {
	switch ToolChoiceMode(c.Mode) {
	case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
		return true
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
