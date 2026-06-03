# AgentScope Go Examples

Project home: [README.md](../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This directory organizes examples by feature area. Each subdirectory is a standalone Go module with its own `go.mod`, `main.go`, `README.md`, and `README-zh.md`.

## Run Examples

Each subdirectory is an independent module. Enter the directory you want to try and run:

```bash
go run .
```

Model and tool examples demonstrate ChatModel tool-call loops where that matches the module purpose. Without an API key, those examples stay on an offline path and print tool schema/token information. With one global API key, they run the full model -> tool call -> local tool execution -> tool result -> final model response loop.

## Example List

| Directory | Feature |
| --- | --- |
| `message` | Conversation history with system, user, and assistant messages |
| `model/providers` | Provider construction and token estimation |
| `model/dashscope` | DashScope OpenAI-compatible ChatModel, tool schemas, data-block input, and optional live call |
| `agent/basic` | Agent + scripted model + task tool end-to-end ReAct flow |
| `agent/configuration` | Agent model fallback, ReAct config, and local context cleanup |
| `agent/external` | Agent pause/resume flow for tools executed outside the Go process |
| `agent/hooks` | Agent middleware hooks for reply, reasoning, model call, acting, and system prompt |
| `agent/permission` | Agent permission confirmation and resume flow |
| `integration/gin` | Gin HTTP integration with direct ChatModel streaming and Agent event streaming |
| `integration/kratos` | Kratos HTTP integration with direct ChatModel streaming and Agent event streaming |
| `tool/function` | Custom function tool |
| `tool/builtin` | Built-in Bash/Edit/Glob/Grep/Read/Write tools |
| `tool/mcp` | MCP client integration, MCP tool wrapping, Toolkit execution, and optional live ChatModel tool call |
| `tool/task` | TaskCreate/TaskGet/TaskList/TaskUpdate |
| `tool/skill` | Local `SKILL.md` loading |
| `workspace/local` | Workspace-backed tool file operations, skills, context offload, and tool result offload |
| `workspace/docker` | Docker workspace tools, container file operations, and optional ChatModel response |

## External Services

The examples use locally verifiable paths by default. To make real DashScope requests in model/tool examples, set one key once:

```bash
export AI_API_KEY=your-key
```

Optional overrides:

```bash
export AI_DASHSCOPE_API_KEY=your-dashscope-key  # provider-specific key (higher priority)
export AI_MODEL=qwen3.7-max                     # global model override
export AI_DASHSCOPE_MODEL=qwen3.7-max           # provider-specific model (higher priority)
```
