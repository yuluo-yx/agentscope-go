# 函数工具示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示如何把 Go 函数包装为 AgentScope 工具：

- 声明工具名、描述和 JSON Schema。
- 实现同步函数 handler。
- 执行工具并收集 `ToolChunk` 到 `ToolResponse`。
- 让 DashScope ChatModel 请求调用 `Greet` 工具，本地执行工具，把 `ToolResultBlock` 回填给模型，并输出最终模型回复。

## 前置条件

- Go 1.26.3。
- 离线 schema 和 token 估算不需要 API Key。
- 设置 `AI_DASHSCOPE_API_KEY` 后会运行真实 model -> tool call -> tool result 闭环。

## 运行

```bash
cd example/tool/function
go run .
```

## 预期输出

输出包含：

```text
function_tool=Greet
chat_tool=Greet
```
