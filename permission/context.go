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
type Context struct {
	Mode               PermissionMode                        `json:"mode"`
	WorkingDirectories map[string]AdditionalWorkingDirectory `json:"working_directories,omitempty"`
	AllowRules         map[string][]Rule                     `json:"allow_rules,omitempty"`
	DenyRules          map[string][]Rule                     `json:"deny_rules,omitempty"`
	AskRules           map[string][]Rule                     `json:"ask_rules,omitempty"`
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
	}
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
