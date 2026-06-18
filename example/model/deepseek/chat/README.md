# DeepSeek ChatModel Example

This example demonstrates live `model/deepseek` ChatModel usage. It covers DeepSeek credentials, non-streaming responses, local tool-call execution, tool-result follow-up, and streaming output.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Entry point | `main()` | Runs `chat()` and then `streamChat()`. |
| Model setup | `newDSModel()` | Creates `deepseek-v4-pro` from `AI_DEEPSEEK_API_KEY`. |
| Non-streaming call | `chat()` | Uses `ChatModel.Call` to get a complete response. |
| Tool schema | `tool.NewToolkit` | Converts the local `GetWeather` tool into model-visible schemas. |
| Tool loop | `chat()` | Executes the returned tool call locally and sends the result back to the model. |
| Streaming call | `streamChat()` | Uses `ChatModel.Stream` to print text deltas. |

## Prerequisites

```bash
export AI_DEEPSEEK_API_KEY="your-deepseek-key"
```

This example makes real DeepSeek requests. Model construction or calls will fail when the key is missing or invalid.

## Run

```bash
cd example/model/deepseek/chat
export AI_DEEPSEEK_API_KEY="your-deepseek-key"
go run .
```

Important output lines:

- `ds_live=ok`: non-streaming call completed.
- `ds_weather=ok`: tool call and tool-result follow-up completed.
- `ds_stream_delta=...`: streaming text delta.
- `ds_stream=ok`: final streaming response.

## Code Walkthrough

### Model Construction

`newDSModel()` creates the DeepSeek model:

```go
chat, err := deepseek.NewChatModel(
    deepseek.NewCredential(os.Getenv("AI_DEEPSEEK_API_KEY")),
    "deepseek-v4-pro",
    deepseek.WithStream(stream),
    deepseek.WithChatParameters(deepseek.ChatParameters{
        MaxTokens:   func() *int64 { v := int64(256); return &v }(),
        Temperature: func() *float64 { v := 0.01; return &v }(),
    }),
)
```

The `stream` argument lets the same helper serve both request paths. The low temperature keeps example output relatively stable.

### Non-Streaming Call

The normal path uses `Call`:

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

`Call` waits for the complete response before returning.

### Tool Definition

`weatherTool()` defines a local function tool:

```go
tool.NewFunctionTool(
    "GetWeather",
    "Return weather for one city.",
    map[string]any{
        "type": "object",
        "properties": map[string]any{
            "city": map[string]any{"type": "string", "description": "City name."},
        },
        "required": []any{"city"},
    },
    func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
        return message.ContentBlockList{message.NewTextBlock("sunny")}, nil
    },
)
```

The schema is visible to the model; the function still executes in the Go process.

### Tool-Call Loop

The model receives tool schemas:

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

The Go process executes the tool and sends the result back:

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
})
```

This shows the boundary between model planning and local execution.

### Streaming Call

The streaming path reads a channel:

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

The example appends normal deltas to a `strings.Builder` and prints `ds_stream=ok` at the end.

## Troubleshooting

### Authentication Error

Check `AI_DEEPSEEK_API_KEY` and confirm that the key can access `deepseek-v4-pro`.

### No Tool Call Returned

If the model returns plain text, the example panics. Tighten the prompt when deterministic tool usage is required.

### Empty Streaming Output

First confirm that non-streaming works. If streaming is still empty, inspect network access, streaming model support, and provider errors.
