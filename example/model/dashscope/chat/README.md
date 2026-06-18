# DashScope ChatModel Example

This example demonstrates the OpenAI-compatible `model/dashscope` ChatModel. It covers multimodal message construction, token estimation, non-streaming responses, local tool-call execution, tool-result follow-up, and streaming output.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Entry point | `main()` | Runs `chat()` and then `streamChat()`. |
| Model setup | `chat()`, `streamChat()` | Creates `qwen3.7-max` from `AI_DASHSCOPE_API_KEY`. |
| Multimodal message | `message.NewDataBlock` | Builds a user message with text and an image URL. |
| Token estimation | `CountTokens` | Estimates token usage for messages plus tool schemas. |
| Non-streaming call | `ChatModel.Call` | Waits for a complete model response. |
| Tool loop | `weatherTool()`, `kit.RunTool` | Registers `GetWeather`, executes it locally, and sends the result back to the model. |
| Streaming call | `ChatModel.Stream` | Reads response chunks and assembles the final text. |

## Prerequisites

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

This example makes real DashScope requests. Model construction or calls will fail when the key is missing or invalid.

## Run

```bash
cd example/model/dashscope/chat
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run .
```

Important output lines:

- `chat_model=dashscope:qwen3.7-max`: the model adapter was created.
- `tools=1 multimodal_blocks=2 estimated_tokens=...`: tools and multimodal content were included in token estimation.
- `dashscope_live=ok`: non-streaming call completed.
- `dashscope_weather=ok`: tool call and tool-result follow-up completed.
- `dashscope_stream_delta=...`: streaming text delta.

## Code Walkthrough

### Model Construction

The example creates a DashScope ChatModel separately for non-streaming and streaming paths:

```go
chat := mustModel(dashscope.NewChatModel(
    credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).ChatCredential(),
    "qwen3.7-max",
    dashscope.WithStream(false),
    dashscope.WithChatParameters(dashscope.ChatParameters{
        MaxTokens:   func() *int64 { v := int64(1000); return &v }(),
        Temperature: func() *float64 { v := 0.0; return &v }(),
    }),
))
```

`credential.NewDashScope(...).ChatCredential()` produces credentials for the OpenAI-compatible Chat Completions endpoint. `Temperature` is `0.0` to reduce randomness in example output.

### Multimodal Message

The example builds a user message containing text and an image URL:

```go
visionMessage := mustMessage(message.NewUserMessage("user", message.ContentBlockList{
    message.NewTextBlock("Describe this image in one sentence."),
    message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
}))
```

Text, images, audio, and other inputs enter the shared message model as content blocks.

### Token Estimation

The token estimate includes both the message and tool schemas:

```go
tokens, err := chat.CountTokens(asmodel.CallRequest{
    Messages: []*message.Message{visionMessage},
    Tools:    schemas,
})
```

Tool schemas consume context budget, so they should be included in preflight estimates.

### Non-Streaming Call

The normal chat path uses `Call`:

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

The example uses `shorten` to keep terminal output readable.

### Tool-Call Loop

The tool-call flow first asks the model to choose a tool:

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
weatherCall := firstToolCall(toolCallResponse.Content)
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
```

Then it sends assistant and tool messages back for the final answer:

```go
weatherResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage, assistantMessage, toolMessage},
})
```

The model selects the tool and arguments; the Go process executes the tool and maintains state.

### Streaming Call

Streaming uses `Stream`:

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

The loop appends normal deltas to a `strings.Builder` and treats the `IsLast` chunk as the final response.

## Troubleshooting

### Authentication Error

Check `AI_DASHSCOPE_API_KEY` and confirm that the account can access `qwen3.7-max`.

### Estimate Differs From Billing

`CountTokens` is a preflight estimate. Billing should be based on provider usage returned by the API.

### No Tool Call Returned

If the model returns plain text instead of a `ToolCallBlock`, the example panics. Tighten the prompt when deterministic tool usage is required.
