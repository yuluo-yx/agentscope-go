# Agent Hooks Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows how one middleware value can implement every Agent hook:

- `OnReply` wraps the whole reply lifecycle.
- `OnReasoning` wraps each reasoning pass.
- `OnSystemPrompt` edits the system prompt before model input is built.
- `OnModelCall` edits the `CallRequest` before `ChatModel.Stream` runs and observes the model response stream.
- `OnActing` wraps local tool execution and observes streamed tool chunks.

The example uses a DashScope ChatModel and the local `TaskCreate` tool, so hooks observe a real model stream and local tool execution.

## Prerequisites

- Go 1.26.3 or newer.
- `AI_DASHSCOPE_API_KEY` for the DashScope ChatModel.

## Run

```bash
cd example/agent/hooks
go run .
```

Expected output includes the final reply, created task count, and hook trace:

```text
reply=...
tasks=1
trace=reply:before,reasoning:before,system_prompt:Friday,model_call:before,...
```

## Test

```bash
go test .
```
