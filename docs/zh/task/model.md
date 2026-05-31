# 模型集成

`model` 包定义统一模型接口。各模型供应商包负责把 SDK 的消息、工具 Schema、流式事件和 Token 统计适配到该接口。

## ChatModel

```go
type ChatModel interface {
	Name() string
	Call(context.Context, CallRequest) (*ChatResponse, error)
	Stream(context.Context, CallRequest) (<-chan ChatResponse, error)
	CountTokens(CallRequest) (int, error)
}
```

## 供应商

| 包 | 说明 |
| --- | --- |
| `model/openai` | OpenAI SDK 集成 |
| `model/anthropic` | Anthropic SDK 集成 |
| `model/dashscope` | DashScope OpenAI 兼容端点 |
| `model/deepseek` | OpenAI 兼容包装 |
| `model/moonshot` | OpenAI 兼容包装 |
| `model/xai` | OpenAI 兼容包装 |
| `model/ollama` | Ollama 官方 Go API |

## 工具 Schema

工具以 OpenAI 兼容的 function schema 传给模型：

```go
schemas, err := kit.ToolSchemas()
response, err := chat.Call(ctx, model.CallRequest{
	Messages: messages,
	Tools:    schemas,
})
```
