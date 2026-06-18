# xAI ChatModel 示例

本示例展示 `model/xai` 的真实 ChatModel 调用。代码覆盖 xAI credential、模型参数、多模态消息、token 估算、非流式回复、本地工具调用闭环和流式输出。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 程序入口 | `main()` | 顺序运行 `chat()` 和 `streamChat()`。 |
| 模型初始化 | `chat()`、`streamChat()` | 使用 `AI_XAI_API_KEY` 创建 `grok-3` 模型。 |
| 多模态消息 | `message.NewDataBlock` | 构造文本和图片 URL 组成的用户消息。 |
| token 估算 | `CountTokens` | 估算消息和工具 schema 的上下文占用。 |
| 非流式调用 | `ChatModel.Call` | 获取完整模型回复。 |
| 工具调用闭环 | `weatherTool()`、`kit.RunTool` | 执行本地 `GetWeather` 工具，并把结果回填给模型。 |
| 流式调用 | `ChatModel.Stream` | 逐块读取模型响应并拼接最终结果。 |

## 运行前提

```bash
export AI_XAI_API_KEY="your-xai-key"
```

该示例会发起真实 xAI 请求。未设置 API Key 时，模型创建或调用会失败。

## 快速运行

```bash
cd example/model/xai/chat
export AI_XAI_API_KEY="your-xai-key"
go run .
```

输出中重点观察：

- `chat_model=xai:grok-3`：模型适配器构造成功。
- `tools=1 multimodal_blocks=2 estimated_tokens=...`：多模态消息和工具 schema 已参与估算。
- `xai_live=ok`：非流式回复完成。
- `xai_weather=ok`：工具调用和结果回填完成。
- `xai_stream_delta=...`：流式增量文本。

## 代码功能解读

### 模型构造

xAI 示例直接使用 provider credential：

```go
chat, err := xai.NewChatModel(
    xai.NewCredential(os.Getenv("AI_XAI_API_KEY")),
    "grok-3",
    xai.WithStream(false),
    xai.WithChatParameters(xai.ChatParameters{
        MaxTokens:   &maxTokens,
        Temperature: &temperature,
    }),
)
```

非流式路径使用 `xai.WithStream(false)`，流式路径使用 `xai.WithStream(true)`。两者共享相同模型名和生成参数。

### 多模态消息

示例构造文本和图片 URL：

```go
visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
    message.NewTextBlock("Describe this image in one sentence."),
    message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
})
```

这段代码主要演示 AgentScope Go 的消息块模型；真实业务应替换为可访问的图片 URL。

### token 估算

工具 schema 会与消息一起参与估算：

```go
tokens, err := chat.CountTokens(asmodel.CallRequest{
    Messages: []*message.Message{visionMessage},
    Tools:    schemas,
})
```

估算结果适合用于日志、预算控制和上下文裁剪。

### 非流式和流式调用

非流式调用：

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

流式调用：

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

流式路径会逐块打印 `xai_stream_delta`，适合终端、SSE 或 WebSocket 输出。

### 工具调用闭环

工具调用先把 schema 交给模型：

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

再执行本地工具并回填结果：

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
})
```

## 常见问题

### 认证失败

确认 `AI_XAI_API_KEY` 是否设置，并确认 key 对 `grok-3` 有调用权限。

### 图片 URL 无法访问

示例 URL 仅用于展示消息块结构。真实调用应使用 xAI 服务可访问的图片 URL。

### 没有工具调用

如果模型没有返回工具调用，示例会 panic。需要稳定触发工具时，可以加强提示词约束。
