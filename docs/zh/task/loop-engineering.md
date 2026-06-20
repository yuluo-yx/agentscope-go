# Loop Engineering

`loop` 包提供构建 Loop Engineering 智能体的框架 API。它不会替代 `agent` 包的 ReAct 执行循环，而是在 Agent 外层增加目标、成功标准、预算、验证、状态和事件。

Loop Engineering 的核心思想是：应用不再只把一段提示词交给 Agent，而是把“目标、上下文、行动、观测、调整和停止条件”设计成可复用的循环。AgentScope Go 提供这些循环的 building blocks，调度器、工单系统、PR 策略和自动合并策略仍由应用侧决定。

## 适用场景

`loop` 包适合以下场景：

- 需要把 Agent 运行过程按固定目标和成功标准约束起来。
- 需要记录每轮模型调用、工具调用、token 使用和停止原因。
- 需要把执行 Agent 和验证 Agent 或验证逻辑分开。
- 需要把 loop 生命周期事件推送到前端、日志、审计或运行记录。
- 需要先以 report-only 模式运行，再逐步升级到 assisted 或 unattended。

如果只需要一次普通多轮工具调用，直接使用 `agent.Agent` 和 `agent.WithReActConfig` 即可。

## 核心概念

### Spec

`loop.Spec` 是循环设计的公开契约。它描述名称、目标、非目标、成功标准、范围、模式、预算和人工交接规则。

```go
spec := loop.Spec{
	Name: "daily-triage",
	Goal: "扫描仓库信号并输出报告，不自动修改代码。",
	NonGoals: []string{
		"不创建 PR",
		"不自动合并代码",
	},
	SuccessCriteria: []loop.SuccessCriterion{
		{Name: "report", Description: "输出发现项和下一步动作。", Required: true},
	},
	Mode:   loop.ModeReportOnly,
	Policy: loop.DefaultPolicy(loop.ModeReportOnly),
}
```

### Mode

`loop.Mode` 表示 loop 的自治等级：

| 模式 | 含义 |
| --- | --- |
| `loop.ModeReportOnly` | 只报告和记录状态，不自动行动。 |
| `loop.ModeAssisted` | 允许有限行动，建议配置 verifier 或人工确认。 |
| `loop.ModeUnattended` | 允许无人值守运行，必须配置 verifier、预算和人工交接规则。 |

### Policy

`loop.Policy` 限制一次运行的迭代、模型调用、工具调用、token 和尝试次数。预算触顶后，loop middleware 会在下一次 reasoning 前注入 wrap-up hint，并在模型请求中设置 `tool_choice=none`。

### Verifier

`loop.Verifier` 用于 maker/checker 分离。执行 Agent 不应该单独判断自己的工作是否完成。Verifier 可以封装测试命令、规则检查、另一个 Agent、CI 结果、MCP 工具或业务系统状态。

```go
verifier := loop.VerifierFunc(func(ctx context.Context, input loop.VerificationInput) (loop.VerificationResult, error) {
	return loop.VerificationResult{
		Passed:   true,
		Reason:   "local checks passed",
		Evidence: []string{"go test ./..."},
	}, nil
})
```

## 接入 Agent

使用 `loop.WithSpec` 把 loop middleware 注册到 Agent：

```go
agent, err := agent.NewAgent(
	"Friday",
	"You are concise.",
	chatModel,
	loop.WithSpec(spec, loop.WithVerifier(verifier)),
)
```

`loop.WithSpec` 会安装以下 Hook：

- `SystemPromptMiddleware`：注入 loop 目标、非目标、成功标准和人工交接规则。
- `ReplyMiddleware`：初始化 `state.LoopContext`，记录指标并发出生命周期事件。
- `ReasoningMiddleware`：发出 iteration 事件，并在预算触顶后注入 wrap-up hint。
- `ModelCallMiddleware`：预算触顶后强制 `tool_choice=none`。
- `ActingMiddleware`：保留工具执行链，不替换业务工具。

## 状态与事件

启用 `loop.WithSpec` 后，Agent 状态中会维护 `state.LoopContext`。它记录 loop 名称、目标、模式、轮次、模型调用数、工具调用数、token、最新验证结果和停止原因。

事件通过现有 `message.CustomEvent` 输出：

| 事件 | 含义 |
| --- | --- |
| `loop.start` | 一次 loop run 开始。 |
| `loop.iteration_start` | 一轮 reasoning 开始。 |
| `loop.iteration_end` | 一轮 reasoning 结束。 |
| `loop.verify_start` | verifier 开始检查。 |
| `loop.verify_end` | verifier 返回结果。 |
| `loop.wrap_up` | loop 进入收束阶段。 |
| `loop.stop` | 一次 loop run 结束。 |

这些事件可以被现有 SSE 和 AG-UI 转换 middleware 保留为 custom event。

## 设计边界

`loop` 包只提供框架 API，不内置以下能力：

- cron、scheduler 或后台常驻服务。
- Git worktree 创建和清理。
- GitHub PR 创建、评论、合并。
- Jira、Linear、Slack、钉钉等业务连接器。
- 固定 `STATE.md` 文件格式。
- 自动 merge 或自动发布策略。

应用可以使用 `workspace`、`team`、`tool/mcp`、`tool/task` 和业务代码组合这些能力。

## 示例

- `example/loop/basic`：report-only loop，演示目标、成功标准、状态和事件。
- `example/loop/assisted-verifier`：assisted loop，演示 verifier 和 maker/checker 分离。
