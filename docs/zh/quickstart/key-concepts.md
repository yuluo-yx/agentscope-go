# 核心概念

AgentScope Go 围绕少量小接口组织。你可以单独使用这些接口，也可以通过 `agent.Agent` 组合成完整智能体。

## Message

`message.Message` 是对话单元，包含角色、发送者名称、时间戳、元数据和内容块列表。

常见内容块包括：

- `TextBlock`：文本内容。
- `DataBlock`：Base64 或 URL 媒体数据。
- `ToolCallBlock`：模型请求执行的工具调用。
- `ToolResultBlock`：工具执行结果。

## Model

`model.ChatModel` 是聊天模型的统一接口：

```go
type ChatModel interface {
	Name() string
	Call(context.Context, CallRequest) (*ChatResponse, error)
	Stream(context.Context, CallRequest) (<-chan ChatResponse, error)
	CountTokens(CallRequest) (int, error)
}
```

模型供应商包负责把 AgentScope 的消息和工具 Schema 转换为各自 SDK 的格式。

## Tool

`tool.Tool` 表达一个可调用能力。工具会暴露名称、描述、JSON Schema、权限行为和执行方法。

使用 `tool.NewToolkit` 注册工具，并执行模型返回的 `ToolCallBlock`。

## Agent

`agent.Agent` 组合模型、工具提供者、状态、权限引擎和中间件钩子。它处理以下循环：

1. 向模型发送消息。
2. 读取模型回复中的工具调用。
3. 检查工具权限。
4. 执行工具。
5. 追加工具结果。
6. 直到模型返回最终文本。

## State

`state.AgentState` 保存运行期状态：

- 对话上下文。
- 权限上下文。
- 工具读取缓存。
- 任务上下文。
- 当前迭代计数。

## Workspace

`workspace/local.Workspace` 提供本地执行环境，用于文件工具、Skill 加载、上下文卸载和工具结果卸载。
