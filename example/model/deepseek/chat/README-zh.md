# DeepSeek ChatModel 示例

本示例展示 `model/deepseek` 的真实 ChatModel 调用。代码覆盖 DeepSeek credential、非流式回复、本地工具调用闭环和流式输出。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 程序入口 | `main()` | 顺序运行 `chat()` 和 `streamChat()`。 |
| 模型初始化 | `newDSModel()` | 使用 `AI_DEEPSEEK_API_KEY` 创建 `deepseek-v4-pro` 模型。 |
| 非流式调用 | `chat()` | 使用 `ChatModel.Call` 获取完整回复。 |
| 工具 schema | `tool.NewToolkit` | 把本地 `GetWeather` 工具转换成模型可见 schema。 |
| 工具调用闭环 | `chat()` | 执行模型返回的工具调用，并把结果回填给模型。 |
| 流式调用 | `streamChat()` | 使用 `ChatModel.Stream` 输出增量文本。 |

## 运行前提

```bash
export AI_DEEPSEEK_API_KEY="your-deepseek-key"
```

该示例会发起真实 DeepSeek 请求。未设置 API Key 时，模型创建或调用会失败。

## 快速运行

```bash
cd example/model/deepseek/chat
export AI_DEEPSEEK_API_KEY="your-deepseek-key"
go run .
```

输出中重点观察：

- `ds_live=ok`：非流式回复完成。
- `ds_weather=ok`：工具调用和结果回填完成。
- `ds_stream_delta=...`：流式增量文本。
- `ds_stream=ok`：流式最终文本。

## 代码功能解读

### 模型构造

`newDSModel()` 统一创建 DeepSeek 模型：

```go
chat, err := deepseek.NewChatModel(
    deepseek.NewCredential(os.Getenv("AI_DEEPSEEK_API_KEY")),
    "deepseek-v4-pro",
    deepseek.WithStream(stream),
    deepseek.WithChatParameters(deepseek.ChatParameters{
        MaxTokens:   func() *int64 { v := int64(256); return &v }(),
        Temperature: func() *float64 { v := 0.01; return &v }(),
    }),
)
```

`stream` 参数让同一个函数同时服务非流式和流式路径。`Temperature` 设置较低，便于示例输出稳定。

### 非流式调用

普通对话使用 `Call`：

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

`Call` 会等待完整回复。示例随后提取文本并打印 `ds_live=ok`。

### 工具定义

`weatherTool()` 定义本地函数工具：

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
)
```

工具 schema 交给模型，工具函数仍在本地执行。

### 工具调用闭环

示例先让模型选择工具：

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
```

再执行工具并回填结果：

```go
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
    message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
})
```

这条链路展示了模型决策和本地执行的边界。

### 流式调用

流式路径读取 channel：

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

示例把普通增量累加到 `strings.Builder`，最后输出 `ds_stream=ok`。

## 常见问题

### 认证失败

确认 `AI_DEEPSEEK_API_KEY` 是否设置，并确认 key 对 `deepseek-v4-pro` 有调用权限。

### 没有工具调用

如果模型直接返回文本，示例会 panic。需要稳定触发工具时，可以把提示词改成“必须调用 `GetWeather` 工具后再回答”。

### 流式输出为空

先确认非流式调用是否成功。若非流式成功但流式为空，检查网络、模型流式能力和 provider 返回错误。
