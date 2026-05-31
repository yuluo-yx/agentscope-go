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
	ToolResultLimit: 3000,
})
```

只需要覆盖少量字段时，可以先使用 `agent.DefaultContextConfig()` 获取默认值。

## 中间件

使用 `agent.WithMiddlewares` 注册中间件。中间件可以拦截回复、推理、模型调用、工具执行和系统提示词构造。
