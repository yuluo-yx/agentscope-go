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

package permission

// AdditionalWorkingDirectory represents an additional trusted working directory.
type AdditionalWorkingDirectory struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

// Context stores permission mode, working directories, and rules by behavior.
// Contains all context required for the permission engine to make a decision.
type Context struct {
	// Mode is the current permission mode, determining the engine's default decision behavior.
	Mode PermissionMode `json:"mode"`
	// WorkingDirectories is a list of trusted working directories where operations are permitted.
	WorkingDirectories map[string]AdditionalWorkingDirectory `json:"working_directories,omitempty"`
	// AllowRules are permission rules grouped by tool name that grant access.
	AllowRules map[string][]Rule `json:"allow_rules,omitempty"`
	// DenyRules are permission rules grouped by tool name that block access.
	DenyRules map[string][]Rule `json:"deny_rules,omitempty"`
	// AskRules are permission rules grouped by tool name that prompt for confirmation.
	AskRules map[string][]Rule `json:"ask_rules,omitempty"`
	// AutoDenialState tracks denial counters in Auto mode.
	AutoDenialState AutoDenialState `json:"auto_denial_state,omitempty"`
}

type PermissionContext = Context

// NewContext creates a permission context with initialized maps.
func NewContext(mode PermissionMode) *Context {
	if mode == "" {
		mode = ModeDefault
	}
	return &Context{
		Mode:               mode,
		WorkingDirectories: map[string]AdditionalWorkingDirectory{},
		AllowRules:         map[string][]Rule{},
		DenyRules:          map[string][]Rule{},
		AskRules:           map[string][]Rule{},
		AutoDenialState:    AutoDenialState{},
	}
}

// Clone returns a deep copy of the permission context.
func (c *Context) Clone() *Context {

	if c == nil {
		return nil
	}

	mode := c.Mode
	if mode == "" {
		mode = ModeDefault
	}

	cp := &Context{
		Mode:               mode,
		WorkingDirectories: make(map[string]AdditionalWorkingDirectory, len(c.WorkingDirectories)),
		AllowRules:         cloneRuleMap(c.AllowRules),
		DenyRules:          cloneRuleMap(c.DenyRules),
		AskRules:           cloneRuleMap(c.AskRules),
		AutoDenialState:    c.AutoDenialState,
	}
	for key, value := range c.WorkingDirectories {
		cp.WorkingDirectories[key] = value
	}

	cp.ensureMaps()

	return cp
}

func (c *Context) ensureMaps() {
	if c.WorkingDirectories == nil {
		c.WorkingDirectories = map[string]AdditionalWorkingDirectory{}
	}
	if c.AllowRules == nil {
		c.AllowRules = map[string][]Rule{}
	}
	if c.DenyRules == nil {
		c.DenyRules = map[string][]Rule{}
	}
	if c.AskRules == nil {
		c.AskRules = map[string][]Rule{}
	}
}

func cloneRuleMap(in map[string][]Rule) map[string][]Rule {

	out := make(map[string][]Rule, len(in))
	for key, rules := range in {
		out[key] = append([]Rule(nil), rules...)
	}

	return out
}
