# 智谱 AI ChatModel 示例

本示例展示 `model/zhipu` 的真实 ChatModel 调用。代码覆盖模型初始化、token 估算、非流式回复、本地工具调用闭环和流式输出。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 程序入口 | `main()` | 顺序运行 `chat()` 和 `streamChat()`。 |
| 模型初始化 | `newZhipuModel()` | 使用 `AI_ZHIPU_API_KEY` 创建 `glm-5.1` 模型。 |
| token 估算 | `chat()`、`streamChat()` | 调用 `CountTokens`，展示请求的 token 估算值。 |
| 非流式调用 | `chat()` | 使用 `ChatModel.Call` 获取完整回复。 |
| 工具调用闭环 | `weatherTool()`、`chat()` | 注册 `GetWeather` 工具，执行本地工具，并把结果回填给模型。 |
| 流式调用 | `streamChat()` | 使用 `ChatModel.Stream` 读取增量回复，并处理流中错误。 |

## 运行前提

```bash
export AI_ZHIPU_API_KEY="your-zhipu-key"
```

该示例会发起真实智谱 AI 请求。未设置 API Key 时，模型创建或调用会失败。

## 快速运行

```bash
cd example/model/zhipu/chat
export AI_ZHIPU_API_KEY="your-zhipu-key"
go run .
```

输出中重点观察：

- `chat_model=zhipu:glm-5.1`：模型适配器构造成功。
- `estimated_tokens=...`：请求 token 估算结果。
- `zhipu_live=ok`：非流式回复完成。
- `zhipu_weather=ok`：工具调用和结果回填完成。
- `zhipu_stream_delta=...`：流式增量文本。

## 代码功能解读

### 模型构造

`newZhipuModel()` 集中配置模型：

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

`stream` 参数决定模型默认流式偏好。示例用同一函数分别创建非流式和流式模型，避免两套配置漂移。

### token 估算

示例在真实请求前调用：

```go
tokens, err := chat.CountTokens(liveRequest)
```

上层应用可以把这个结果用于上下文压缩、日志记录和成本预估。

### 非流式调用

`chat()` 发送一个用户消息并等待完整回复：

```go
response, err := chat.Call(ctx, liveRequest)
fmt.Printf("zhipu_live=ok response=%q\n", shorten(textContent(response), 120))
```

这条路径适合需要一次性拿到完整答案的命令行和服务端任务。

### 工具调用闭环

示例通过 toolkit 暴露本地工具：

```go
kit, err := tool.NewToolkit(weatherTool())
schemas, err := kit.ToolSchemas()
```

随后让模型在这些工具中选择：

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

本地执行工具后，把结果封装成 `ToolResultBlock` 回填给模型：

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage := mustMessage(message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
}))
```

这说明工具执行不在模型侧发生，而是在当前 Go 进程内完成。

### 流式调用

`streamChat()` 遍历模型响应 channel：

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

流式响应中间可能出现错误，所以示例在循环内检查 `response.Error`。这比只检查 `Stream` 的返回值更完整。

## 常见问题

### 认证失败

确认 `AI_ZHIPU_API_KEY` 是否设置，并确认 key 对 `glm-5.1` 有调用权限。

### 没有工具调用

如果模型没有返回工具调用，示例会 panic 并打印模型文本。需要更稳定触发工具时，可以把提示词改成“必须调用 `GetWeather` 工具后再回答”。

### 流式输出为空

先确认非流式 `chat()` 是否成功。若非流式成功但流式为空，重点检查流式接口权限、网络代理和 `response.Error`。
