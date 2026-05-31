# Workspace

`workspace.LocalWorkspace` 为工具和资源提供本地环境。

## 本地 Workspace

本地 Workspace 会初始化一个目录，并创建以下子目录：

- `data/`：工具使用的文件。
- `skills/`：本地 Skill。
- `sessions/`：卸载后的上下文和工具结果。

```go
ws, err := workspace.NewLocalWorkspace("/tmp/agentscope-workspace")
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}
```

## 工具

`ListTools` 会暴露内置本地文件和 Shell 工具：

```go
tools, err := ws.ListTools(ctx)
```

当智能体需要使用 Workspace 支持的工具时，把这些工具注册到 Toolkit。

## Skills

使用 `workspace.WithSkillPaths` 预置 Skill：

```go
ws, err := workspace.NewLocalWorkspace(
	"/tmp/agentscope-workspace",
	workspace.WithSkillPaths("./skills/review"),
)
```

## 卸载

Workspace 可以把对话上下文和工具结果卸载到文件中。这样可以把大内容移出当前模型上下文，同时保留可追溯记录。
