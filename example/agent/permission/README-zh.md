# Agent 权限确认示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

这个示例展示 Agent 的权限暂停与恢复流程：

- 模型请求调用一个类似写操作的工具。
- 工具返回默认 ask 决策，并提供建议规则。
- Agent 发出 `RequireUserConfirmEvent` 并结束当前回复。
- 宿主应用发送 `UserConfirmResultEvent`。
- Agent 恢复执行工具，并再次请求模型生成最终回复。

示例使用 DashScope ChatModel 和本地函数工具。模型请求类似写操作的工具，宿主应用控制确认结果。

## 前置条件

- Go 1.26.3 或更新版本。
- DashScope ChatModel 需要 `AI_DASHSCOPE_API_KEY`。

## 运行

```bash
cd example/agent/permission
go run .
```

预期输出：

```text
confirmation=required tool=WriteThing suggestions=1
confirmed_reply=... executed=true
```

## 测试

```bash
go test .
```
