# Examples

Examples live under `example/`. Each subdirectory is an independent Go module with its own `go.mod`, `main.go`, `README.md`, and `README-zh.md`.

## Run an Example

```bash
cd example/tool/mcp
go run .
```

## Example Matrix

| Directory | Purpose |
| --- | --- |
| `message` | System, user, and assistant message construction |
| `model/providers` | Provider construction and token estimation |
| `model/dashscope` | DashScope chat, tool schemas, data-block input, and optional live call |
| `agent/basic` | Agent with scripted model and task tool |
| `tool/function` | Custom function tool |
| `tool/builtin` | Built-in local tools |
| `tool/mcp` | MCP client and MCP tool execution through Toolkit |
| `tool/task` | Task tool usage |
| `tool/skill` | Local `SKILL.md` loading |
| `workspace/local` | Local workspace tools, skills, and offload |

## Live Model Calls

Set `AI_DASHSCOPE_API_KEY` to run live DashScope paths:

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

Without the key, examples keep a local or offline path when possible.
