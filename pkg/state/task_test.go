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

package state_test

import (
	"testing"

	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
)

func TestTaskLifecycleHelpers(t *testing.T) {
	t.Parallel()

	task := statepkg.NewTask("Implement stage two", "root interfaces", nil)
	if task.ID == "" || task.CreatedAt == "" {
		t.Fatalf("task should create id and timestamp: %#v", task)
	}
	if task.State != statepkg.TaskPending {
		t.Fatalf("task should default to pending, got %q", task.State)
	}

	ctx := statepkg.NewTaskContext()
	ctx.AddTask(task)
	if err := ctx.UpdateTaskState(task.ID, statepkg.TaskInProgress); err != nil {
		t.Fatalf("UpdateTaskState returned error: %v", err)
	}
	if got, ok := ctx.GetTask(task.ID); !ok || got.State != statepkg.TaskInProgress {
		t.Fatalf("task state not updated: %#v ok=%v", got, ok)
	}
	if err := ctx.UpdateTaskState("missing", statepkg.TaskCompleted); err == nil {
		t.Fatal("missing task should return error")
	}
	if err := ctx.UpdateTaskState(task.ID, "unknown"); err == nil {
		t.Fatal("invalid task state should return error")
	}
}

func TestTaskValidationAndClone(t *testing.T) {
	t.Parallel()

	owner := "Friday"
	task := statepkg.NewTask("Subject", "Description", map[string]any{"nested": map[string]any{"phase": 2}})
	task.Owner = &owner
	task.Blocks = []string{"block-a"}
	task.BlockedBy = []string{"block-b"}
	if err := task.Validate(); err != nil {
		t.Fatalf("valid task returned error: %v", err)
	}

	cloned := task.Clone()
	cloned.Metadata["nested"].(map[string]any)["phase"] = 3
	*cloned.Owner = "Tony"
	cloned.Blocks[0] = "changed"
	if task.Metadata["nested"].(map[string]any)["phase"] != 2 || *task.Owner != "Friday" || task.Blocks[0] != "block-a" {
		t.Fatalf("task clone mutated original: %#v", task)
	}

	task.Subject = ""
	if err := task.Validate(); err == nil {
		t.Fatal("empty subject should return validation error")
	}
	task.Subject = "Subject"
	task.ID = ""
	if err := task.Validate(); err == nil {
		t.Fatal("empty id should return validation error")
	}
	task.ID = "task-1"
	task.State = "invalid"
	if err := task.Validate(); err == nil {
		t.Fatal("invalid state should return validation error")
	}
}

func TestTaskContextNilAndClone(t *testing.T) {
	t.Parallel()

	var taskContext *statepkg.TaskContext
	if _, ok := taskContext.GetTask("missing"); ok {
		t.Fatal("nil task context should not return task")
	}
	if taskContext.Clone() != nil {
		t.Fatal("nil task context clone should return nil")
	}

	taskContext = &statepkg.TaskContext{}
	task := statepkg.NewTask("Subject", "Description", nil)
	taskContext.AddTask(task)
	cloned := taskContext.Clone()
	cloned.Tasks[0].Subject = "changed"
	if taskContext.Tasks[0].Subject != "Subject" {
		t.Fatalf("task context clone mutated original: %#v", taskContext)
	}
}
