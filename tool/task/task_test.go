// Copyright 20\d\d AgentScope Go
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

package task_test

import (
	"context"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	tasktool "github.com/yuluo-yx/agentscope-go/tool/task"
)

func TestTaskCreateListAndPermissions(t *testing.T) {
	t.Parallel()

	state := astate.NewAgentState()
	create := tasktool.NewTaskCreate()
	assertTaskCreateMetadata(t, create)

	firstCreate := runTool(t, create, map[string]any{
		"subject":     "Translate task tools",
		"description": "Port Python task tools to Go.",
		"metadata":    map[string]any{"phase": "five"},
	}, state)
	if firstCreate.State != message.ToolResultSuccess || !strings.Contains(textOutput(firstCreate), "created successfully") {
		t.Fatalf("TaskCreate should succeed, got %#v output=%q", firstCreate, textOutput(firstCreate))
	}
	if len(state.TaskContext.Tasks) != 1 {
		t.Fatalf("TaskCreate should append one task, got %#v", state.TaskContext.Tasks)
	}
	firstID := state.TaskContext.Tasks[0].ID
	if got := state.TaskContext.Tasks[0].State; got != astate.TaskPending {
		t.Fatalf("new task should be pending, got %q", got)
	}

	list := runTool(t, tasktool.NewTaskList(), nil, state)
	if list.State != message.ToolResultSuccess || !strings.Contains(textOutput(list), "#"+firstID+" [pending] Translate task tools") {
		t.Fatalf("TaskList should include task summaries, got %#v output=%q", list, textOutput(list))
	}
}

func TestNewToolsUsesPythonExportOrder(t *testing.T) {
	t.Parallel()

	tools := tasktool.NewTools()
	want := []string{"TaskCreate", "TaskGet", "TaskList", "TaskUpdate"}
	if len(tools) != len(want) {
		t.Fatalf("NewTools length mismatch: got %d want %d", len(tools), len(want))
	}
	for index, tool := range tools {
		if tool.Name() != want[index] {
			t.Fatalf("NewTools order mismatch at %d: got %q want %q", index, tool.Name(), want[index])
		}
	}
}

func TestTaskUpdateGetAndDelete(t *testing.T) {
	t.Parallel()

	state := astate.NewAgentState()
	firstID := createTask(t, state, "Translate task tools", "Port Python task tools to Go.", map[string]any{"phase": "five"})
	secondID := createTask(t, state, "Add E2E smoke", "Cover Agent, model, tool, and state together.", map[string]any{"phase": "old"})

	update := tasktool.NewTaskUpdate()
	updated := runTool(t, update, map[string]any{
		"task_id":        secondID,
		"description":    "",
		"status":         "in_progress",
		"owner":          "agent-1",
		"add_blocked_by": []any{firstID},
		"metadata":       map[string]any{"phase": nil, "scope": "global"},
	}, state)
	if updated.State != message.ToolResultSuccess || !strings.Contains(textOutput(updated), "status") {
		t.Fatalf("TaskUpdate should report updated fields, got %#v output=%q", updated, textOutput(updated))
	}
	assertUpdatedTask(t, state, firstID, secondID)

	got := runTool(t, tasktool.NewTaskGet(), map[string]any{"task_id": secondID}, state)
	if got.State != message.ToolResultSuccess || !strings.Contains(textOutput(got), "Blocked by: #"+firstID) {
		t.Fatalf("TaskGet should include full details, got %#v output=%q", got, textOutput(got))
	}

	deleted := runTool(t, update, map[string]any{"task_id": firstID, "status": "deleted"}, state)
	assertDeletedTask(t, deleted, state)
}

func TestTaskUpdateAddBlocksRelation(t *testing.T) {
	t.Parallel()

	state := astate.NewAgentState()
	firstID := createTask(t, state, "Finish base task", "A task that blocks another one.", nil)
	secondID := createTask(t, state, "Wait for base task", "A task blocked by the base task.", nil)

	updated := runTool(t, tasktool.NewTaskUpdate(), map[string]any{
		"task_id":    firstID,
		"add_blocks": []any{secondID},
	}, state)
	if updated.State != message.ToolResultSuccess || !strings.Contains(textOutput(updated), "add_blocks") {
		t.Fatalf("TaskUpdate should report add_blocks, got %#v output=%q", updated, textOutput(updated))
	}
	if len(state.TaskContext.Tasks[0].Blocks) != 1 || state.TaskContext.Tasks[0].Blocks[0] != secondID {
		t.Fatalf("TaskUpdate did not add blocks relation: %#v", state.TaskContext.Tasks[0].Blocks)
	}
	if len(state.TaskContext.Tasks[1].BlockedBy) != 1 || state.TaskContext.Tasks[1].BlockedBy[0] != firstID {
		t.Fatalf("TaskUpdate did not add reverse blocked_by relation: %#v", state.TaskContext.Tasks[1].BlockedBy)
	}
}

func TestTaskToolsReturnErrorsForInvalidStateAndInput(t *testing.T) {
	t.Parallel()

	create := runTool(t, tasktool.NewTaskCreate(), map[string]any{"subject": "Missing state", "description": "No AgentState."}, nil)
	if create.State != message.ToolResultError || !strings.Contains(textOutput(create), "requires AgentState") {
		t.Fatalf("TaskCreate should require AgentState, got %#v output=%q", create, textOutput(create))
	}

	state := astate.NewAgentState()
	missing := runTool(t, tasktool.NewTaskGet(), map[string]any{"task_id": "missing"}, state)
	if missing.State != message.ToolResultError || !strings.Contains(textOutput(missing), "Task not found") {
		t.Fatalf("TaskGet should report missing tasks, got %#v output=%q", missing, textOutput(missing))
	}

	task := astate.NewTask("Keep status valid", "Status validation.", nil)
	state.TaskContext.AddTask(task)
	invalid := runTool(t, tasktool.NewTaskUpdate(), map[string]any{"task_id": task.ID, "status": "blocked"}, state)
	if invalid.State != message.ToolResultError || !strings.Contains(textOutput(invalid), "invalid task status") {
		t.Fatalf("TaskUpdate should reject invalid status, got %#v output=%q", invalid, textOutput(invalid))
	}
}

func assertTaskCreateMetadata(t *testing.T, create *tasktool.TaskCreate) {
	t.Helper()
	if create.Name() != "TaskCreate" || !create.IsStateInjected() || create.IsReadOnly() {
		t.Fatalf("TaskCreate metadata mismatch: name=%q state=%v readOnly=%v", create.Name(), create.IsStateInjected(), create.IsReadOnly())
	}
	decision, err := create.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("TaskCreate CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("TaskCreate should be allowed by default, got %#v", decision)
	}
}

func createTask(t *testing.T, state *astate.AgentState, subject, description string, metadata map[string]any) string {
	t.Helper()
	input := map[string]any{"subject": subject, "description": description}
	if metadata != nil {
		input["metadata"] = metadata
	}
	before := len(state.TaskContext.Tasks)
	response := runTool(t, tasktool.NewTaskCreate(), input, state)
	if response.State != message.ToolResultSuccess || len(state.TaskContext.Tasks) != before+1 {
		t.Fatalf("TaskCreate should append a task, got %#v tasks=%#v", response, state.TaskContext.Tasks)
	}
	return state.TaskContext.Tasks[before].ID
}

func assertUpdatedTask(t *testing.T, state *astate.AgentState, firstID, secondID string) {
	t.Helper()
	second := state.TaskContext.Tasks[1]
	if second.State != astate.TaskInProgress || second.Owner == nil || *second.Owner != "agent-1" {
		t.Fatalf("TaskUpdate did not update status or owner: %#v", second)
	}
	if second.Description != "" {
		t.Fatalf("TaskUpdate should allow clearing description, got %q", second.Description)
	}
	if len(second.BlockedBy) != 1 || second.BlockedBy[0] != firstID {
		t.Fatalf("TaskUpdate did not add blocked_by relation: %#v", second.BlockedBy)
	}
	if len(state.TaskContext.Tasks[0].Blocks) != 1 || state.TaskContext.Tasks[0].Blocks[0] != secondID {
		t.Fatalf("TaskUpdate did not update reverse blocks relation: %#v", state.TaskContext.Tasks[0].Blocks)
	}
	if _, exists := second.Metadata["phase"]; exists || second.Metadata["scope"] != "global" {
		t.Fatalf("TaskUpdate should merge and delete metadata keys, got %#v", second.Metadata)
	}
}

func assertDeletedTask(t *testing.T, deleted *astool.ToolResponse, state *astate.AgentState) {
	t.Helper()
	if deleted.State != message.ToolResultSuccess || len(state.TaskContext.Tasks) != 1 {
		t.Fatalf("TaskUpdate should delete tasks, got %#v tasks=%#v", deleted, state.TaskContext.Tasks)
	}
	if len(state.TaskContext.Tasks[0].BlockedBy) != 0 {
		t.Fatalf("deleting a task should remove dependency references, got %#v", state.TaskContext.Tasks[0].BlockedBy)
	}
}

func runTool(t *testing.T, tool astool.Tool, input map[string]any, state *astate.AgentState) *astool.ToolResponse {
	t.Helper()
	chunks, err := tool.Execute(context.Background(), input, state)
	if err != nil {
		t.Fatalf("%s Execute returned error: %v", tool.Name(), err)
	}
	response := astool.NewToolResponse("")
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			t.Fatalf("AppendChunk returned error: %v", err)
		}
	}
	return response
}

func textOutput(response *astool.ToolResponse) string {
	if response == nil {
		return ""
	}
	var builder strings.Builder
	for _, block := range response.Content {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}
