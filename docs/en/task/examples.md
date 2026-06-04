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
| `agent/configuration` | Agent model fallback, ReAct config, and local context cleanup |
| `agent/context_strategy` | Summary compression, workspace offload, and custom context strategies |
| `agent/external` | Agent pause/resume flow for external tool execution |
| `agent/hooks` | Agent middleware hooks for reply, reasoning, model call, acting, and system prompt |
| `agent/middleware_tracing` | Tracing middleware for reply, model-call, and tool-execution spans |
| `agent/permission` | Agent permission confirmation and resume flow |
| `integration/gin` | Gin HTTP integration for direct ChatModel streams and Agent event streams |
| `integration/kratos` | Kratos HTTP integration for direct ChatModel streams and Agent event streams |
| `tool/function` | Custom function tool |
| `tool/builtin` | Built-in local tools |
| `tool/mcp` | MCP client and MCP tool execution through Toolkit |
| `tool/task` | Task tool usage |
| `tool/skill` | Local `SKILL.md` loading |
| `workspace/local` | Local workspace tools, skills, and offload |
| `workspace/docker` | Docker workspace tools, container file operations, and optional ChatModel response |

## Live Model Calls

Set `AI_DASHSCOPE_API_KEY` to run live DashScope paths:

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

Without the key, examples keep a local or offline path when possible.
