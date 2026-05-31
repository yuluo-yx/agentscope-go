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

// TaskGet retrieves a task by ID.
type TaskGet struct {
	baseTool
}

// NewTaskGet creates the TaskGet tool.
func NewTaskGet() *TaskGet {
	return &TaskGet{baseTool: baseTool{
		name:            taskGetName,
		description:     taskGetDescription,
		concurrencySafe: true,
		readOnly:        true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "The ID of the task to retrieve."},
			},
			"required": []string{"task_id"},
		},
	}}
}

// Execute returns full task details.
func (t *TaskGet) Execute(_ context.Context, input map[string]any, state *astate.AgentState) (<-chan astool.ToolChunk, error) {
	taskContext, errChunk := taskContextOrError(t.Name(), state)
	if errChunk != nil {
		return errChunk, nil
	}
	taskID := strings.TrimSpace(stringValue(input, "task_id"))
	task, ok := taskContext.GetTask(taskID)
	if !ok {
		return errorText("Task not found"), nil
	}
	lines := []string{
		fmt.Sprintf("Task #%s: %s", task.ID, task.Subject),
		fmt.Sprintf("Status: %s", task.State),
		fmt.Sprintf("Description: %s", task.Description),
	}
	if task.Owner != nil && *task.Owner != "" {
		lines = append(lines, fmt.Sprintf("Owner: %s", *task.Owner))
	}
	if len(task.BlockedBy) > 0 {
		lines = append(lines, fmt.Sprintf("Blocked by: %s", prefixedIDs(task.BlockedBy)))
	}
	if len(task.Blocks) > 0 {
		lines = append(lines, fmt.Sprintf("Blocks: %s", prefixedIDs(task.Blocks)))
	}
	if len(task.Metadata) > 0 {
		lines = append(lines, fmt.Sprintf("Metadata: %v", task.Metadata))
	}
	return successText(strings.Join(lines, "\n")), nil
}
