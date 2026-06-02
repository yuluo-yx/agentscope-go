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

`Call` and `Stream` both use `ChatResponse`. Use the content query helpers to read text blocks or tool calls:

```go
text := response.GetTextContent()
if text != nil {
	fmt.Println(*text)
}

toolCalls := response.GetContentBlocks("tool_call")
if len(toolCalls) > 0 {
	fmt.Println(toolCalls[0].BlockID())
}
```
