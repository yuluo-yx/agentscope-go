# 权限

`permission` 包决定工具调用是否可以执行。

## 模式

| 模式 | 行为 |
| --- | --- |
| `default` | 除非工具或规则允许，否则需要询问 |
| `accept_edits` | 允许只读工具，写入操作需要询问 |
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
