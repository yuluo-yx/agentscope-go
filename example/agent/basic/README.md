# Basic Agent Example

Chinese documentation: [README-zh.md](README-zh.md).

This example shows a minimal end-to-end Agent flow:

- Use a scripted ChatModel so no external model service is required.
- The first model response asks the Agent to call `TaskCreate`.
- The Agent executes the task tool and appends the tool result to context.
- The second model response produces the final assistant reply.

## Prerequisites

- Go 1.26.3.
- No API key is required.

## Run

```bash
cd example/agent/basic
go run .
```

## Expected Output

Output includes:

```text
agent_reply=task tracked
```
