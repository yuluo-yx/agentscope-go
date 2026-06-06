# Workspace

`workspace/local.Workspace` provides a local environment for tools and resources.

## Local Workspace

A local workspace initializes a directory with:

- `data/` for files used by tools,
- `skills/` for local skills,
- `sessions/` for offloaded context and tool results.

```go
ws, err := local.NewWorkspace("/tmp/agentscope-workspace")
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}
```

## Docker Workspace

`workspace/docker.Workspace` runs workspace tools inside a Docker container. It is useful for local isolated shell, file, and search workflows.

```go
ws, err := docker.NewWorkspace(
	docker.WithImage("ubuntu:latest"),
	docker.WithHostWorkdir("/tmp/agentscope-docker-workspace"),
)
if err != nil {
	panic(err)
}
```

When `WithHostWorkdir` is set, offload, skills, and MCP indexes are written to the host mirror directory.

## Agent Sandbox Workspace

`workspace/agentsandbox.Workspace` creates Kubernetes `SandboxClaim` resources through the agent-sandbox Go SDK and runs `Bash`, `Read`, `Write`, `Edit`, `Glob`, and `Grep` inside an Agent Sandbox runtime.

```go
ws, err := agentsandbox.NewWorkspace(
	agentsandbox.WithTemplateName("python-sandbox-template"),
	agentsandbox.WithNamespace("default"),
	agentsandbox.WithHostWorkdir("/tmp/agentscope-agent-sandbox-workspace"),
)
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}
```

Prerequisites:

- A reachable Kubernetes cluster.
- agent-sandbox controller, extensions, and sandbox-router installed.
- Current kubeconfig can create `SandboxClaim` resources.
- A `SandboxTemplate` exists in the target namespace. The examples use `python-sandbox-template`.

Connection modes:

- Default: port-forward mode for local and KinD tests.
- `WithAPIURL`: connect to a sandbox-router direct URL.
- `WithGateway`: connect through Kubernetes Gateway API.

The `Write` tool still accepts absolute paths. Because the agent-sandbox Go SDK `Write()` accepts only plain filenames, AgentScope-Go uploads a temporary file first and then moves it to the requested absolute path inside the sandbox.

## Tools

`ListTools` exposes built-in local file and shell tools:

```go
tools, err := ws.ListTools(ctx)
```

Register them in a Toolkit when the agent should use workspace-backed tools.

## Skills

Seed skills with `local.WithSkillPaths`:

```go
ws, err := local.NewWorkspace(
	"/tmp/agentscope-workspace",
	local.WithSkillPaths("./skills/review"),
)
```

## Offload

The workspace can offload conversation context and tool results to files. This keeps large content outside the active model context while preserving a retrievable record.
