# Workspace

`workspace/local.Workspace` 为工具和资源提供本地环境。

## 本地 Workspace

本地 Workspace 会初始化一个目录，并创建以下子目录：

- `data/`：工具使用的文件。
- `skills/`：本地 Skill。
- `sessions/`：卸载后的上下文和工具结果。

```go
ws, err := local.NewWorkspace("/tmp/agentscope-workspace")
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}
```

## Docker Workspace

`workspace/docker.Workspace` 会在 Docker 容器中执行 workspace 工具。它适合本地隔离执行 Shell、文件读写和搜索任务。

```go
ws, err := docker.NewWorkspace(
	docker.WithImage("ubuntu:latest"),
	docker.WithHostWorkdir("/tmp/agentscope-docker-workspace"),
)
if err != nil {
	panic(err)
}
```

当设置 `WithHostWorkdir` 时，offload、Skill 和 MCP 索引会写入宿主机 mirror 目录。

## Agent Sandbox Workspace

`workspace/agentsandbox.Workspace` 会通过 agent-sandbox Go SDK 创建 Kubernetes `SandboxClaim`，并在 Agent Sandbox runtime 中执行 `Bash`、`Read`、`Write`、`Edit`、`Glob` 和 `Grep` 工具。

```go
ws, err := agentsandbox.NewWorkspace(
	agentsandbox.WithTemplateName("python-sandbox-template"),
	agentsandbox.WithNamespace("default"),
	agentsandbox.WithHostWorkdir("/tmp/agentscope-agent-sandbox-workspace"),
)
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}
```

前置条件：

- Kubernetes 集群可访问。
- agent-sandbox controller、extensions 和 sandbox-router 已安装。
- 当前 kubeconfig 有权限创建 `SandboxClaim`。
- 目标 namespace 中存在 `SandboxTemplate`，默认示例使用 `python-sandbox-template`。

连接模式：

- 默认：port-forward 模式，适合本地和 kind 测试。
- `WithAPIURL`：连接 sandbox-router direct URL。
- `WithGateway`：通过 Kubernetes Gateway API 连接。

`Write` 工具继续接受绝对路径。由于 agent-sandbox Go SDK 的 `Write()` 只接受普通文件名，AgentScope-Go 会先上传临时文件，再在 sandbox 内移动到目标绝对路径。

## 工具

`ListTools` 会暴露内置本地文件和 Shell 工具：

```go
tools, err := ws.ListTools(ctx)
```

当智能体需要使用 Workspace 支持的工具时，把这些工具注册到 Toolkit。

Docker 与 Agent Sandbox 后端会保留和本地 Workspace 一致的模型可见
`Bash`、`Read`、`Write`、`Edit`、`Glob`、`Grep` schema，但执行时会进入对应
后端运行时：Docker 工具通过 Docker engine 作用于 workspace 容器，Agent
Sandbox 工具通过 sandbox handle 执行。这些工具调用不得回落到宿主机执行。

这与 Python Docker/E2B 通过 workspace 内 gateway 暴露工具的路径不同。当前
AgentScope Go 将该差异记录为有意边界：内置 workspace 工具使用类型化 Go
runtime adapter，MCP server 仍可通过 `workspace/gateway.Server` 和宿主侧
gateway client 暴露。依赖 Docker 或 Agent Sandbox 的测试继续显式门控：
`AGENTSCOPE_TEST_DOCKER=1` 与 `AGENTSCOPE_TEST_AGENT_SANDBOX=1`。

## Skills

使用 `local.WithSkillPaths` 预置 Skill：

```go
ws, err := local.NewWorkspace(
	"/tmp/agentscope-workspace",
	local.WithSkillPaths("./skills/review"),
)
```

## 卸载

Workspace 可以把对话上下文和工具结果卸载到文件中。这样可以把大内容移出当前模型上下文，同时保留可追溯记录。
