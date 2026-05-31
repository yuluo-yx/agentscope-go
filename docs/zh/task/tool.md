# 工具系统

工具允许模型请求 Go 代码执行某项能力。

## 函数工具

使用 `tool.NewFunctionTool` 把普通 Go 函数包装成工具：

```go
greet, err := tool.NewFunctionTool(
	"Greet",
	"Return a greeting for one name.",
	map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	},
	func(_ context.Context, input map[string]any, _ *state.AgentState) (message.ContentBlockList, error) {
		name, _ := input["name"].(string)
		return message.ContentBlockList{message.NewTextBlock("hello " + name)}, nil
	},
	tool.WithFunctionReadOnly(true),
)
```

## Toolkit

`tool.Toolkit` 负责注册工具、暴露模型 Schema，并执行模型创建的工具调用：

```go
kit, err := tool.NewToolkit(greet)
schemas, err := kit.ToolSchemas()
result, err := kit.RunTool(ctx, toolCall, state.NewAgentState())
```

## 内置工具

`tool/builtin` 包含：

- `Bash`
- `Edit`
- `Glob`
- `Grep`
- `Read`
- `Write`
- `ResetTools`

这些工具会接入权限系统。写入类操作默认需要询问，除非权限规则明确允许。

## 任务工具

`tool/task` 提供任务管理工具：

- `TaskCreate`
- `TaskGet`
- `TaskList`
- `TaskUpdate`

任务工具会写入 `state.AgentState.TaskContext`。

## Skill 工具

`tool/skill` 用于加载本地 `SKILL.md`。当项目需要把本地操作说明暴露给智能体时，可以使用该包。
