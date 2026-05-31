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

package task

import (
	"context"
	"fmt"
	"strings"

	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

// TaskList lists tasks in AgentState.
type TaskList struct {
	baseTool
}

// NewTaskList creates the TaskList tool.
func NewTaskList() *TaskList {
	return &TaskList{baseTool: baseTool{
		name:            taskListName,
		description:     taskListDescription,
		concurrencySafe: true,
		readOnly:        true,
		schema:          map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}

// Execute returns a compact summary of all tasks.
func (t *TaskList) Execute(_ context.Context, _ map[string]any, state *astate.AgentState) (<-chan astool.ToolChunk, error) {
	taskContext, errChunk := taskContextOrError(t.Name(), state)
	if errChunk != nil {
		return errChunk, nil
	}
	if len(taskContext.Tasks) == 0 {
		return successText("No tasks available."), nil
	}
	lines := make([]string, 0, len(taskContext.Tasks))
	for _, task := range taskContext.Tasks {
		owner := ""
		if task.Owner != nil && *task.Owner != "" {
			owner = fmt.Sprintf("(%s)", *task.Owner)
		}
		blocked := ""
		if len(task.BlockedBy) > 0 {
			blocked = fmt.Sprintf("[blocked by %s]", strings.Join(task.BlockedBy, ", "))
		}
		lines = append(lines, fmt.Sprintf("#%s [%s] %s%s%s", task.ID, task.State, task.Subject, owner, blocked))
	}
	return successText(strings.Join(lines, "\n")), nil
}
