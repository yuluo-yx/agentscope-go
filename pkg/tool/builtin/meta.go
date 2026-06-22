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

package builtin

import astool "github.com/yuluo-yx/agentscope-go/pkg/tool"

// GroupInfo is the minimal group metadata required by reset_tools.
type GroupInfo = astool.GroupInfo

// ResetTools resets ToolContext.ActivatedGroups from boolean inputs.
type ResetTools = astool.ResetTools

// NewResetTools creates the tool group activation control tool.
func NewResetTools(groups []GroupInfo) *ResetTools {
	return astool.NewResetTools(groups)
}
