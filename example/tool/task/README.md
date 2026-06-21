# Task Tool Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows how task tools read and write `AgentState.TaskContext`:

- `TaskCreate`: create a task.
- `TaskUpdate`: update status, owner, and metadata.
- `TaskList`: list task summaries.
- `TaskGet`: read one task in detail.
- Let DashScope ChatModel request `TaskGet`, execute it against `AgentState.TaskContext`, send a `ToolResultBlock` back, and print the final model response.

## Prerequisites

- Go 1.26.3.
- `AI_DASHSCOPE_API_KEY` for the DashScope model -> tool call -> tool result loop.

## Run

```bash
cd example/tool/task
go run .
```

## Expected Output

Output includes:

```text
task_tools=TaskCreate,TaskGet,TaskList,TaskUpdate
chat_tool=TaskGet
```
