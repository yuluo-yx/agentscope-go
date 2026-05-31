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

Middleware runs in registration order. A middleware can call the next handler, inspect the result, replace it, or return an error.
