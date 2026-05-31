# Models

The `model` package defines the provider contract. Provider packages adapt SDK-specific messages, tool schemas, streaming events, and token counting to this contract.

## ChatModel

```go
type ChatModel interface {
	Name() string
	Call(context.Context, CallRequest) (*ChatResponse, error)
	Stream(context.Context, CallRequest) (<-chan ChatResponse, error)
	CountTokens(CallRequest) (int, error)
}
```

## Providers

| Package | Notes |
| --- | --- |
| `model/openai` | OpenAI SDK integration |
| `model/anthropic` | Anthropic SDK integration |
| `model/dashscope` | DashScope OpenAI-compatible endpoint |
| `model/deepseek` | OpenAI-compatible wrapper |
| `model/moonshot` | OpenAI-compatible wrapper |
| `model/xai` | OpenAI-compatible wrapper |
| `model/ollama` | Ollama official Go API |

## Tool Schemas

Tools are passed to models as OpenAI-compatible function schemas:

```go
schemas, err := kit.ToolSchemas()
response, err := chat.Call(ctx, model.CallRequest{
	Messages: messages,
	Tools:    schemas,
})
```
