# Gemini ChatModel 示例

本示例展示 `model/gemini` 的真实 ChatModel 调用。代码覆盖 Gemini credential 适配、多模态消息、token 估算、非流式回复、本地工具调用闭环和流式输出。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 程序入口 | `main()` | 顺序运行 `chat()` 和 `streamChat()`。 |
| 模型初始化 | `chat()`、`streamChat()` | 使用 `AI_GEMINI_API_KEY` 创建 `gemini-2.5-flash` 模型。 |
| 多模态消息 | `message.NewDataBlock` | 构造文本和图片 URL 组成的用户消息。 |
| token 估算 | `CountTokens` | 估算消息和工具 schema 的上下文占用。 |
| 非流式调用 | `ChatModel.Call` | 获取完整模型回复。 |
| 工具调用闭环 | `weatherTool()`、`kit.RunTool` | 执行本地 `GetWeather` 工具，并把结果交还给模型。 |
| 流式调用 | `ChatModel.Stream` | 逐块读取模型响应并拼接最终结果。 |

## 运行前提

```bash
export AI_GEMINI_API_KEY="your-gemini-key"
```

该示例会发起真实 Gemini 请求。未设置 API Key 时，模型创建或调用会失败。

## 快速运行

```bash
cd example/model/gemini/chat
export AI_GEMINI_API_KEY="your-gemini-key"
go run .
```

输出中重点观察：

- `chat_model=gemini:gemini-2.5-flash`：模型适配器构造成功。
- `tools=1 multimodal_blocks=2 estimated_tokens=...`：多模态消息和工具 schema 已参与估算。
- `gemini_live=ok`：非流式回复完成。
- `gemini_weather=ok`：工具调用和结果回填完成。
- `gemini_stream_delta=...`：流式增量文本。

## 代码功能解读

### 模型构造

Gemini 示例直接在 `chat()` 和 `streamChat()` 中构造模型：

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

`MaxTokens` 是 `int32`，`Temperature` 是 `float32`，这与 Gemini provider 的参数类型保持一致。

### 多模态消息

示例构造了一个包含文本和图片 URL 的消息：

```go
visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
    message.NewTextBlock("Describe this image in one sentence."),
    message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
})
```

这段代码演示了 AgentScope Go 如何用统一 content block 表达多模态输入。

### token 估算

`CountTokens` 会把消息和工具 schema 一起纳入估算：

```go
tokens, err := chat.CountTokens(asmodel.CallRequest{
    Messages: []*message.Message{visionMessage},
    Tools:    schemas,
})
```

多模态消息和工具 schema 都会增加上下文占用。生产系统应在调用前做估算，在调用后记录 provider 返回的 usage。

### 非流式调用

普通请求使用 `Call`：

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

该路径适合一次性返回完整答案的场景。

### 工具调用闭环

示例通过 `ToolSchemas` 把本地工具暴露给模型：

```go
schemas, err := kit.ToolSchemas()
```

模型返回工具调用后，本地执行工具，再把 `ToolResultBlock` 回填：

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
})
```

### 流式调用

`Stream` 返回响应 channel：

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

示例把普通增量写入 `strings.Builder`，并在最后输出 `gemini_stream=ok`。

## 常见问题

### 认证失败

确认 `AI_GEMINI_API_KEY` 是否设置，并确认 key 对 `gemini-2.5-flash` 有调用权限。

### 图片 URL 无法访问

示例中的 `https://example.com/sample.png` 仅用于展示数据块格式。真实业务应替换为模型服务可访问的图片 URL。

### 没有工具调用

如果模型直接返回文本，示例会 panic。需要稳定工具调用时，可以在提示词中明确要求先调用 `GetWeather`。
