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

	astate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

// TaskCreate creates a task in AgentState.
type TaskCreate struct {
	baseTool
}

// NewTaskCreate creates the TaskCreate tool.
func NewTaskCreate() *TaskCreate {
	return &TaskCreate{baseTool: baseTool{
		name:            taskCreateName,
		description:     taskCreateDescription,
		concurrencySafe: false,
		readOnly:        false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subject":     map[string]any{"type": "string", "description": "A brief title for the task."},
				"description": map[string]any{"type": "string", "description": "What needs to be done."},
				"metadata":    map[string]any{"type": "object", "description": "Arbitrary metadata to attach to the task."},
			},
			"required": []string{"subject", "description"},
		},
	}}
}

// Execute creates the task and appends it to AgentState.
func (t *TaskCreate) Execute(_ context.Context, input map[string]any, state *astate.AgentState) (<-chan astool.ToolChunk, error) {
	taskContext, errChunk := taskContextOrError(t.Name(), state)
	if errChunk != nil {
		return errChunk, nil
	}
	subject := strings.TrimSpace(stringValue(input, "subject"))
	if subject == "" {
		return errorText("CreateTaskError: subject is required"), nil
	}
	description := stringValue(input, "description")
	newTask := astate.NewTask(subject, description, metadataValue(input, "metadata"))
	taskContext.AddTask(newTask)
	return successText(fmt.Sprintf("Task %s created successfully: %s", newTask.ID, newTask.Subject)), nil
}
