# Workspace

`workspace.LocalWorkspace` provides a local environment for tools and resources.

## Local Workspace

A local workspace initializes a directory with:

- `data/` for files used by tools,
- `skills/` for local skills,
- `sessions/` for offloaded context and tool results.

```go
ws, err := workspace.NewLocalWorkspace("/tmp/agentscope-workspace")
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}
```

## Tools

`ListTools` exposes built-in local file and shell tools:

```go
tools, err := ws.ListTools(ctx)
```

Register them in a Toolkit when the agent should use workspace-backed tools.

## Skills

Seed skills with `workspace.WithSkillPaths`:

```go
ws, err := workspace.NewLocalWorkspace(
	"/tmp/agentscope-workspace",
	workspace.WithSkillPaths("./skills/review"),
)
```

## Offload

The workspace can offload conversation context and tool results to files. This keeps large content outside the active model context while preserving a retrievable record.
