# Moonshot ChatModel Example

This example demonstrates live `model/moonshot` ChatModel usage. It covers Moonshot credential adaptation, multimodal messages, token estimation, non-streaming responses, local tool-call execution, tool-result follow-up, and streaming output.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Entry point | `main()` | Runs `chat()` and then `streamChat()`. |
| Model setup | `chat()`, `streamChat()` | Creates `moonshot-v1-8k` from `AI_MOONSHOT_API_KEY`. |
| Multimodal message | `message.NewDataBlock` | Builds a user message with text and an image URL. |
| Token estimation | `CountTokens` | Estimates context usage for messages plus tool schemas. |
| Non-streaming call | `ChatModel.Call` | Gets a complete model response. |
| Tool loop | `weatherTool()`, `kit.RunTool` | Executes `GetWeather` locally and sends the result back to the model. |
| Streaming call | `ChatModel.Stream` | Reads response chunks and assembles the final text. |

## Prerequisites

```bash
export AI_MOONSHOT_API_KEY="your-moonshot-key"
```

This example makes real Moonshot requests. Model construction or calls will fail when the key is missing or invalid.

## Run

```bash
cd example/model/moonshot/chat
export AI_MOONSHOT_API_KEY="your-moonshot-key"
go run .
```

Important output lines:

- `chat_model=moonshot:moonshot-v1-8k`: the model adapter was created.
- `tools=1 multimodal_blocks=2 estimated_tokens=...`: multimodal content and tool schemas were included in token estimation.
- `moonshot_live=ok`: non-streaming call completed.
- `moonshot_weather=ok`: tool call and tool-result follow-up completed.
- `moonshot_stream_delta=...`: streaming text delta.

## Code Walkthrough

### Model Construction

The Moonshot model is built separately for non-streaming and streaming paths:

```go
chat, err := moonshot.NewChatModel(
    credential.NewMoonshot(os.Getenv("AI_MOONSHOT_API_KEY")).ChatCredential(),
    "moonshot-v1-8k",
    moonshot.WithStream(false),
    moonshot.WithChatParameters(moonshot.ChatParameters{
        MaxTokens:   &maxTokens,
        Temperature: &temperature,
    }),
)
```

The streaming path uses `moonshot.WithStream(true)` with the same model and generation parameters.

### Multimodal Message

The example uses content blocks for text plus an image URL:

```go
visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
    message.NewTextBlock("Describe this image in one sentence."),
    message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
})
```

This demonstrates that the message model can carry URL-backed data sources, not just text.

### Token Estimation

The estimate includes both message content and tool schemas:

```go
tokens, err := chat.CountTokens(asmodel.CallRequest{
    Messages: []*message.Message{visionMessage},
    Tools:    schemas,
})
```

Tool schemas consume context budget. Estimate before calling when prompts or tool definitions can grow.

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

Both use `CallRequest`; the difference is whether the caller receives a full response or a channel of chunks.

### Tool-Call Loop

The model receives tool schemas:

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

The Go process executes the tool and sends back a `ToolResultBlock`:

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
})
```

## Troubleshooting

### Authentication Error

Check `AI_MOONSHOT_API_KEY` and confirm that the key can access `moonshot-v1-8k`.

### Image URL Is Not Reachable

The image URL in the example only demonstrates message shape. Replace it with a resource reachable by the model service in real usage.

### No Tool Call Returned

If the model does not return a tool call, the example panics. Tighten the prompt when deterministic tool usage is required.
