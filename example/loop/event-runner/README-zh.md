# 事件驱动 Loop 示例

本示例演示 `loop/automation/event`、`loop/automation/runner` 和 `loop/automation/store` 如何把一个通用事件转换成启用了 `runtime.WithSpec` 的 Agent run。

示例使用 DashScope ChatModel，运行前需要设置 `AI_DASHSCOPE_API_KEY`。

运行：

```bash
go run .
```

示例会构造 `event.Event`，把事件路由到一个 Agent，用 `runner.TemplateMapper` 映射为用户消息，执行 `Agent.ReplyStream`，最后把 run 记录到 `store.MemoryRunStore`。

概念映射：

- `event.Event` 对应 Loop Engineering 里的 automation heartbeat：外部信号负责发现和触发工作。
- `runner.TemplateMapper` 对应“系统替你提示 agent”：事件被稳定转换为 Agent 输入，而不是人工反复手写 prompt。
- `store.MemoryRunStore` 对应跨 run state：记录事件和 run，避免只依赖单次对话上下文。
- 示例保持 report-only，不创建 PR、不修改外部系统，符合生产 loop 默认保守的边界。
