# Ollama ChatModel 示例

本示例展示 `model/ollama` 的本地 ChatModel 调用。代码覆盖本地 Ollama 服务连接、非流式回复、本地工具调用闭环和流式输出。

## 功能点总览

| 功能点 | 代码位置 | 说明 |
| --- | --- | --- |
| 程序入口 | `main()` | 顺序运行 `chat()` 和 `streamChat()`。 |
| 模型初始化 | `newOllamaModel()` | 连接 `http://127.0.0.1:11434`，使用 `llama3.1` 模型。 |
| 非流式调用 | `chat()` | 使用 `ChatModel.Call` 获取完整回复。 |
| 工具 schema | `tool.NewToolkit` | 把本地 `GetWeather` 工具转换成模型可见 schema。 |
| 工具调用闭环 | `chat()` | 执行模型返回的工具调用，并把结果回填给模型。 |
| 流式调用 | `streamChat()` | 使用 `ChatModel.Stream` 输出增量文本。 |

## 运行前提

本示例不需要远端 API Key，但需要本地 Ollama 服务：

```bash
ollama serve
ollama pull llama3.1
```

代码固定连接：

```text
http://127.0.0.1:11434
```

## 快速运行

```bash
cd example/model/ollama/chat
go run .
```

输出中重点观察：

- `ollama_live=ok`：非流式回复完成。
- `ollama_weather=ok`：工具调用和结果回填完成。
- `ollama_stream_delta=...`：流式增量文本。
- `ollama_stream=ok`：流式最终文本。

## 代码功能解读

### 模型构造

`newOllamaModel()` 使用本地 host 和模型名创建 ChatModel：

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

这段配置把模型调用限定在本机 Ollama 服务，不依赖云端 provider。

### 非流式调用

普通对话使用 `Call`：

```go
response, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
})
```

`Call` 适合一次性拿到完整回复的命令行或服务端任务。

### 工具调用闭环

示例同样演示工具 schema、工具调用和结果回填：

```go
toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
    Messages: []*message.Message{weatherMessage},
    Tools:    schemas,
})
weatherCall := firstToolCall(toolCallResponse.Content)
toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
```

随后把 assistant tool call 和 tool result 放回消息历史，再让模型生成最终答案。

### 流式调用

流式路径读取 channel：

```go
responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
    Messages: []*message.Message{liveMessage},
    Stream:   true,
})
```

示例把增量文本写入 `strings.Builder`，最后输出 `ollama_stream=ok`。

## 常见问题

### 连接被拒绝

如果报错包含 `connection refused`，确认 Ollama 服务是否已启动：

```bash
ollama serve
```

### 模型不存在

如果报错提示找不到 `llama3.1`，先拉取模型：

```bash
ollama pull llama3.1
```

### 工具调用不稳定

本地模型的工具调用能力与模型版本有关。如果模型直接返回文本，可以更换支持工具调用更好的模型，或强化提示词。
