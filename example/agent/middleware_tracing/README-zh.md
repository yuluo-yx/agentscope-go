# Agent Middleware Tracing 示例

本示例演示如何使用带内存 tracer 的 `middleware.NewTracingMiddleware`。

它会使用 DashScope ChatModel 运行一个完整 ReAct 闭环：

1. 模型请求调用 `Echo` 工具；
2. Agent 执行本地 function tool；
3. 模型收到工具结果并返回最终回复；
4. tracing middleware 记录 reply、model call 和 tool execution span。

运行前需要设置 `AI_DASHSCOPE_API_KEY`。

## 运行

```bash
go run .
```

预期输出：

```text
reply="..." spans=invoke_agent Friday,chat dashscope/qwen3.7-max,execute_tool Echo,... tool_result="echo Ada"
```

本示例不需要 OpenTelemetry。应用需要导出 span 时，可以把 OpenTelemetry tracer 适配到 `middleware.Tracer` 接口。
