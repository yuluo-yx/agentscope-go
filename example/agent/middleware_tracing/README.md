# Agent Middleware Tracing Example

This example demonstrates `middleware.NewTracingMiddleware` with an in-memory tracer.

It runs a full local ReAct loop:

1. the scripted model asks for the `Echo` tool;
2. the Agent executes the local function tool;
3. the model receives the tool result and returns the final reply;
4. tracing middleware records reply, model-call, and tool-execution spans.

## Run

```bash
go run .
```

Expected output:

```text
reply="trace complete" spans=invoke_agent Friday,chat scripted-tracing,execute_tool Echo,chat scripted-tracing tool_result="echo Ada"
```

The example does not require OpenTelemetry. Applications can provide an adapter from an OpenTelemetry tracer to `middleware.Tracer` when they want to export spans.
