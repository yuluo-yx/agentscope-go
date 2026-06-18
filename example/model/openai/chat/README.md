# OpenAI ChatModel Example

This example demonstrates live `model/openai` Chat Completions usage. It covers non-streaming chat, tool calls, tool-result follow-up, streaming output, and local proxy configuration through a custom HTTP client.

## Feature Map

| Feature | Code | Description |
| --- | --- | --- |
| Entry point | `main()` | Runs the normal `Call` example first, then the `Stream` example. |
| Model setup | `newOpenAIModel()` | Reads `AI_OPENAI_API_KEY`, creates the `gpt-5.4` ChatModel, and sets generation parameters. |
| Proxy setup | `newOpenAIHTTPClient()` | Reads `AI_OPENAI_PROXY_URL` and injects a proxied, timeout-bound `http.Client` into the OpenAI SDK. |
| Non-streaming call | `chat()` | Uses `ChatModel.Call` to get one complete response. |
| Tool call | `weatherTool()`, `firstToolCall()` | Registers the local `GetWeather` tool and extracts the model's tool-call block. |
| Tool result follow-up | `chat()` | Sends the tool result back as message history and asks the model for the final answer. |
| Streaming call | `streamChat()` | Uses `ChatModel.Stream` to read text deltas and assemble the final response. |

## Prerequisites

Set an OpenAI API key:

```bash
export AI_OPENAI_API_KEY="sk-..."
```

If local access to `api.openai.com` requires a proxy, set `AI_OPENAI_PROXY_URL`:

```bash
export AI_OPENAI_PROXY_URL="http://127.0.0.1:7890"
```

When `AI_OPENAI_PROXY_URL` is not set, the example keeps the Go standard library proxy behavior and still reads `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`. The example HTTP client also has a 90-second total timeout so failed direct connections do not hang for too long.

## Run

```bash
cd example/model/openai/chat
export AI_OPENAI_API_KEY="sk-..."
export AI_OPENAI_PROXY_URL="http://127.0.0.1:7890"
go run .
```

Expected output includes:

- `openai_live=ok`: the non-streaming chat call completed.
- `openai_weather=ok`: the model requested a tool call, the local tool ran, and the model produced a final answer.
- `openai_stream_delta=...`: streaming text deltas.
- `openai_stream=ok`: final streaming response.

## Code Walkthrough

### Entry Flow

`main()` keeps the two request paths separate:

```go
fmt.Println("start chat call: ------------------")
chat()

fmt.Println("\nstart stream chat call: ------------------")
streamChat()
```

`chat()` is the full non-streaming and tool-call walkthrough. `streamChat()` focuses on consuming incremental model output.

### Model Construction

`newOpenAIModel()` centralizes OpenAI model options:

```go
chat, err := openai.NewChatModel(
    credential.NewOpenAI(os.Getenv("AI_OPENAI_API_KEY")).ChatCredential(),
    "gpt-5.4",
    openai.WithHTTPClient(httpClient),
    openai.WithStream(stream),
    openai.WithChatParameters(openai.ChatParameters{
        MaxTokens:   func() *int64 { v := int64(256); return &v }(),
        Temperature: func() *float64 { v := 0.01; return &v }(),
    }),
)
```

The important parts are:

- `credential.NewOpenAI(...).ChatCredential()` adapts the shared credential type to `model/openai`.
- `"gpt-5.4"` is the model used by this example.
- `openai.WithHTTPClient(httpClient)` gives the SDK a custom transport.
- `MaxTokens` and `Temperature` constrain response length and randomness.

### Proxy-Aware HTTP Client

`newOpenAIHTTPClient()` clones the default transport and only overrides `Transport.Proxy` when `AI_OPENAI_PROXY_URL` is set:

```go
transport := http.DefaultTransport.(*http.Transport).Clone()
proxyURL := strings.TrimSpace(os.Getenv("AI_OPENAI_PROXY_URL"))
if proxyURL == "" {
    return &http.Client{Transport: transport, Timeout: openAIRequestTimeout}, ""
}

parsedProxyURL, err := url.Parse(proxyURL)
transport.Proxy = http.ProxyURL(parsedProxyURL)
```

This preserves default connection pooling, TLS behavior, and standard proxy environment variables, while still allowing an explicit local proxy for OpenAI traffic.

### Non-Streaming Call

The normal chat call sends one user message and waits for a complete response:

```go
liveMessage, _ := message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go.")
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

After the call returns, the example extracts text with `response.GetTextContent()` and prints `openai_live=ok`.

### Tool Definition

`weatherTool()` defines a local read-only function tool:

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
    tool.WithFunctionReadOnly(true),
)
```

The toolkit turns this local function into tool schemas that can be sent to the model.

### Tool-Call Loop

The tool loop has three steps:

1. Send tool schemas in `CallRequest.Tools`.
2. Extract the returned `ToolCallBlock` and execute it locally.
3. Send the assistant tool call plus tool result back to the model.

```go
toolCallResponse, _ := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
weatherCall := firstToolCall(toolCallResponse.Content)
toolResponse, _ := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())

assistantMessage, _ := message.NewAssistantMessage("assistant", toolCallResponse.Content)
toolMessage, _ := message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
})
weatherResponse, _ := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage, assistantMessage, toolMessage},
})
```

The model decides which tool to call; the Go process owns execution and sends the result back as conversation history.

### Streaming Call

Streaming uses `Stream` and consumes a channel:

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

The loop prints intermediate chunks and keeps the final chunk when `IsLast` is true:

```go
for response := range responses {
    if response.IsLast {
        finalText = text
        continue
    }
    streamed.WriteString(text)
}
```

Use this pattern for terminals, SSE endpoints, WebSocket endpoints, or any UI that should render model output as it arrives.

## Troubleshooting

### OpenAI Request Times Out

If the error contains `dial tcp ... i/o timeout`, the machine likely cannot reach `api.openai.com` directly. Verify the local proxy first:

```bash
nc -vz 127.0.0.1 7890
```

Then run with the proxy:

```bash
AI_OPENAI_PROXY_URL=http://127.0.0.1:7890 go run .
```

### `401 invalid_api_key`

The request reached OpenAI, but the key is invalid or expired. Replace `AI_OPENAI_API_KEY` and run the example again.

### No Tool Call Returned

The example panics and prints model text when no `ToolCallBlock` is found. Make the prompt stricter if you need deterministic tool usage, for example: “Call `GetWeather` before answering.”
