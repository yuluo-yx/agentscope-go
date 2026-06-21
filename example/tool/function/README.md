# Function Tool Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows how to wrap a Go function as an AgentScope tool:

- Declare a tool name, description, and JSON Schema.
- Implement a synchronous function handler.
- Execute the tool and accumulate `ToolChunk` values into a `ToolResponse`.
- Let DashScope ChatModel request the `Greet` tool, execute the tool locally, send a `ToolResultBlock` back, and print the final model response.

## Prerequisites

- Go 1.26.3.
- `AI_DASHSCOPE_API_KEY` for the DashScope model -> tool call -> tool result loop.

## Run

```bash
cd example/tool/function
go run .
```

## Expected Output

Output includes:

```text
function_tool=Greet
chat_tool=Greet
```
