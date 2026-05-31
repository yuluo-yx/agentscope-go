# AgentScope Go 示例

英文文档：[README.md](README.md)。

本目录按功能模块组织示例。每个子目录都是独立 Go 项目，包含自己的 `go.mod`、`main.go`、`README.md` 和 `README-zh.md`，可以单独进入目录运行。

## 运行示例

每个子目录都是独立 Go module。进入要体验的目录后运行：

```bash
go run .
```

模型和工具相关示例会在符合模块用途时演示 ChatModel 调用工具的闭环。没有 `AI_DASHSCOPE_API_KEY` 时，这些示例走离线路径并输出工具 schema/token 信息；设置后会运行完整的 model -> tool call -> 本地工具执行 -> tool result -> 最终模型回复流程。

## 示例列表

| 目录 | 功能 |
| --- | --- |
| `message` | system、user、assistant 消息组成的对话历史 |
| `model/providers` | 已实现 provider 的构造与 token 估算 |
| `model/dashscope` | DashScope OpenAI-compatible ChatModel、工具 schema、数据块输入和可选真调用 |
| `agent/basic` | Agent + scripted model + task tool 的端到端 ReAct 流程 |
| `agent/configuration` | Agent model fallback、ReAct 配置和本地上下文清理 |
| `agent/external` | Agent 外部工具执行的暂停与恢复流程 |
| `agent/hooks` | Agent middleware hook 示例，覆盖 reply、reasoning、model call、acting 和 system prompt |
| `agent/permission` | Agent 权限确认与恢复流程 |
| `integration/gin` | Gin HTTP 集成，覆盖底层 ChatModel 流式与 Agent 事件流式 |
| `integration/kratos` | Kratos HTTP 集成，覆盖底层 ChatModel 流式与 Agent 事件流式 |
| `tool/function` | 自定义函数工具 |
| `tool/builtin` | Bash/Edit/Glob/Grep/Read/Write 内置工具 |
| `tool/mcp` | MCP client 集成、MCP tool 包装、Toolkit 执行和可选真实 ChatModel 工具调用 |
| `tool/task` | TaskCreate/TaskGet/TaskList/TaskUpdate |
| `tool/skill` | 本地 `SKILL.md` 加载 |
| `workspace/local` | workspace 支撑的工具文件操作、skills、上下文与工具结果 offload |

## 外部服务

默认示例只走本地可验证路径。模型/工具示例要发起 DashScope 真请求时设置：

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```
