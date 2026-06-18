# Moonshot ChatModel 示例

本示例展示 `model/moonshot` 的真实 ChatModel 调用。代码覆盖 Moonshot credential 适配、多模态消息、token 估算、非流式回复、本地工具调用闭环和流式输出。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 程序入口 | `main()` | 顺序运行 `chat()` 和 `streamChat()`。 |
| 模型初始化 | `chat()`、`streamChat()` | 使用 `AI_MOONSHOT_API_KEY` 创建 `moonshot-v1-8k` 模型。 |
| 多模态消息 | `message.NewDataBlock` | 构造文本和图片 URL 组成的用户消息。 |
| token 估算 | `CountTokens` | 估算消息和工具 schema 的上下文占用。 |
| 非流式调用 | `ChatModel.Call` | 获取完整模型回复。 |
| 工具调用闭环 | `weatherTool()`、`kit.RunTool` | 执行本地 `GetWeather` 工具，并把结果回填给模型。 |
| 流式调用 | `ChatModel.Stream` | 逐块读取模型响应并拼接最终结果。 |

## 运行前提

```bash
export AI_MOONSHOT_API_KEY="your-moonshot-key"
```

该示例会发起真实 Moonshot 请求。未设置 API Key 时，模型创建或调用会失败。

## 快速运行

```bash
cd example/model/moonshot/chat
export AI_MOONSHOT_API_KEY="your-moonshot-key"
go run .
```

输出中重点观察：

- `chat_model=moonshot:moonshot-v1-8k`：模型适配器构造成功。
- `tools=1 multimodal_blocks=2 estimated_tokens=...`：多模态消息和工具 schema 已参与估算。
- `moonshot_live=ok`：非流式回复完成。
- `moonshot_weather=ok`：工具调用和结果回填完成。
- `moonshot_stream_delta=...`：流式增量文本。

## 代码功能解读

### 模型构造

Moonshot 示例在两个路径中分别构造模型：

```go
chat, err := moonshot.NewChatModel(
    credential.NewMoonshot(os.Getenv("AI_MOONSHOT_API_KEY")).ChatCredential(),
    "moonshot-v1-8k",
    moonshot.WithStream(false),
    moonshot.WithChatParameters(moonshot.ChatParameters{
        MaxTokens:   &maxTokens,
        Temperature: &temperature,
    }),
)
```

`moonshot.WithStream(false)` 用于非流式路径；流式路径会改成 `moonshot.WithStream(true)`。

### 多模态消息

示例用统一 content block 表达文本和图片：

```go
visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
    message.NewTextBlock("Describe this image in one sentence."),
    message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
})
```

这段代码说明消息结构不只承载纯文本，也能承载 URL 数据源。

### token 估算

token 估算包含消息和工具：

```go
tokens, err := chat.CountTokens(asmodel.CallRequest{
    Messages: []*message.Message{visionMessage},
    Tools:    schemas,
})
```

如果工具 schema 很大，它同样会占用上下文。调用前估算可以帮助提前发现超长请求。

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

两者使用同一种 `CallRequest` 结构，差异在于返回完整结果还是增量 channel。

### 工具调用闭环

工具闭环先让模型选择工具：

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

再由本地执行工具并回填结果：

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
})
```

## 常见问题

### 认证失败

确认 `AI_MOONSHOT_API_KEY` 是否设置，并确认 key 对 `moonshot-v1-8k` 有调用权限。

### 图片 URL 无法访问

示例中的图片 URL 仅用于展示消息块结构。真实业务应替换为 Moonshot 服务可访问的资源。

### 没有工具调用

如果模型没有返回工具调用，示例会 panic。需要稳定触发工具时，可以加强提示词约束。
