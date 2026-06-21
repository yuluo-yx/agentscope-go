# Agent 外部执行示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

这个示例展示 Agent 遇到必须在当前 Go 进程外执行的工具时如何暂停：

- 模型请求调用 `DeployJob`。
- 工具被注册为 external tool。
- Agent 发出 `RequireExternalExecutionEvent`，而不是直接调用 `Execute`。
- 宿主应用完成外部工作后发送 `ExternalExecutionResultEvent`。
- Agent 恢复并请求模型生成最终回复。

示例使用 DashScope ChatModel 和本地 external tool 定义。宿主应用仍负责提供外部执行结果；模型负责请求工具并生成最终回复。

## 前置条件

- Go 1.26.3 或更新版本。
- DashScope ChatModel 需要 `AI_DASHSCOPE_API_KEY`。

## 运行

```bash
cd example/agent/external
go run .
```

预期输出：

```text
external=required tool=DeployJob calls=1
external_reply=... result_state=success
```

## 测试

```bash
go test .
```
