# Docker Workspace Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example demonstrates `workspace/docker.Workspace`:

- Start a Docker-backed workspace from an Ubuntu image.
- Bind a temporary host workspace directory into the container.
- Use workspace `Write` and `Read` tools against container paths under `/workspace`.
- Build system, user, and assistant messages from the Docker workspace instructions and tool output.
- Send those messages to a DashScope ChatModel when an API key is available.

## Prerequisites

- Go 1.26.3.
- Docker daemon running and reachable by the current user.
- A local Ubuntu image. The example defaults to `ubuntu:latest`.
- Optional DashScope API key for the live ChatModel call.

The Docker workspace needs an image with `/bin/bash`, `sleep`, `find`, and `grep`.

## Run

```bash
cd example/workspace/docker
go run .
```

Use a different local image tag:

```bash
AGENTSCOPE_DOCKER_IMAGE=ubuntu:24.04 go run .
```

Run the live ChatModel path:

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

## Expected Output

Without an API key, output includes:

```text
docker_workspace_alive=true
read_has_brief=true
dashscope_live=skipped
```

With an API key, output also includes:

```text
dashscope_live=ok
```
