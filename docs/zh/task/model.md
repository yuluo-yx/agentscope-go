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
| `model/openairesponse` | OpenAI Responses API 集成 |
| `model/anthropic` | Anthropic SDK 集成 |
| `model/dashscope` | DashScope OpenAI 兼容端点 |
| `model/deepseek` | OpenAI 兼容包装 |
| `model/gemini` | Gemini 官方 Go SDK 集成 |
| `model/moonshot` | OpenAI 兼容包装 |
| `model/xai` | OpenAI 兼容包装 |
| `model/zhipu` | 智谱 OpenAI 兼容包装 |
| `model/ollama` | Ollama 官方 Go API |

## Embedding

`embedding` 包定义 embedding 请求、响应、缓存辅助能力和供应商模型元数据。Go
实现会为已覆盖的同类 provider 内嵌与 Python AgentScope 能力定义对齐的
embedding model card：

```go
cards, err := dashscopeembedding.ListModels()
```

当前已覆盖 `embedding/dashscope`、`embedding/gemini`、`embedding/openai` 和
`embedding/ollama`。

## 语音能力

语音能力统一放在 `audio/*` 包下。`audio/tts` 定义文本转语音供应商接口，
`audio/stt` 定义语音识别供应商接口。`model/*` 继续负责 chat/generation
多模态模型；其中的音频输入输出能力与独立语音合成、语音识别 provider 分开维护。

DashScope 原生 TTS 适配位于 `audio/tts/dashscope`：

```go
speech, err := dashscopetts.NewModel(
	dashscopetts.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
	"qwen3-tts-flash",
	dashscopetts.WithStream(false),
)
chunks, err := speech.Synthesize(ctx, tts.Request{Text: "hello"})
for chunk := range chunks {
	if chunk.Content == nil {
		continue
	}
	source := chunk.Content.Source.(*message.Base64Source)
	fmt.Println(source.MediaType, len(source.Data))
}
```

DashScope TTS model card 与 Python AgentScope 的同类 provider 能力定义保持对齐，并通过
`dashscopetts.ListModels()` 暴露，包含普通和 realtime 两类模型定义。

`WithStream(true)` 会输出兼容 WAV 的流式分块：首个分块包含 streaming WAV
header 和 PCM 字节，后续分块在同一个 `audio/wav` media type 下追加 PCM
字节。`WithStream(false)` 会把供应商返回的 PCM 分块聚合成一个完整 WAV 载荷。

DashScope 语音识别适配位于 `audio/stt/dashscope`：

```go
speech, err := dashscopestt.NewModel(
	dashscopestt.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
	"paraformer-v2",
)
chunks, err := speech.Recognize(ctx, stt.Request{
	Audio: stt.NewAudioBlock(rawWAV, "audio/wav"),
})
for chunk := range chunks {
	if chunk.Error != nil {
		return chunk.Error
	}
	fmt.Println(chunk.Text)
}
```

DashScope STT model card 通过 `dashscopestt.ListModels()` 暴露。

## 工具 Schema

工具以 OpenAI 兼容的 function schema 传给模型：

```go
schemas, err := kit.ToolSchemas()
response, err := chat.Call(ctx, model.CallRequest{
	Messages: messages,
	Tools:    schemas,
})
```

`Call` 和 `Stream` 都使用同一个 `ChatResponse` 结构。需要读取模型返回的文本块内容时，可以直接调用：

```go
text := response.GetTextContent()
if text != nil {
	fmt.Println(*text)
}

toolCalls := response.GetContentBlocks("tool_call")
if len(toolCalls) > 0 {
	fmt.Println(toolCalls[0].BlockID())
}
```
