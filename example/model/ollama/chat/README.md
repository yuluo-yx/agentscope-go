# Ollama ChatModel Example

This example demonstrates local `model/ollama` ChatModel usage. It covers local Ollama connection setup, non-streaming responses, local tool-call execution, tool-result follow-up, and streaming output.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Entry point | `main()` | Runs `chat()` and then `streamChat()`. |
| Model setup | `newOllamaModel()` | Connects to `http://127.0.0.1:11434` and uses `llama3.1`. |
| Non-streaming call | `chat()` | Uses `ChatModel.Call` to get a complete response. |
| Tool schema | `tool.NewToolkit` | Converts the local `GetWeather` tool into model-visible schemas. |
| Tool loop | `chat()` | Executes the returned tool call locally and sends the result back to the model. |
| Streaming call | `streamChat()` | Uses `ChatModel.Stream` to print text deltas. |

## Prerequisites

This example does not need a remote API key, but it does need a local Ollama service:

```bash
ollama serve
ollama pull llama3.1
```

The code connects to:

```text
http://127.0.0.1:11434
```

## Run

```bash
cd example/model/ollama/chat
go run .
```

Important output lines:

- `ollama_live=ok`: non-streaming call completed.
- `ollama_weather=ok`: tool call and tool-result follow-up completed.
- `ollama_stream_delta=...`: streaming text delta.
- `ollama_stream=ok`: final streaming response.

## Code Walkthrough

### Model Construction

`newOllamaModel()` creates the model with a local host and model name:

```go
chat, err := ollama.NewChatModel(
    credential.NewOllama(
        credential.WithHost("http://127.0.0.1:11434"),
    ).ChatCredential(),
    "llama3.1",
    ollama.WithStream(stream),
    ollama.WithChatParameters(ollama.ChatParameters{
        MaxTokens:   func() *int { v := 256; return &v }(),
        Temperature: func() *float64 { v := 0.01; return &v }(),
    }),
)
```

This keeps the call local and avoids remote provider credentials.

### Non-Streaming Call

The normal path uses `Call`:

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

Use this path when a complete response is enough.

### Tool-Call Loop

The example still demonstrates tool schemas, tool calls, and result follow-up:

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
weatherCall := firstToolCall(toolCallResponse.Content)
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
```

The assistant tool call and tool result are then sent back as message history for the final answer.

### Streaming Call

The streaming path reads a channel:

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

The example appends deltas to a `strings.Builder` and prints `ollama_stream=ok` at the end.

## Troubleshooting

### Connection Refused

If the error contains `connection refused`, start the Ollama service:

```bash
ollama serve
```

### Model Not Found

If `llama3.1` is missing, pull it first:

```bash
ollama pull llama3.1
```

### Tool Calls Are Unstable

Local model tool-call quality depends on the model. If the model returns plain text, try a model with stronger tool support or tighten the prompt.
