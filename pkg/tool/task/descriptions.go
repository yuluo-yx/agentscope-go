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

const taskCreateDescription = `Use this tool to create a structured task list for your current session. This helps you track progress, organize complex tasks, and demonstrate thoroughness to the user.
It also helps the user understand the progress of the task and overall progress of their requests.

## When to Use This Tool
Use this tool proactively in these scenarios:

- Complex multi-step tasks - When a task requires 3 or more distinct steps or actions
- Non-trivial and complex tasks - Tasks that require careful planning or multiple operations
- Plan mode - When using plan mode, create a task list to track the work
- User explicitly requests todo list - When the user directly asks you to use the todo list
- User provides multiple tasks - When users provide a list of things to be done (numbered or comma-separated)
- After receiving new instructions - Immediately capture user requirements as tasks
- When you start working on a task - Mark it as in_progress BEFORE beginning work
- After completing a task - Mark it as completed and add any new follow-up tasks discovered during implementation

## When NOT to Use This Tool

Skip using this tool when:
- There is only **one single, straightforward** task
- The task is trivial and tracking it provides no organizational benefit
- The task can be completed in less than 3 trivial steps
- The task is purely conversational or informational

NOTE that you should **NOT** use this tool if there is only one trivial task to do. In this case you are better off just doing the task directly.

## Task Fields

- **subject**: A brief, actionable title in imperative form (e.g., "Fix authentication bug in login flow")
- **description**: What needs to be done

All tasks are created with status ` + "`pending`" + `.

## Tips

- Create tasks with clear, specific subjects that describe the outcome
- After creating tasks, use TaskUpdate to set up dependencies (blocks/blockedBy) if needed
- Check TaskList first to avoid creating duplicate tasks`

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
