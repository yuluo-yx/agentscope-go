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

package state

import (
	"fmt"
	"time"

	"github.com/yuluo-yx/agentscope-go/utils"
)

// TaskState represents the current task state.
type TaskState string

const (
	// TaskPending means the task has not started.
	TaskPending TaskState = "pending"
	// TaskInProgress means the task is currently running.
	TaskInProgress TaskState = "in_progress"
	// TaskCompleted means the task has completed.
	TaskCompleted TaskState = "completed"
)

// Task represents one task tracked during Agent execution.
type Task struct {
	Subject     string         `json:"subject"`
	Description string         `json:"description"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   string         `json:"created_at"`
	State       TaskState      `json:"state"`
	ID          string         `json:"id"`
	Owner       *string        `json:"owner,omitempty"`
	Blocks      []string       `json:"blocks,omitempty"`
	BlockedBy   []string       `json:"blocked_by,omitempty"`
}

// NewTask creates a task with the default pending state.
func NewTask(subject, description string, metadata map[string]any) Task {
	return Task{
		Subject:     subject,
		Description: description,
		Metadata:    utils.CloneAnyMap(metadata),
		CreatedAt:   nowRFC3339Nano(),
		State:       TaskPending,
		ID:          newTaskID(),
		Blocks:      []string{},
		BlockedBy:   []string{},
	}
}

// Validate validates task state and required fields.
func (t Task) Validate() error {
	if t.Subject == "" {
		return fmt.Errorf("agentscope: task subject is empty")
	}
	if t.ID == "" {
		return fmt.Errorf("agentscope: task id is empty")
	}
	if !isValidTaskState(t.State) {
		return fmt.Errorf("agentscope: invalid task state %q", t.State)
	}
	return nil
}

// Clone returns a deep copy of the task.
func (t Task) Clone() Task {
	cp := t
	cp.Metadata = utils.CloneAnyMap(t.Metadata)
	if t.Owner != nil {
		owner := *t.Owner
		cp.Owner = &owner
	}
	cp.Blocks = append([]string(nil), t.Blocks...)
	cp.BlockedBy = append([]string(nil), t.BlockedBy...)
	return cp
}

// TaskContext stores the Agent's current task list.
type TaskContext struct {
	Tasks []Task `json:"tasks"`
}

// NewTaskContext creates a task context.
func NewTaskContext() *TaskContext {
	return &TaskContext{Tasks: []Task{}}
}

// AddTask appends a task.
func (c *TaskContext) AddTask(task Task) {
	if c.Tasks == nil {
		c.Tasks = []Task{}
	}
	c.Tasks = append(c.Tasks, task.Clone())
}

// GetTask returns a task by ID.
func (c *TaskContext) GetTask(id string) (*Task, bool) {
	if c == nil {
		return nil, false
	}
	for index := range c.Tasks {
		if c.Tasks[index].ID == id {
			return &c.Tasks[index], true
		}
	}
	return nil, false
}

// UpdateTaskState updates task state.
func (c *TaskContext) UpdateTaskState(id string, state TaskState) error {
	if !isValidTaskState(state) {
		return fmt.Errorf("agentscope: invalid task state %q", state)
	}
	task, ok := c.GetTask(id)
	if !ok {
		return fmt.Errorf("agentscope: task %q not found", id)
	}
	task.State = state
	return nil
}

// Clone returns a deep copy of the task context.
func (c *TaskContext) Clone() *TaskContext {
	if c == nil {
		return nil
	}
	cp := &TaskContext{Tasks: make([]Task, 0, len(c.Tasks))}
	for _, task := range c.Tasks {
		cp.Tasks = append(cp.Tasks, task.Clone())
	}
	return cp
}

func isValidTaskState(state TaskState) bool {
	switch state {
	case TaskPending, TaskInProgress, TaskCompleted:
		return true
	default:
		return false
	}
}

func nowRFC3339Nano() string {
	return time.Now().Format(time.RFC3339Nano)
}

func newTaskID() string {
	return utils.NewID()
}
