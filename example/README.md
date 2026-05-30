# AgentScope Go Examples

Chinese documentation: [README-zh.md](README-zh.md).

This directory organizes examples by feature area. Each subdirectory is a standalone Go module with its own `go.mod`, `main.go`, `README.md`, and `README-zh.md`.

## Run Examples

Each subdirectory is an independent module. Enter the directory you want to try and run:

```bash
go run .
```

Model and tool examples demonstrate ChatModel tool-call loops where that matches the module purpose. Without `AI_DASHSCOPE_API_KEY`, those examples stay on an offline path and print tool schema/token information. With `AI_DASHSCOPE_API_KEY`, they run the full model -> tool call -> local tool execution -> tool result -> final model response loop.

## Example List

| Directory | Feature |
| --- | --- |
| `message` | Conversation history with system, user, and assistant messages |
| `model/providers` | Provider construction and token estimation |
| `model/dashscope` | DashScope OpenAI-compatible ChatModel, tool schemas, multimodal input, and optional live call |
| `agent/basic` | Agent + scripted model + task tool end-to-end ReAct flow |
| `tool/function` | Custom function tool |
| `tool/builtin` | Built-in Bash/Edit/Glob/Grep/Read/Write tools |
| `tool/task` | TaskCreate/TaskGet/TaskList/TaskUpdate |
| `tool/skill` | Local `SKILL.md` loading |
| `workspace/local` | Workspace-backed tool file operations, skills, context offload, and tool result offload |

## External Services

The examples use locally verifiable paths by default. To make real DashScope requests in model/tool examples, set:

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```
