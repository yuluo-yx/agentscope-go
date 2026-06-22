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
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	astate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

func TestTaskHelperValueAndRelationBranches(t *testing.T) {
	t.Parallel()

	if got := stringValue(nil, "missing"); got != "" {
		t.Fatalf("stringValue nil = %q", got)
	}
	if got := stringValue(map[string]any{"count": 7}, "count"); got != "7" {
		t.Fatalf("stringValue should format non-string values, got %q", got)
	}
	if value, ok := optionalString(nil, "missing"); ok || value != "" {
		t.Fatalf("optionalString nil mismatch: value=%q ok=%v", value, ok)
	}
	if value, ok := optionalString(map[string]any{"count": 7}, "count"); !ok || value != "7" {
		t.Fatalf("optionalString should format non-string values: value=%q ok=%v", value, ok)
	}
	if metadata, ok := optionalMetadata(map[string]any{"metadata": "invalid"}, "metadata"); ok || metadata != nil {
		t.Fatalf("optionalMetadata should reject non-map metadata: %#v ok=%v", metadata, ok)
	}
	metadata := metadataValue(map[string]any{"metadata": map[string]any{"phase": "one"}}, "metadata")
	metadata["phase"] = "changed"
	original := metadataValue(map[string]any{"metadata": map[string]any{"phase": "one"}}, "metadata")
	if original["phase"] != "one" {
		t.Fatalf("metadataValue should clone metadata maps: %#v", original)
	}

	stringsInput := map[string]any{"ids": []string{"a", "b"}}
	stringsOut := stringSliceValue(stringsInput, "ids")
	stringsOut[0] = "changed"
	if stringsInput["ids"].([]string)[0] != "a" {
		t.Fatalf("stringSliceValue should clone []string inputs: %#v", stringsInput["ids"])
	}
	if got := stringSliceValue(map[string]any{"ids": []any{"a", nil, 2}}, "ids"); strings.Join(got, ",") != "a,2" {
		t.Fatalf("stringSliceValue []any mismatch: %#v", got)
	}
	if got := stringSliceValue(map[string]any{"ids": 42}, "ids"); got != nil {
		t.Fatalf("stringSliceValue should reject unsupported input, got %#v", got)
	}

	taskContext := astate.NewTaskContext()
	first := astate.NewTask("first", "first task", nil)
	second := astate.NewTask("second", "second task", nil)
	taskContext.AddTask(first)
	taskContext.AddTask(second)
	if taskIndex(nil, first.ID) != -1 || taskIndex(taskContext, "missing") != -1 || taskIndex(taskContext, second.ID) != 1 {
		t.Fatalf("taskIndex mismatch")
	}

	existing := existingTaskIDs(taskContext)
	candidates := []string{" " + second.ID + " ", first.ID, "missing", ""}
	if got := newRelationIDs(candidates, []string{first.ID}, existing); len(got) != 1 || got[0] != second.ID {
		t.Fatalf("newRelationIDs mismatch: %#v", got)
	}
	if !containsString([]string{"a", "b"}, "b") || containsString([]string{"a", "b"}, "c") {
		t.Fatalf("containsString mismatch")
	}
	if got := removeString([]string{"a", "b", "a"}, "a"); strings.Join(got, ",") != "b" {
		t.Fatalf("removeString mismatch: %#v", got)
	}
}

func TestTaskToolExecutionMessageBranches(t *testing.T) {
	t.Parallel()

	state := astate.NewAgentState()
	if state.TaskContext == nil {
		state.TaskContext = astate.NewTaskContext()
	}
	emptyList := taskResponse(t, NewTaskList(), nil, state)
	if text := emptyList.GetTextContent(""); emptyList.State != message.ToolResultSuccess || text == nil || *text != "No tasks available." {
		t.Fatalf("TaskList empty response mismatch: %#v text=%#v", emptyList, text)
	}

	missingSubject := taskResponse(t, NewTaskCreate(), map[string]any{"description": "no subject"}, state)
	if text := missingSubject.GetTextContent(""); missingSubject.State != message.ToolResultError || text == nil || !strings.Contains(*text, "subject is required") {
		t.Fatalf("TaskCreate missing subject mismatch: %#v text=%#v", missingSubject, text)
	}

	task := astate.NewTask("finish", "finish task", map[string]any{"keep": "yes"})
	state.TaskContext.AddTask(task)
	noUpdate := taskResponse(t, NewTaskUpdate(), map[string]any{"task_id": task.ID}, state)
	if text := noUpdate.GetTextContent(""); noUpdate.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, "No updates were made") {
		t.Fatalf("TaskUpdate no-op response mismatch: %#v text=%#v", noUpdate, text)
	}
	completed := taskResponse(t, NewTaskUpdate(), map[string]any{
		"task_id": task.ID,
		"status":  string(astate.TaskCompleted),
	}, state)
	if text := completed.GetTextContent(""); completed.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, "Task completed") {
		t.Fatalf("TaskUpdate completed response mismatch: %#v text=%#v", completed, text)
	}

	if taskResponse(t, NewTaskGet(), map[string]any{"task_id": task.ID}, state).State != message.ToolResultSuccess {
		t.Fatalf("TaskGet should still find completed task")
	}
}

func taskResponse(t *testing.T, tool astool.Tool, input map[string]any, state *astate.AgentState) *astool.ToolResponse {
	t.Helper()

	chunks, err := tool.Execute(context.Background(), input, state)
	if err != nil {
		t.Fatalf("%s Execute returned error: %v", tool.Name(), err)
	}
	response := astool.NewToolResponse(astool.WithToolResponseID("task-call"))
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			t.Fatalf("AppendChunk returned error: %v", err)
		}
	}
	return response
}
