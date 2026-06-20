# Microsandbox Workspace Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example demonstrates `workspace/microsandbox.Workspace`:

- Start a Microsandbox-backed workspace from a Python image.
- Use workspace `Write` and `Read` tools against paths under `/workspace`.
- Keep offload, skill, and MCP indexes in a temporary host mirror directory.
- Send the workspace tool result to a DashScope ChatModel when an API key is available.

## Prerequisites

- Go 1.26.4.
- Linux with KVM enabled, or macOS with Apple Silicon.
- Microsandbox runtime support. The Go SDK downloads runtime assets into `~/.microsandbox/` on first initialization when they are missing.
- Optional DashScope API key for the live ChatModel call.

The default image is `python:3.12`.

## Run

```bash
cd example/workspace/microsandbox
go run .
```

Use a different image tag:

```bash
AGENTSCOPE_MICROSANDBOX_IMAGE=python:3.13 go run .
```

Use a fixed sandbox name:

```bash
AGENTSCOPE_MICROSANDBOX_NAME=agentscope-msb-demo go run .
```

Run the live ChatModel path:

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

## Expected Output

Without an API key, output includes:

```text
microsandbox_workspace_alive=true
read_has_brief=true
dashscope_live=skipped
```

With an API key, output also includes:

```text
dashscope_live=ok
```
