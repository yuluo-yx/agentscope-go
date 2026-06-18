# DashScope ChatModel 示例

本示例展示 `model/dashscope` 的 OpenAI-compatible ChatModel 调用。代码覆盖多模态消息构造、token 估算、非流式回复、本地工具调用闭环和流式输出。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 程序入口 | `main()` | 顺序运行 `chat()` 和 `streamChat()`。 |
| 模型初始化 | `chat()`、`streamChat()` | 使用 `AI_DASHSCOPE_API_KEY` 创建 `qwen3.7-max` 模型。 |
| 多模态消息 | `message.NewDataBlock` | 构造包含文本和图片 URL 的用户消息，用于演示消息块格式。 |
| token 估算 | `CountTokens` | 估算消息和工具 schema 的 token 占用。 |
| 非流式调用 | `ChatModel.Call` | 等待模型完整回复后输出。 |
| 工具调用闭环 | `weatherTool()`、`kit.RunTool` | 注册 `GetWeather` 工具，执行本地工具，并把结果回填给模型。 |
| 流式调用 | `ChatModel.Stream` | 逐块读取响应内容并拼接最终文本。 |

## 运行前提

```bash
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
```

该示例会发起真实 DashScope 请求。未设置 API Key 时，模型创建或调用会失败。

## 快速运行

```bash
cd example/model/dashscope/chat
export AI_DASHSCOPE_API_KEY="your-dashscope-key"
go run .
```

输出中重点观察：

- `chat_model=dashscope:qwen3.7-max`：模型适配器构造成功。
- `tools=1 multimodal_blocks=2 estimated_tokens=...`：工具和多模态消息参与 token 估算。
- `dashscope_live=ok`：非流式回复完成。
- `dashscope_weather=ok`：工具调用和结果回填完成。
- `dashscope_stream_delta=...`：流式增量文本。

## 代码功能解读

### 模型构造

示例在非流式和流式路径里分别构造 DashScope ChatModel：

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

`credential.NewDashScope(...).ChatCredential()` 会生成 OpenAI-compatible Chat Completions 所需的 credential。`Temperature` 设置为 `0.0`，用于降低示例输出的随机性。

### 多模态消息

示例构造了一个包含文本和图片 URL 的用户消息：

```go
visionMessage := mustMessage(message.NewUserMessage("user", message.ContentBlockList{
    message.NewTextBlock("Describe this image in one sentence."),
    message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
}))
```

这段代码用于展示 `ContentBlockList` 的组织方式。文本、图片、音频等输入都以 content block 形式进入统一消息结构。

### token 估算

示例把多模态消息和工具 schema 一起传给 `CountTokens`：

```go
tokens, err := chat.CountTokens(asmodel.CallRequest{
    Messages: []*message.Message{visionMessage},
    Tools:    schemas,
})
```

这能帮助上层应用在真实调用前预估上下文大小。工具 schema 也会消耗上下文预算，因此应纳入估算。

### 非流式调用

普通对话使用 `Call`：

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

`Call` 返回完整回复。示例用 `shorten` 限制终端输出长度，避免长文本影响阅读。

### 工具调用闭环

工具调用路径分为 3 步：

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
weatherCall := firstToolCall(toolCallResponse.Content)
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
```

随后构造 assistant 消息和 tool 消息，再次调用模型：

```go
weatherResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage, assistantMessage, toolMessage},
})
```

这个闭环说明模型不会直接执行本地函数；工具执行由 Go 进程完成，执行结果再通过消息历史交回模型。

### 流式调用

流式调用使用 `Stream`：

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

示例遍历响应 channel，把普通增量写入 `strings.Builder`，把 `IsLast` 块作为最终结果。

## 常见问题

### 认证失败

确认 `AI_DASHSCOPE_API_KEY` 是否设置，并确认账号有 `qwen3.7-max` 调用权限。

### token 估算和实际计费不完全一致

`CountTokens` 用于调用前估算，实际计费以 provider 返回的 usage 为准。生产系统应把两者都记录下来。

### 模型没有返回工具调用

如果模型直接回答而没有生成 `ToolCallBlock`，示例会 panic。需要稳定触发工具时，可以把提示词改成“必须调用 `GetWeather` 工具后再回答”。
