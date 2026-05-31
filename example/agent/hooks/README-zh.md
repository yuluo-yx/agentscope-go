# Agent Hook 示例

英文文档：[README.md](README.md)。

这个示例展示一个 middleware 如何同时实现 Agent 的所有 hook：

- `OnReply` 包裹完整回复生命周期。
- `OnReasoning` 包裹每一轮推理。
- `OnSystemPrompt` 在模型输入构造前修改 system prompt。
- `OnModelCall` 在 `ChatModel.Stream` 执行前修改 `CallRequest`，并观察模型响应流。
- `OnActing` 包裹本地工具执行，并观察流式工具 chunk。

示例使用脚本化 ChatModel 和本地 `TaskCreate` 工具，因此不需要 API Key 或外部服务。

## 前置条件

- Go 1.26.3 或更新版本。
- 不需要 API Key。

## 运行

```bash
cd example/agent/hooks
go run .
```

预期输出包含最终回复、创建的任务数量和 hook trace：

```text
reply=hooks observed tasks=1 trace=reply:before,reasoning:before,system_prompt:Friday,model_call:before,...
```

## 测试

```bash
go test .
```
