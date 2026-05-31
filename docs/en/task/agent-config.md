# Agent Configuration

`agent.NewAgent` accepts explicit Go options instead of a YAML or JSON agent configuration file.

## Constructor

```go
agent, err := agent.NewAgent(
	"Friday",
	"You are concise and use tools when useful.",
	chatModel,
	agent.WithToolkit(kit),
	agent.WithAgentState(state.NewAgentState()),
)
```

## Model Configuration

`ModelConfig` controls retry behavior and the optional fallback model:

```go
agent.WithModelConfig(agent.ModelConfig{
	MaxRetries:    3,
	FallbackModel: fallbackModel,
})
```

## ReAct Configuration

`ReActConfig` controls the reasoning and acting loop:

```go
agent.WithReActConfig(agent.ReActConfig{
	MaxIters:     10,
	StopOnReject: true,
})
```

## Context Configuration

`ContextConfig` defines compression thresholds, summary prompts, summary schema, and tool-result truncation:

```go
agent.WithContextConfig(agent.ContextConfig{
	TriggerRatio:    0.8,
	ReserveRatio:    0.1,
	ToolResultLimit: 3000,
})
```

Use `agent.DefaultContextConfig()` when you only need to override one field.

## Middleware

Register middleware with `agent.WithMiddlewares`. Middleware can intercept replies, reasoning, model calls, tool execution, and system-prompt construction.
