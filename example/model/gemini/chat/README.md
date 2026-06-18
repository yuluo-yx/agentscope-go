# Gemini ChatModel Example

This example demonstrates live `model/gemini` ChatModel usage. It covers Gemini credential adaptation, multimodal messages, token estimation, non-streaming responses, local tool-call execution, tool-result follow-up, and streaming output.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Entry point | `main()` | Runs `chat()` and then `streamChat()`. |
| Model setup | `chat()`, `streamChat()` | Creates `gemini-2.5-flash` from `AI_GEMINI_API_KEY`. |
| Multimodal message | `message.NewDataBlock` | Builds a user message with text and an image URL. |
| Token estimation | `CountTokens` | Estimates context usage for messages plus tool schemas. |
| Non-streaming call | `ChatModel.Call` | Gets a complete model response. |
| Tool loop | `weatherTool()`, `kit.RunTool` | Executes the local `GetWeather` tool and sends the result back to the model. |
| Streaming call | `ChatModel.Stream` | Reads response chunks and assembles the final text. |

## Prerequisites

```bash
export AI_GEMINI_API_KEY="your-gemini-key"
```

This example makes real Gemini requests. Model construction or calls will fail when the key is missing or invalid.

## Run

```bash
cd example/model/gemini/chat
export AI_GEMINI_API_KEY="your-gemini-key"
go run .
```

Important output lines:

- `chat_model=gemini:gemini-2.5-flash`: the model adapter was created.
- `tools=1 multimodal_blocks=2 estimated_tokens=...`: multimodal content and tool schemas were included in token estimation.
- `gemini_live=ok`: non-streaming call completed.
- `gemini_weather=ok`: tool call and tool-result follow-up completed.
- `gemini_stream_delta=...`: streaming text delta.

## Code Walkthrough

### Model Construction

The Gemini model is built in both `chat()` and `streamChat()`:

```go
chat, err := gemini.NewChatModel(
    credential.NewGemini(os.Getenv("AI_GEMINI_API_KEY")).ChatCredential(),
    "gemini-2.5-flash",
    gemini.WithChatParameters(gemini.ChatParameters{
        MaxTokens:   &maxTokens,
        Temperature: &temperature,
    }),
)
```

`MaxTokens` is `int32` and `Temperature` is `float32`, matching the provider parameter types.

### Multimodal Message

The example builds a message with text and an image URL:

```go
visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
    message.NewTextBlock("Describe this image in one sentence."),
    message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
})
```

AgentScope Go represents multimodal input through a shared content-block model.

### Token Estimation

The estimate includes both messages and tool schemas:

```go
tokens, err := chat.CountTokens(asmodel.CallRequest{
    Messages: []*message.Message{visionMessage},
    Tools:    schemas,
})
```

Use this estimate before the call, and record provider usage after the call.

### Non-Streaming Call

The normal path uses `Call`:

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

Use this path when a complete response is enough.

### Tool-Call Loop

Local tools are exposed through schemas:

```go
schemas, err := kit.ToolSchemas()
```

After the model returns a tool call, the Go process executes it and sends a `ToolResultBlock` back:

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
})
```

### Streaming Call

`Stream` returns a response channel:

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

The example appends normal deltas to a `strings.Builder` and prints `gemini_stream=ok` at the end.

## Troubleshooting

### Authentication Error

Check `AI_GEMINI_API_KEY` and confirm that the key can access `gemini-2.5-flash`.

### Image URL Is Not Reachable

`https://example.com/sample.png` only demonstrates the data-block shape. Replace it with an image URL reachable by the model service in real usage.

### No Tool Call Returned

If the model returns plain text, the example panics. Tighten the prompt when deterministic tool usage is required.
