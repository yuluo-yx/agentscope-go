# Agent External Execution Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows how an Agent pauses for a tool that must run outside the current Go process:

- The model asks for `DeployJob`.
- The tool is registered as an external tool.
- The Agent emits `RequireExternalExecutionEvent` instead of calling `Execute`.
- The host application performs the work and sends `ExternalExecutionResultEvent`.
- The Agent resumes and asks the model for the final reply.

The example uses a scripted ChatModel and a local placeholder tool definition, so it runs without API keys or external services.

## Prerequisites

- Go 1.26.3 or newer.
- No API key is required.

## Run

```bash
cd example/agent/external
go run .
```

Expected output:

```text
external=required tool=DeployJob calls=1
external_reply=deployment recorded result_state=success
```

## Test

```bash
go test .
```
