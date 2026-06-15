# 钩子系统

AgentScope Go 通过 `agent.Middleware` 暴露钩子能力。一个中间件可以实现一个或多个钩子接口。

## 钩子类型

| 接口 | 用途 |
| --- | --- |
| `ReplyMiddleware` | 拦截完整回复流程 |
| `ReasoningMiddleware` | 拦截模型推理事件 |
| `ActingMiddleware` | 拦截工具执行 |
| `ModelCallMiddleware` | 拦截原始模型调用 |
| `SystemPromptMiddleware` | 在模型调用前修改系统提示词 |

## 系统提示词钩子

```go
type PromptNote struct{}

func (PromptNote) MiddlewareName() string { return "prompt-note" }

func (PromptNote) OnSystemPrompt(ctx context.Context, accessor agent.AgentAccessor, prompt string) (string, error) {
	return prompt + "\nUse concise answers.", nil
}
```

构造智能体时注册：

```go
agent.WithMiddlewares(PromptNote{})
```

## 执行顺序

中间件按注册顺序执行。中间件可以调用下一个处理器，检查结果，替换结果，或返回错误。

## 可选 tracing

使用 `github.com/yuluo-yx/agentscope-go/middleware` 获取 tracing middleware：

```go
agent.WithMiddlewares(middleware.NewTracingMiddleware(tracer))
```

`TracingMiddleware` 只依赖很小的 `middleware.Tracer` 接口。核心 `agent` 包不导入 OpenTelemetry。需要接入 OpenTelemetry 的应用可以通过 `github.com/yuluo-yx/agentscope-go/middleware/otel` 适配 tracer。

## 可选 TTS

`middleware.NewTTSMiddleware` 可以为 assistant reply 流追加合成音频：

```go
speech, err := dashscopetts.NewModel(
	dashscopetts.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
	"qwen3-tts-flash",
)
agent.WithMiddlewares(middleware.NewTTSMiddleware(speech))
```

该 middleware 会保留原始文本事件。对于批处理模型，它会收集一个文本块，并在文本块结束后追加
`DATA_BLOCK_START`、`DATA_BLOCK_DELTA` 和 `DATA_BLOCK_END`。实时模型会通过
`Push` 接收文本增量，并在文本块结束时用空 `tts.Request` 做收尾读取。
