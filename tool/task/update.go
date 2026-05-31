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
	"github.com/yuluo-yx/agentscope-go/utils"
)

// TaskUpdate updates or deletes a task in AgentState.
type TaskUpdate struct {
	baseTool
}

// NewTaskUpdate creates the TaskUpdate tool.
func NewTaskUpdate() *TaskUpdate {
	return &TaskUpdate{baseTool: baseTool{
		name:            taskUpdateName,
		description:     taskUpdateDescription,
		concurrencySafe: false,
		readOnly:        false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":        map[string]any{"type": "string", "description": "The task ID."},
				"subject":        map[string]any{"type": "string", "description": "New subject for the task."},
				"description":    map[string]any{"type": "string", "description": "New description for the task."},
				"add_blocks":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Task IDs that this task blocks."},
				"status":         map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "deleted"}, "description": "New status for the task."},
				"add_blocked_by": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Task IDs that block this task."},
				"owner":          map[string]any{"type": "string", "description": "New owner for the task."},
				"metadata":       map[string]any{"type": "object", "description": "Metadata keys to merge into the task. Set a key to null to delete it."},
			},
			"required": []string{"task_id"},
		},
	}}
}

// Execute applies task updates.
func (t *TaskUpdate) Execute(_ context.Context, input map[string]any, state *astate.AgentState) (<-chan astool.ToolChunk, error) {
	taskContext, errChunk := taskContextOrError(t.Name(), state)
	if errChunk != nil {
		return errChunk, nil
	}
	taskID := strings.TrimSpace(stringValue(input, "task_id"))
	index := taskIndex(taskContext, taskID)
	if index < 0 {
		return errorText(fmt.Sprintf("TaskNotFoundError: The task %s does not exist.", taskID)), nil
	}

	outcome := applyTaskUpdates(taskContext, index, taskID, input)
	if outcome.errorText != "" {
		return errorText(outcome.errorText), nil
	}
	if outcome.deleted {
		return successText(fmt.Sprintf("Task %s has been deleted.", taskID)), nil
	}
	return successText(taskUpdateMessage(taskID, outcome.updatedFields, taskContext.Tasks[index])), nil
}

type taskUpdateOutcome struct {
	updatedFields []string
	deleted       bool
	errorText     string
}

type statusUpdateOutcome struct {
	updated   bool
	deleted   bool
	errorText string
}

func applyTaskUpdates(taskContext *astate.TaskContext, index int, taskID string, input map[string]any) taskUpdateOutcome {
	updatedFields := make([]string, 0, 6)
	task := &taskContext.Tasks[index]
	updatedFields = append(updatedFields, updateTaskTextFields(task, input)...)
	updatedFields = append(updatedFields, updateTaskRelations(taskContext, taskID, task, input)...)

	status := updateTaskStatus(taskContext, index, taskID, input)
	if status.errorText != "" || status.deleted {
		return taskUpdateOutcome{updatedFields: updatedFields, deleted: status.deleted, errorText: status.errorText}
	}
	if status.updated {
		updatedFields = append(updatedFields, "status")
		task = &taskContext.Tasks[index]
	}
	updatedFields = append(updatedFields, updateTaskOwner(task, input)...)
	updatedFields = append(updatedFields, updateTaskMetadata(task, input)...)
	return taskUpdateOutcome{updatedFields: updatedFields}
}

func updateTaskTextFields(task *astate.Task, input map[string]any) []string {
	updatedFields := []string{}
	if subject, ok := optionalString(input, "subject"); ok && subject != "" {
		task.Subject = subject
		updatedFields = append(updatedFields, "subject")
	}
	if description, ok := optionalString(input, "description"); ok {
		task.Description = description
		updatedFields = append(updatedFields, "description")
	}
	return updatedFields
}

func updateTaskRelations(taskContext *astate.TaskContext, taskID string, task *astate.Task, input map[string]any) []string {
	existingIDs := existingTaskIDs(taskContext)
	updatedFields := []string{}
	newBlocks := newRelationIDs(stringSliceValue(input, "add_blocks"), task.Blocks, existingIDs)
	if len(newBlocks) > 0 {
		updatedFields = append(updatedFields, "add_blocks")
		for _, blockID := range newBlocks {
			updateBlockRelation(taskContext, taskID, blockID)
		}
	}
	newBlockedBy := newRelationIDs(stringSliceValue(input, "add_blocked_by"), task.BlockedBy, existingIDs)
	if len(newBlockedBy) > 0 {
		updatedFields = append(updatedFields, "add_blocked_by")
		for _, blockedByID := range newBlockedBy {
			updateBlockRelation(taskContext, blockedByID, taskID)
		}
	}
	return updatedFields
}

func updateTaskStatus(taskContext *astate.TaskContext, index int, taskID string, input map[string]any) statusUpdateOutcome {
	status, ok := optionalString(input, "status")
	if !ok || status == "" {
		return statusUpdateOutcome{}
	}
	if status == "deleted" {
		deleteTask(taskContext, index)
		return statusUpdateOutcome{deleted: true}
	}
	if err := taskContext.UpdateTaskState(taskID, astate.TaskState(status)); err != nil {
		return statusUpdateOutcome{errorText: fmt.Sprintf("TaskUpdateError: invalid task status %q", status)}
	}
	return statusUpdateOutcome{updated: true}
}

func updateTaskOwner(task *astate.Task, input map[string]any) []string {
	owner, ok := optionalString(input, "owner")
	if !ok {
		return nil
	}
	task.Owner = &owner
	return []string{"owner"}
}

func updateTaskMetadata(task *astate.Task, input map[string]any) []string {
	metadata, ok := optionalMetadata(input, "metadata")
	if !ok || len(metadata) == 0 {
		return nil
	}
	if task.Metadata == nil {
		task.Metadata = map[string]any{}
	}
	for key, value := range metadata {
		if value == nil {
			delete(task.Metadata, key)
			continue
		}
		task.Metadata[key] = utils.CloneAny(value)
	}
	return []string{"metadata"}
}

func taskUpdateMessage(taskID string, updatedFields []string, task astate.Task) string {
	result := fmt.Sprintf("No updates were made to the task #%s. Make sure you provided at least one field to update and the values are correct.", taskID)
	if len(updatedFields) > 0 {
		result = fmt.Sprintf("Update task #%s %s.", taskID, strings.Join(updatedFields, ", "))
	}
	if task.State == astate.TaskCompleted {
		result += "\n\nTask completed. Call TaskList now to find your next available task or see if your work unblocked others."
	}
	return result
}

func existingTaskIDs(taskContext *astate.TaskContext) map[string]bool {
	ids := make(map[string]bool, len(taskContext.Tasks))
	for _, task := range taskContext.Tasks {
		ids[task.ID] = true
	}
	return ids
}

func newRelationIDs(candidates, current []string, existing map[string]bool) []string {
	already := make(map[string]bool, len(current))
	for _, id := range current {
		already[id] = true
	}
	out := make([]string, 0, len(candidates))
	for _, id := range candidates {
		id = strings.TrimSpace(id)
		if id == "" || already[id] || !existing[id] {
			continue
		}
		already[id] = true
		out = append(out, id)
	}
	return out
}

func updateBlockRelation(taskContext *astate.TaskContext, blockID, blockedByID string) {
	for index := range taskContext.Tasks {
		task := &taskContext.Tasks[index]
		if task.ID == blockID && !containsString(task.Blocks, blockedByID) {
			task.Blocks = append(task.Blocks, blockedByID)
		}
		if task.ID == blockedByID && !containsString(task.BlockedBy, blockID) {
			task.BlockedBy = append(task.BlockedBy, blockID)
		}
	}
}

func deleteTask(taskContext *astate.TaskContext, index int) {
	deletedID := taskContext.Tasks[index].ID
	taskContext.Tasks = append(taskContext.Tasks[:index], taskContext.Tasks[index+1:]...)
	for taskIndex := range taskContext.Tasks {
		task := &taskContext.Tasks[taskIndex]
		task.Blocks = removeString(task.Blocks, deletedID)
		task.BlockedBy = removeString(task.BlockedBy, deletedID)
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

func removeString(values []string, target string) []string {
	out := values[:0]
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}
