# Anthropic ChatModel 示例

本示例展示 `model/anthropic` 的真实 ChatModel 调用。代码覆盖模型初始化、token 估算、非流式回复、本地工具调用闭环和流式输出。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 程序入口 | `main()` | 顺序运行 `chat()` 和 `streamChat()`。 |
| 模型初始化 | `newAnthropicModel()` | 使用 `AI_ANTHROPIC_API_KEY` 创建 `claude-sonnet-4-5` 模型。 |
| token 估算 | `chat()`、`streamChat()` | 调用 `CountTokens`，展示请求进入模型前的 token 预算。 |
| 非流式调用 | `chat()` | 使用 `ChatModel.Call` 获取完整回复。 |
| 工具调用闭环 | `weatherTool()`、`chat()` | 注册 `GetWeather` 工具，执行本地工具，并把结果回填给模型。 |
| 流式调用 | `streamChat()` | 使用 `ChatModel.Stream` 读取增量回复，并处理 `response.Error`。 |

## 运行前提

```bash
export AI_ANTHROPIC_API_KEY="your-anthropic-key"
```

该示例会发起真实 Anthropic API 请求。未设置 API Key 时，模型创建或调用会失败。

## 快速运行

```bash
cd example/model/anthropic/chat
export AI_ANTHROPIC_API_KEY="your-anthropic-key"
go run .
```

输出中重点观察：

- `chat_model=anthropic:claude-sonnet-4-5`：模型适配器构造成功。
- `estimated_tokens=...`：当前消息的 token 估算结果。
- `anthropic_live=ok`：非流式回复完成。
- `anthropic_weather=ok`：工具调用和结果回填完成。
- `anthropic_stream_delta=...`：流式增量文本。

## 代码功能解读

### 模型构造

`newAnthropicModel()` 把 credential、模型名和生成参数集中在一个函数里：

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

`stream` 参数由调用方传入。`chat()` 传 `false`，用于普通请求；`streamChat()` 传 `true`，用于流式请求。这样可以复用同一套模型配置。

### token 估算

示例在发起真实请求前调用 `CountTokens`：

```go
tokens, err := chat.CountTokens(liveRequest)
```

这个结果可用于观察消息大小，也能帮助上层应用在请求前做上下文裁剪、预算控制或日志记录。

### 非流式调用

普通对话使用 `Call`：

```go
response, err := chat.Call(ctx, liveRequest)
fmt.Printf("anthropic_live=ok response=%q\n", shorten(textContent(response), 120))
```

`Call` 适合命令行工具、批处理任务和不需要边生成边展示的服务端逻辑。

### 工具调用闭环

示例把 `GetWeather` 注册进 toolkit：

```go
kit, err := tool.NewToolkit(weatherTool())
schemas, err := kit.ToolSchemas()
```

随后把 `schemas` 放入模型请求：

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

模型返回工具调用后，本地代码执行工具，并构造 `ToolResultBlock` 回填：

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage := mustMessage(message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
}))
```

这条链路说明：模型负责选择工具和参数，本地应用负责执行工具和维护状态。

### 流式调用

`streamChat()` 遍历响应 channel：

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

流式路径要额外检查 `response.Error`。因为错误可能在流已经建立后才出现，不能只检查 `Stream` 返回时的 `err`。

## 常见问题

### 返回认证错误

确认 `AI_ANTHROPIC_API_KEY` 是否设置，并确认 key 对当前模型有调用权限。

### 没有工具调用

如果模型直接返回文本而不是工具调用，示例会 panic 并打印模型文本。需要更稳定的工具触发时，可以把提示词改成“必须调用 `GetWeather` 工具后再回答”。

### 流式输出为空

先检查非流式 `chat()` 是否成功。如果非流式成功而流式为空，重点排查 provider 的流式接口权限、网络代理和响应中是否携带 `response.Error`。
