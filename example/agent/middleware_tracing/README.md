# Agent Middleware Tracing Example

This example demonstrates `middleware.NewTracingMiddleware` with an in-memory tracer.

It runs a full ReAct loop with a DashScope ChatModel:

1. the model asks for the `Echo` tool;
2. the Agent executes the local function tool;
3. the model receives the tool result and returns the final reply;
4. tracing middleware records reply, model-call, and tool-execution spans.

Set `AI_DASHSCOPE_API_KEY` before running the example.

## Run

```bash
go run .
```

Expected output:

```text
reply="..." spans=invoke_agent Friday,chat dashscope/qwen3.7-max,execute_tool Echo,... tool_result="echo Ada"
```

The example does not require OpenTelemetry. Applications can provide an adapter from an OpenTelemetry tracer to `middleware.Tracer` when they want to export spans.
