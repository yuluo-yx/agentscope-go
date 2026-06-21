# Basic Agent Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows a minimal end-to-end Agent flow:

- Create a DashScope ChatModel from `AI_DASHSCOPE_API_KEY`.
- The model response asks the Agent to call `TaskCreate`.
- The Agent executes the task tool and appends the tool result to context.
- The second model response produces the final assistant reply.
- The example consumes `Agent.ReplyStream` so you can see the Agent event stream.

## Prerequisites

- Go 1.26.3.
- `AI_DASHSCOPE_API_KEY` for the DashScope ChatModel.

## Run

```bash
cd example/agent/basic
go run .
```

## Expected Output

Output includes:

```text
agent_stream=...
tasks=1
events=tool_call:TaskCreate,tool_result:success
```
