# 基础 Agent 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示一个最小端到端 Agent 流程：

- 使用 `AI_DASHSCOPE_API_KEY` 创建 DashScope ChatModel。
- 模型输出 `TaskCreate` 工具调用。
- Agent 执行 task 工具并把工具结果放回上下文。
- Agent 第二轮模型输出最终 assistant 回复。
- 示例通过 `Agent.ReplyStream` 消费事件流，展示 Agent 事件流式用法。

## 前置条件

- Go 1.26.3。
- DashScope ChatModel 需要 `AI_DASHSCOPE_API_KEY`。

## 运行

```bash
cd example/agent/basic
go run .
```

## 预期输出

输出包含：

```text
agent_stream=...
tasks=1
events=tool_call:TaskCreate,tool_result:success
```
