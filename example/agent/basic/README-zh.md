# 基础 Agent 示例

英文文档：[README.md](README.md)。

本示例展示一个最小端到端 Agent 流程：

- 使用脚本化 ChatModel，避免依赖外部模型服务。
- Agent 首轮模型输出 `TaskCreate` 工具调用。
- Agent 执行 task 工具并把工具结果放回上下文。
- Agent 第二轮模型输出最终 assistant 回复。

## 前置条件

- Go 1.26.3。
- 不需要 API Key。

## 运行

```bash
cd example/agent/basic
go run .
```

## 预期输出

输出包含：

```text
agent_reply=task tracked
```
