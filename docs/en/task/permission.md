# Permission

The `permission` package decides whether a tool call can run.

## Modes

| Mode | Behavior |
| --- | --- |
| `default` | Ask unless a tool or rule allows the call |
| `accept_edits` | Allow read-only tools, ask for writes |
| `explore` | Allow read-only tools, deny writes |
| `bypass` | Allow after tool-specific checks |
| `dont_ask` | Deny when a user decision would be required |

## Engine

```go
ctx := permission.NewContext(permission.ModeExplore)
engine := permission.NewEngine(ctx)

decision, err := engine.CheckPermission(context.Background(), readTool, input)
```

The engine checks deny rules, ask rules, mode behavior, tool-specific decisions, allow rules, and final defaults.

## Rules

```go
engine.AddRule(permission.Rule{
	ToolName:    "Write",
	RuleContent: "/tmp/agentscope/**",
	Behavior:    permission.BehaviorAllow,
	Source:      "example",
})
```

Tools decide how to match `RuleContent`. Built-in file tools use path-oriented rules. Function tools match empty rules by default unless configured otherwise.
