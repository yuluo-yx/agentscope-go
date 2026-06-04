# 示例

示例位于 `example/`。每个子目录都是独立 Go Module，包含自己的 `go.mod`、`main.go`、`README.md` 和 `README-zh.md`。

## 运行示例

```bash
cd example/tool/mcp
go run .
```

## 示例矩阵

| 目录 | 用途 |
| --- | --- |
| `message` | 构造系统、用户和助手消息 |
| `model/providers` | 构造模型供应商和估算 Token |
| `model/dashscope` | DashScope 聊天、工具 Schema、数据块输入和可选真实调用 |
| `agent/basic` | 使用脚本模型和任务工具的智能体示例 |
| `agent/configuration` | Agent model fallback、ReAct 配置和本地上下文清理 |
| `agent/context_strategy` | 摘要压缩、workspace offload 和自定义上下文策略 |
| `agent/external` | Agent 外部工具执行暂停与恢复流程 |
| `agent/hooks` | Agent middleware hook 示例，覆盖 reply、reasoning、model call、acting 和 system prompt |
| `agent/middleware_tracing` | reply、model call 和 tool execution span 的 tracing middleware |
| `agent/permission` | Agent 权限确认与恢复流程 |
| `integration/gin` | Gin HTTP 集成，演示底层 ChatModel 流式和 Agent 事件流式 |
| `integration/kratos` | Kratos HTTP 集成，演示底层 ChatModel 流式和 Agent 事件流式 |
| `tool/function` | 自定义函数工具 |
| `tool/builtin` | 内置本地工具 |
| `tool/mcp` | MCP 客户端和通过 Toolkit 执行 MCP 工具 |
| `tool/task` | 任务工具用法 |
| `tool/skill` | 加载本地 `SKILL.md` |
| `workspace/local` | 本地 Workspace 工具、Skill 和卸载能力 |
| `workspace/docker` | Docker Workspace 工具、容器文件操作和可选 ChatModel 回复 |

## 真实模型调用

设置 `AI_DASHSCOPE_API_KEY` 可以运行 DashScope 真实调用路径：

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

没有 Key 时，示例会尽可能保留本地或离线路径。
