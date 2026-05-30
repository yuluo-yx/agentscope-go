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

package task

const taskCreateDescription = `Use this tool to create a structured task list for your current coding session. This helps track progress, organize complex tasks, and show the user the current work state.

When to use this tool:
- Complex multi-step tasks that require several actions
- Non-trivial implementation work that benefits from explicit progress tracking
- User requests that contain multiple tasks
- New instructions that should be captured as tasks

Task fields:
- subject: a brief, actionable title
- description: what needs to be done
- metadata: optional structured metadata

All tasks are created with status pending.`

const taskListDescription = `Use this tool to list all tasks in the task list.

The output returns one summary line for each task:
- id: task identifier for TaskGet and TaskUpdate
- subject: brief task description
- status: pending, in_progress, or completed
- owner: agent ID if assigned
- blockedBy: open task IDs that must be resolved first

Prefer working on tasks in ID order when multiple tasks are available.`

const taskGetDescription = `Use this tool to retrieve a task by its ID from the task list.

Use TaskGet when you need full task details before starting work, including the detailed description, owner, dependencies, and metadata.`

const taskUpdateDescription = `Use this tool to update a task in the task list.

Supported updates:
- status: pending, in_progress, completed, or deleted
- subject: replace the task title
- description: replace the task description
- owner: assign or replace the owner
- metadata: merge metadata keys, with null deleting a key
- add_blocks: mark tasks that cannot start until this one completes
- add_blocked_by: mark tasks that must complete before this one can start

Only mark a task as completed when the requested work has actually been finished. Use deleted to permanently remove a task.`
