# 智能体配置

`agent.NewAgent` 使用显式 Go Option 配置智能体，不依赖 YAML 或 JSON 配置文件。

## 构造函数

```go
agent, err := agent.NewAgent(
	"Friday",
	"You are concise and use tools when useful.",
	chatModel,
	agent.WithToolkit(kit),
	agent.WithAgentState(state.NewAgentState()),
)
```

## Workspace 资源

当智能体需要直接使用 Workspace 时，使用 `agent.WithWorkspace(ctx, ws)`。
该 option 会初始化 Workspace，把 Workspace 指令和 Skill 指令追加到 system
prompt，注册 Workspace 与 MCP 工具，并将 Workspace 作为上下文、工具结果和
DataBlock 的 offloader。

如果服务层已经调用 `workspace.BuildAgentResources` 生成资源对象，可以使用
`agent.WithAgentResources(resources)` 装配同一组 system prompt 片段、toolkit
和 offloader，避免调用方手动拆分资源对象。

## 模型配置

`ModelConfig` 控制重试行为和可选的兜底模型：

```go
agent.WithModelConfig(agent.ModelConfig{
	MaxRetries:    3,
	FallbackModel: fallbackModel,
})
```

## ReAct 配置

`ReActConfig` 控制推理和行动循环：

```go
agent.WithReActConfig(agent.ReActConfig{
	MaxIters:     10,
	StopOnReject: true,
})
```

## 上下文配置

`ContextConfig` 定义压缩阈值、摘要提示词、摘要 Schema 和工具结果截断长度：

```go
agent.WithContextConfig(agent.ContextConfig{
	TriggerRatio:    0.8,
	ReserveRatio:    0.1,
	MaxTokens:       32000,
	ToolResultLimit: 3000,
})
```

`MaxTokens` 用于启用上下文压力跟踪和摘要压缩。为 `0` 时，默认策略链只保留既有的轻量清理行为：在配置 offloader 时卸载 base64 `DataBlock`，再截断或卸载超长工具结果。

为正数时，默认策略链会包含 `ThresholdContextStrategy`。该策略会写入
`AgentState.ContextStatus`，并按剩余 token 数执行三档渐进响应：

| 阈值 | 默认值 | 行为 |
| --- | ---: | --- |
| Warning | `20000` | 记录 `warning` 状态 |
| Compact | `13000` | 自动摘要压缩旧上下文 |
| Blocking | `3000` | 压缩后仍不足时返回 `ContextWindowError` |

内置绝对阈值仅在 `MaxTokens` 大于 Warning 阈值时自动生效。较小模型窗口或测试场景需要三档行为时，应显式配置更小的
`ThresholdContextStrategy` 阈值。

既有摘要策略仍作为基于比例的兜底：当前请求超过 `TriggerRatio * MaxTokens` 时，摘要策略会保留最新上下文，让模型生成结构化摘要，并通过已配置的 offloader 或 workspace 卸载被压缩的旧消息。

替换策略链时可以自定义三档阈值：

```go
agent.WithContextStrategies(
	agent.NewToolResultContextStrategy(),
	agent.ThresholdContextStrategy{
		WarningThreshold:  20000,
		CompactThreshold:  13000,
		BlockingThreshold: 3000,
	},
	agent.NewSummaryContextStrategy(),
)
```

应用需要自定义存储或压缩策略时，可以替换上下文策略链：

```go
agent.WithContextStrategies(customStrategy)
```

只需要覆盖少量字段时，可以先使用 `agent.DefaultContextConfig()` 获取默认值。

## 中间件

使用 `agent.WithMiddlewares` 注册中间件。中间件可以拦截回复、推理、模型调用、工具执行和系统提示词构造。

可选 `middleware` 包提供 tracing middleware，不让 tracing 进入核心 `agent` 包路径：

```go
agent.WithMiddlewares(middleware.NewTracingMiddleware(tracer))
```

需要接入 OpenTelemetry 时，再显式使用 `middleware/otel` 子包。
