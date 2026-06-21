# Goal Runner Loop 示例

本示例演示 `loop/automation/goal.GoalRunner` 如何在 verifier 第一次拒绝后，根据 `NextAction` 自动发起第二轮 Agent run，并在第二次 verifier 通过后停止。

示例使用本地脚本模型，不需要配置模型供应商凭据。

运行：

```bash
go run .
```

预期输出会显示：

- `completed=true`。
- `attempts=2`。
- 第一条 run 停止原因为 `verifier_failed`。
- 第二条 run 停止原因为 `completed`。

这个流程对应跨 run 目标推进：初始输入触发第一轮，verifier 返回失败与 `NextAction`，`TemplateNextActionMapper` 把反馈转换为下一轮用户消息，第二轮补齐证据后通过验证。

概念映射：

- `goal.GoalRunner` 对应 blog 里的 `/goal`：循环持续推进，直到一个可验证的停止条件成立。
- 脚本模型代表 maker，`core.Verifier` 代表 checker：写结果的 Agent 不负责给自己的完成状态打分。
- `goal.TemplateNextActionMapper` 把 checker 反馈变成下一轮输入，体现“系统替你提示 agent”。
- `store.MemoryRunStore` 记录每轮 run 和 report，模拟长期 loop 需要的外部 state。
