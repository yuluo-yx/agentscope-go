# Hook System

AgentScope Go exposes hooks through `agent.Middleware`. Middleware values can implement one or more hook interfaces.

## Hook Types

| Interface | Purpose |
| --- | --- |
| `ReplyMiddleware` | Intercepts the full reply flow |
| `ReasoningMiddleware` | Intercepts model reasoning events |
| `ActingMiddleware` | Intercepts tool execution |
| `ModelCallMiddleware` | Intercepts raw model calls |
| `SystemPromptMiddleware` | Edits the system prompt before model calls |

## System Prompt Hook

```go
type PromptNote struct{}

func (PromptNote) MiddlewareName() string { return "prompt-note" }

func (PromptNote) OnSystemPrompt(ctx context.Context, accessor agent.AgentAccessor, prompt string) (string, error) {
	return prompt + "\nUse concise answers.", nil
}
```

Register it during agent construction:

```go
agent.WithMiddlewares(PromptNote{})
```

## Ordering

Middleware is ordered by priority and dependencies before it enters the hook chain. A middleware can call the next handler, inspect the result, replace it, or return an error.

The default priority is `0`. When a middleware implements `MiddlewarePriority()`, lower values wrap and run earlier. When it implements `MiddlewareDependsOn()`, the current middleware waits for the named dependencies:

```go
type PolicyPrompt struct{}

func (PolicyPrompt) MiddlewareName() string { return "policy-prompt" }
func (PolicyPrompt) MiddlewarePriority() int { return -10 }
func (PolicyPrompt) MiddlewareDependsOn() []string { return []string{"base-prompt"} }
```

Dependency names must exist. Cycles make `agent.NewAgent` return an error.

## Optional Tracing

Use `github.com/yuluo-yx/agentscope-go/pkg/middleware` for tracing middleware:

```go
agent.WithMiddlewares(middleware.NewTracingMiddleware(tracer))
```

`TracingMiddleware` depends on a small `middleware.Tracer` interface. The core `agent` package does not import OpenTelemetry. Applications that want OpenTelemetry can adapt a tracer with `github.com/yuluo-yx/agentscope-go/pkg/middleware/otel`.

## Optional TTS

`middleware.NewTTSMiddleware` can append synthesized audio to assistant reply
streams:

```go
speech, err := dashscopetts.NewModel(
	dashscopetts.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
	"qwen3-tts-flash",
)
agent.WithMiddlewares(middleware.NewTTSMiddleware(speech))
```

The middleware preserves the original text events. For batch models it collects
one text block and emits `DATA_BLOCK_START`, `DATA_BLOCK_DELTA`, and
`DATA_BLOCK_END` after the text block ends. Realtime models receive text deltas
through `Push` and are flushed with an empty `tts.Request` when the text block
ends.
