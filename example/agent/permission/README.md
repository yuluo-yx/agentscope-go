# Agent Permission Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows the Agent permission pause-and-resume flow:

- The model asks to call a write-like tool.
- The tool returns the default ask decision and provides a suggested rule.
- The Agent emits `RequireUserConfirmEvent` and ends the current reply.
- The host application sends `UserConfirmResultEvent`.
- The Agent resumes, executes the tool, and asks the model for the final reply.

The example uses a DashScope ChatModel and a local function tool. The model requests the write-like tool, and the host application controls confirmation.

## Prerequisites

- Go 1.26.3 or newer.
- `AI_DASHSCOPE_API_KEY` for the DashScope ChatModel.

## Run

```bash
cd example/agent/permission
go run .
```

Expected output:

```text
confirmation=required tool=WriteThing suggestions=1
confirmed_reply=... executed=true
```

## Test

```bash
go test .
```
