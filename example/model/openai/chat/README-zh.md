# OpenAI ChatModel 示例

本示例展示 `model/openai` 的真实 Chat Completions 调用。代码覆盖非流式对话、工具调用、工具结果回填、流式输出，以及通过自定义 HTTP client 配置本地代理的路径。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 程序入口 | `main()` | 先执行普通 `Call` 示例，再执行 `Stream` 示例。 |
| 模型初始化 | `newOpenAIModel()` | 读取 `AI_OPENAI_API_KEY`，创建 `gpt-5.4` ChatModel，并设置生成参数。 |
| 代理配置 | `newOpenAIHTTPClient()` | 读取 `AI_OPENAI_PROXY_URL`，为 OpenAI SDK 注入带代理和超时的 `http.Client`。 |
| 非流式调用 | `chat()` | 使用 `ChatModel.Call` 获取一次完整回复。 |
| 工具调用 | `weatherTool()`、`firstToolCall()` | 注册本地 `GetWeather` 工具，提取模型生成的工具调用块。 |
| 工具结果回填 | `chat()` | 把 `ToolResultBlock` 作为消息历史回填给模型，生成最终答案。 |
| 流式调用 | `streamChat()` | 使用 `ChatModel.Stream` 逐块读取增量文本，并拼接最终回复。 |

## 运行前提

需要准备 OpenAI API Key：

```bash
export AI_OPENAI_API_KEY="sk-..."
```

如果本地访问 `api.openai.com` 需要代理，设置 `AI_OPENAI_PROXY_URL`：

```bash
export AI_OPENAI_PROXY_URL="http://127.0.0.1:7890"
```

未设置 `AI_OPENAI_PROXY_URL` 时，示例仍会沿用 Go 标准库默认代理行为，自动读取 `HTTP_PROXY`、`HTTPS_PROXY` 和 `NO_PROXY` 等环境变量。示例 HTTP client 设置了 90 秒总超时，用于避免直连不可达时长时间挂起。

## 快速运行

```bash
cd example/model/openai/chat
export AI_OPENAI_API_KEY="sk-..."
export AI_OPENAI_PROXY_URL="http://127.0.0.1:7890"
go run .
```

运行成功后会依次看到：

- `openai_live=ok`：普通非流式回复成功。
- `openai_weather=ok`：模型触发工具调用，本地工具执行完成，并生成最终回复。
- `openai_stream_delta=...`：流式响应的增量片段。
- `openai_stream=ok`：流式最终结果。

## 代码功能解读

### 入口流程

`main()` 只负责串联两个演示场景：

```go
fmt.Println("start chat call: ------------------")
chat()

fmt.Println("\nstart stream chat call: ------------------")
streamChat()
```

这种写法把同步调用和流式调用拆开，方便单独阅读两条请求路径。`chat()` 更适合观察完整工具调用闭环，`streamChat()` 更适合观察增量响应如何被消费。

### 创建 OpenAI 模型

`newOpenAIModel()` 负责集中配置模型：

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

这里有 4 个关键点：

- `credential.NewOpenAI(...).ChatCredential()` 把通用 credential 适配成 `model/openai` 可用的 credential。
- `"gpt-5.4"` 是本示例使用的模型名。
- `openai.WithHTTPClient(httpClient)` 把自定义 HTTP client 传给 OpenAI SDK。
- `MaxTokens` 和 `Temperature` 控制输出长度和生成随机性。

### 配置本地代理

`newOpenAIHTTPClient()` 会克隆默认 transport，并在显式配置代理时覆盖 `Transport.Proxy`：

```go
transport := http.DefaultTransport.(*http.Transport).Clone()
proxyURL := strings.TrimSpace(os.Getenv("AI_OPENAI_PROXY_URL"))
if proxyURL == "" {
    return &http.Client{Transport: transport, Timeout: openAIRequestTimeout}, ""
}

parsedProxyURL, err := url.Parse(proxyURL)
transport.Proxy = http.ProxyURL(parsedProxyURL)
```

这样做有两个好处：

- 保留 Go 默认 transport 的连接池、TLS 和标准代理行为。
- 在需要本地代理时，用 `AI_OPENAI_PROXY_URL` 明确覆盖请求出口。

如果代理地址缺少 `http://`、`socks5://` 这类 scheme，示例会直接报错，避免把错误延迟到网络请求阶段。

### 非流式调用

普通对话使用 `ChatModel.Call`：

```go
liveMessage, _ := message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go.")
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

`Call` 会等待模型完整返回后再继续执行。示例随后通过 `response.GetTextContent()` 提取文本内容，并打印 `openai_live=ok`。

### 工具定义与 schema 提取

`weatherTool()` 创建一个只读函数工具：

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

这段代码同时定义了工具名称、自然语言描述、JSON Schema 参数和本地执行函数。`tool.NewToolkit(weatherTool())` 会把工具包装成 toolkit，`kit.ToolSchemas()` 会生成可发送给模型的工具 schema。

### 工具调用闭环

工具调用分 3 步：

1. 把工具 schema 放进 `CallRequest.Tools`，让模型决定是否调用工具。
2. 从模型回复中提取 `ToolCallBlock`，本地执行 `kit.RunTool`。
3. 构造 assistant 消息和 tool 消息，再次调用模型生成最终答案。

核心代码如下：

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

这个流程体现了 AgentScope Go 的核心约定：模型只负责提出工具调用，本地代码负责执行工具，并把执行结果作为消息历史交还给模型。

### 流式调用

流式调用使用 `Stream`：

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

返回值是只读 channel。示例遍历 channel，遇到普通增量就立即打印，遇到 `IsLast` 的最终块时记录最终文本：

```go
for response := range responses {
    if response.IsLast {
        finalText = text
        continue
    }
    streamed.WriteString(text)
}
```

这种模式适合终端输出、SSE、WebSocket 或任何需要边生成边展示的场景。

## 常见问题

### 访问 OpenAI 超时

如果报错包含 `dial tcp ... i/o timeout`，通常是本机网络无法直连 `api.openai.com`。可以先确认代理端口：

```bash
nc -vz 127.0.0.1 7890
```

再用代理运行：

```bash
AI_OPENAI_PROXY_URL=http://127.0.0.1:7890 go run .
```

### 返回 `401 invalid_api_key`

这说明请求已经到达 OpenAI，但 API Key 不正确或已失效。重新设置 `AI_OPENAI_API_KEY` 后再运行。

### 模型没有返回工具调用

示例会在没有 `ToolCallBlock` 时 panic，并打印模型文本。通常原因是模型没有选择工具，或提示词没有明确要求使用工具。可以加强提示词，例如要求“必须调用 `GetWeather` 工具后再回答”。
