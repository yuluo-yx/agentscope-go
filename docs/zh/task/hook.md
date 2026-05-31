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
