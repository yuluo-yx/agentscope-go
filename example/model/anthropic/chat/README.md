# Anthropic ChatModel Example

This example demonstrates live `model/anthropic` ChatModel usage. It covers model construction, token estimation, non-streaming responses, local tool-call execution, tool-result follow-up, and streaming output.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Entry point | `main()` | Runs `chat()` and then `streamChat()`. |
| Model setup | `newAnthropicModel()` | Creates `claude-sonnet-4-5` from `AI_ANTHROPIC_API_KEY`. |
| Token estimation | `chat()`, `streamChat()` | Calls `CountTokens` before live requests. |
| Non-streaming call | `chat()` | Uses `ChatModel.Call` to get a complete response. |
| Tool loop | `weatherTool()`, `chat()` | Registers `GetWeather`, executes it locally, and sends the result back to the model. |
| Streaming call | `streamChat()` | Uses `ChatModel.Stream` and checks `response.Error` while reading chunks. |

## Prerequisites

```bash
export AI_ANTHROPIC_API_KEY="your-anthropic-key"
```

This example makes real Anthropic API requests. Model construction or calls will fail when the key is missing or invalid.

## Run

```bash
cd example/model/anthropic/chat
export AI_ANTHROPIC_API_KEY="your-anthropic-key"
go run .
```

Important output lines:

- `chat_model=anthropic:claude-sonnet-4-5`: the model adapter was created.
- `estimated_tokens=...`: request token estimate.
- `anthropic_live=ok`: non-streaming call completed.
- `anthropic_weather=ok`: tool call and tool-result follow-up completed.
- `anthropic_stream_delta=...`: streaming text delta.

## Code Walkthrough

### Model Construction

`newAnthropicModel()` keeps credential, model name, and generation parameters in one place:

```go
chat, err := anthropic.NewChatModel(
    credential.NewAnthropic(os.Getenv("AI_ANTHROPIC_API_KEY")).ChatCredential(),
    "claude-sonnet-4-5",
    anthropic.WithStream(stream),
    anthropic.WithChatParameters(anthropic.ChatParameters{
        MaxTokens:   &maxTokens,
        Temperature: &temperature,
    }),
)
```

The caller controls the `stream` flag. `chat()` passes `false`; `streamChat()` passes `true`.

### Token Estimation

The example calls `CountTokens` before making a live request:

```go
tokens, err := chat.CountTokens(liveRequest)
```

Applications can use this value for logging, context trimming, and budget checks.

### Non-Streaming Call

`Call` waits until the full model response is available:

```go
response, err := chat.Call(ctx, liveRequest)
fmt.Printf("anthropic_live=ok response=%q\n", shorten(textContent(response), 120))
```

Use this path for CLIs, batch jobs, and service handlers that do not need incremental rendering.

### Tool-Call Loop

The local tool is registered through a toolkit:

```go
kit, err := tool.NewToolkit(weatherTool())
schemas, err := kit.ToolSchemas()
```

The model receives those schemas in `CallRequest.Tools`:

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

After extracting the tool call, the Go process executes it and sends a `ToolResultBlock` back:

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage := mustMessage(message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
}))
```

The model decides which tool to call; local code owns execution and state.

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

Always check `response.Error` while reading. Streaming errors may appear after the stream has already been created.

## Troubleshooting

### Authentication Error

Check that `AI_ANTHROPIC_API_KEY` is set and that the key can access the configured model.

### No Tool Call Returned

If the model returns plain text instead of a `ToolCallBlock`, the example panics and prints the model text. Tighten the prompt if deterministic tool usage is required.

### Empty Streaming Output

First confirm that `chat()` succeeds. If non-streaming works but streaming is empty, inspect streaming permissions, proxy settings, and `response.Error` values.
