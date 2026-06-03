# 任务工具示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示 task 工具对 `AgentState.TaskContext` 的读写：

- `TaskCreate`：创建任务。
- `TaskUpdate`：更新任务状态、负责人和元数据。
- `TaskList`：列出任务摘要。
- `TaskGet`：读取单个任务详情。
- 让 DashScope ChatModel 请求调用 `TaskGet`，基于 `AgentState.TaskContext` 本地执行工具，把 `ToolResultBlock` 回填给模型，并输出最终模型回复。

## 前置条件

- Go 1.26.3。
- 离线 schema 和 token 估算不需要 API Key。
- 设置 `AI_DASHSCOPE_API_KEY` 后会运行真实 model -> tool call -> tool result 闭环。

## 运行

```bash
cd example/tool/task
go run .
```

## 预期输出

输出包含：

```text
task_tools=TaskCreate,TaskGet,TaskList,TaskUpdate
chat_tool=TaskGet
```
