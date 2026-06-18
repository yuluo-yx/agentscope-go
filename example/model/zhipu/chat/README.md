# Zhipu AI ChatModel Example

This example demonstrates live `model/zhipu` ChatModel usage. It covers model construction, token estimation, non-streaming responses, local tool-call execution, tool-result follow-up, and streaming output.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Entry point | `main()` | Runs `chat()` and then `streamChat()`. |
| Model setup | `newZhipuModel()` | Creates `glm-5.1` from `AI_ZHIPU_API_KEY`. |
| Token estimation | `chat()`, `streamChat()` | Calls `CountTokens` before live requests. |
| Non-streaming call | `chat()` | Uses `ChatModel.Call` to get a complete response. |
| Tool loop | `weatherTool()`, `chat()` | Registers `GetWeather`, executes it locally, and sends the result back to the model. |
| Streaming call | `streamChat()` | Uses `ChatModel.Stream` and checks stream errors. |

## Prerequisites

```bash
export AI_ZHIPU_API_KEY="your-zhipu-key"
```

This example makes real Zhipu AI requests. Model construction or calls will fail when the key is missing or invalid.

## Run

```bash
cd example/model/zhipu/chat
export AI_ZHIPU_API_KEY="your-zhipu-key"
go run .
```

Important output lines:

- `chat_model=zhipu:glm-5.1`: the model adapter was created.
- `estimated_tokens=...`: request token estimate.
- `zhipu_live=ok`: non-streaming call completed.
- `zhipu_weather=ok`: tool call and tool-result follow-up completed.
- `zhipu_stream_delta=...`: streaming text delta.

## Code Walkthrough

### Model Construction

`newZhipuModel()` keeps credential, model name, and generation parameters together:

```go
chat, err := zhipu.NewChatModel(
    zhipu.NewCredential(os.Getenv("AI_ZHIPU_API_KEY")),
    "glm-5.1",
    zhipu.WithStream(stream),
    zhipu.WithChatParameters(zhipu.ChatParameters{
        MaxTokens:   &maxTokens,
        Temperature: &temperature,
    }),
)
```

The same helper creates both non-streaming and streaming models so the configuration stays aligned.

### Token Estimation

The example calls `CountTokens` before the live request:

```go
tokens, err := chat.CountTokens(liveRequest)
```

Applications can use this value for context trimming, logs, and budget estimation.

### Non-Streaming Call

`Call` sends a user message and waits for the full response:

```go
response, err := chat.Call(ctx, liveRequest)
fmt.Printf("zhipu_live=ok response=%q\n", shorten(textContent(response), 120))
```

Use this path when the caller does not need incremental rendering.

### Tool-Call Loop

The toolkit exposes a local tool schema:

```go
kit, err := tool.NewToolkit(weatherTool())
schemas, err := kit.ToolSchemas()
```

The model receives the schemas in `CallRequest.Tools`:

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

After local execution, the tool result is sent back:

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage := mustMessage(message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
}))
```

The model chooses a tool and arguments; the Go process performs execution and state handling.

### Streaming Call

`streamChat()` consumes a response channel:

```go
for response := range responses {
    if response.Error != nil {
        panic(response.Error)
    }
    if response.IsLast {
        finalText = textContent(&response)
        continue
    }
}
```

Check `response.Error` inside the loop because stream errors may arrive after stream creation.

## Troubleshooting

### Authentication Error

Check that `AI_ZHIPU_API_KEY` is set and that the key can access `glm-5.1`.

### No Tool Call Returned

If the model returns plain text instead of a tool call, the example panics and prints the model text. Tighten the prompt when deterministic tool usage is required.

### Empty Streaming Output

First confirm that `chat()` succeeds. If non-streaming works but streaming is empty, inspect streaming permissions, proxy settings, and `response.Error`.
