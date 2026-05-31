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

// Package task provides state-injected task management tools.
package task

import astool "github.com/yuluo-yx/agentscope-go/tool"

const (
	taskCreateName = "TaskCreate"
	taskListName   = "TaskList"
	taskGetName    = "TaskGet"
	taskUpdateName = "TaskUpdate"
)

// NewTools creates all built-in task tools in Python-compatible order.
func NewTools() []astool.Tool {
	return []astool.Tool{
		NewTaskCreate(),
		NewTaskGet(),
		NewTaskList(),
		NewTaskUpdate(),
	}
}
