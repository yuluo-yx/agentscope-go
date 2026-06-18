# xAI ChatModel Example

This example demonstrates live `model/xai` ChatModel usage. It covers xAI credentials, generation parameters, multimodal messages, token estimation, non-streaming responses, local tool-call execution, tool-result follow-up, and streaming output.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Entry point | `main()` | Runs `chat()` and then `streamChat()`. |
| Model setup | `chat()`, `streamChat()` | Creates `grok-3` from `AI_XAI_API_KEY`. |
| Multimodal message | `message.NewDataBlock` | Builds a user message with text and an image URL. |
| Token estimation | `CountTokens` | Estimates context usage for messages plus tool schemas. |
| Non-streaming call | `ChatModel.Call` | Gets a complete model response. |
| Tool loop | `weatherTool()`, `kit.RunTool` | Executes `GetWeather` locally and sends the result back to the model. |
| Streaming call | `ChatModel.Stream` | Reads response chunks and assembles the final text. |

## Prerequisites

```bash
export AI_XAI_API_KEY="your-xai-key"
```

This example makes real xAI requests. Model construction or calls will fail when the key is missing or invalid.

## Run

```bash
cd example/model/xai/chat
export AI_XAI_API_KEY="your-xai-key"
go run .
```

Important output lines:

- `chat_model=xai:grok-3`: the model adapter was created.
- `tools=1 multimodal_blocks=2 estimated_tokens=...`: multimodal content and tool schemas were included in token estimation.
- `xai_live=ok`: non-streaming call completed.
- `xai_weather=ok`: tool call and tool-result follow-up completed.
- `xai_stream_delta=...`: streaming text delta.

## Code Walkthrough

### Model Construction

The xAI example uses provider credentials directly:

```go
chat, err := xai.NewChatModel(
    xai.NewCredential(os.Getenv("AI_XAI_API_KEY")),
    "grok-3",
    xai.WithStream(false),
    xai.WithChatParameters(xai.ChatParameters{
        MaxTokens:   &maxTokens,
        Temperature: &temperature,
    }),
)
```

The non-streaming path uses `xai.WithStream(false)`; the streaming path uses `xai.WithStream(true)`.

### Multimodal Message

The example builds a text-plus-image message:

```go
visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
    message.NewTextBlock("Describe this image in one sentence."),
    message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
})
```

This demonstrates AgentScope Go's content-block message model. Replace the URL with a reachable image in real usage.

### Token Estimation

Tool schemas are included in the estimate:

```go
tokens, err := chat.CountTokens(asmodel.CallRequest{
    Messages: []*message.Message{visionMessage},
    Tools:    schemas,
})
```

Use the estimate for logs, budget checks, and context trimming.

### Non-Streaming and Streaming Calls

Non-streaming call:

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

Streaming call:

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

The streaming path prints `xai_stream_delta` chunks and then the final response.

### Tool-Call Loop

The model receives tool schemas:

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

The Go process executes the tool and sends back the result:

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
})
```

## Troubleshooting

### Authentication Error

Check `AI_XAI_API_KEY` and confirm that the key can access `grok-3`.

### Image URL Is Not Reachable

The example URL only demonstrates message shape. Replace it with an image reachable by the xAI service in real usage.

### No Tool Call Returned

If the model does not return a tool call, the example panics. Tighten the prompt when deterministic tool usage is required.
