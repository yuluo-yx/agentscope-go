# 权限

`permission` 包决定工具调用是否可以执行。

## 模式

| 模式 | 行为 |
| --- | --- |
| `default` | 除非工具或规则允许，否则需要询问 |
| `accept_edits` | 允许只读工具，写入操作需要询问 |
| `auto` | 由 `AutoPermissionClassifier` 判断原本需要询问的调用 |
| `explore` | 允许只读工具，拒绝写入操作 |
| `bypass` | 通过工具自身检查后允许 |
| `dont_ask` | 需要用户决策时直接拒绝 |

## 引擎

```go
ctx := permission.NewContext(permission.ModeExplore)
engine := permission.NewEngine(ctx)

decision, err := engine.CheckPermission(context.Background(), readTool, input)
```

权限引擎按顺序检查拒绝规则、询问规则、模式行为、工具特定决策、允许规则和最终默认值。
在 `explore` 和 `accept_edits` 模式下，工具可以实现可选的
`permission.InputReadOnlyTool` 接口，让只读判定基于当前输入完成。例如
`Bash` 的静态 `IsReadOnly()` 是 `false`，但 `pwd`、`git status` 等当前命令会被
识别为只读调用并放行；写入类命令仍会被拒绝或进入后续权限检查。

## Auto 权限

`ModeAuto` 面向无人值守 Agent 和 CI/CD 场景。它仍然让显式拒绝规则和询问规则保持最高优先级，直接放行只读调用，复用
`accept_edits` 模式下工具已有的安全检查；只有仍然需要人工确认的调用才交给 AI 分类器判断。分类器调用失败、返回空响应、返回非法 JSON 或非法行为时，权限引擎会 fail closed，返回 `deny`。

```go
classifier, err := agent.NewModelAutoPermissionClassifier(chatModel)
if err != nil {
	panic(err)
}

state := agent.NewAgentState()
state.PermissionContext = permission.NewContext(permission.ModeAuto)

runner, err := agent.NewAgent(
	"runner",
	"Run the requested task.",
	chatModel,
	agent.WithAgentState(state),
	agent.WithAutoPermissionClassifier(classifier),
)
```

自定义分类器实现下面的接口即可：

```go
type AutoPermissionClassifier interface {
	Classify(context.Context, permission.ClassifierRequest) (*permission.Decision, error)
}
```

分类器会收到经过清洗的 transcript：包含用户文本和历史工具调用，以及当前工具动作。普通助手文本不会进入 transcript，避免模型刚生成的文本直接授权工具调用。分类器连续拒绝达到阈值后，auto 模式会退回 `ask`，让交互式调用方可以恢复。

## 规则

```go
engine.AddRule(permission.Rule{
	ToolName:    "Write",
	RuleContent: "/tmp/agentscope/**",
	Behavior:    permission.BehaviorAllow,
	Source:      "example",
})
```

工具决定如何匹配 `RuleContent`。内置文件工具使用路径规则。函数工具默认匹配空规则，除非工具自身另有配置。
