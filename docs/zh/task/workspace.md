# Sandbox 沙箱

`workspace/local.Workspace` 是本地沙箱实现，为工具和资源提供运行环境。

Sandbox 沙箱是 AgentScope Go 中 Agent 运行时和工具执行环境的抽象。它负责准备文件目录、暴露内置工具、加载 Skill、恢复 MCP 配置，并把过大的上下文或工具结果卸载到外部存储。相关 Go API 位于 `workspace` 包。

沙箱不是每个 Agent 都必需。只有当工具需要文件系统、Shell、Skill、MCP 持久化或大内容卸载时，才需要接入。

```{mermaid}
flowchart TD
    Workspace["Sandbox 沙箱"] --> Dirs["data / skills / sessions"]
    Workspace --> Tools["Bash / Read / Write / Edit / Glob / Grep"]
    Workspace --> Skills["Skill 摘要和 Skill 查看工具"]
    Workspace --> MCP["MCP 配置和 MCP 工具"]
    Workspace --> Offload["上下文、工具结果和 DataBlock 卸载"]
    Workspace --> Resources["workspace.BuildAgentResources"]
    Resources --> Agent["agent.WithAgentResources"]
    Workspace --> WithWorkspace["agent.WithWorkspace"]
    WithWorkspace --> Agent
```

## 什么时候需要 Sandbox 沙箱

适合使用沙箱的场景：

- Agent 需要读写文件、运行 Shell 或搜索目录。
- 需要把 `SKILL.md` 作为智能体可读取的操作说明。
- 需要把 MCP 配置和工具恢复放在一个工作环境中管理。
- 需要把 base64 数据块、大工具结果或旧上下文卸载到文件。
- 需要让同一套工具在本地、Docker、Agent Sandbox 或 OpenSandbox 运行时之间切换。

不需要沙箱的场景：

- 只有一次模型调用。
- 只有少量无副作用函数工具。
- 文件、任务和业务状态已经由应用服务管理，不希望暴露给模型。

## 后端选择

| 后端 | 包 | 适用场景 | 主要依赖 |
| --- | --- | --- | --- |
| 本地沙箱 | `workspace/local` | 本地开发、测试、普通服务端文件工具 | 本机文件系统 |
| Docker 沙箱 | `workspace/docker` | 需要容器隔离的文件和 Shell 工具 | Docker engine |
| Microsandbox 沙箱 | `workspace/microsandbox` | 需要本地 microVM 隔离的文件和 Shell 工具 | Microsandbox Go SDK、KVM 或 Apple Silicon |
| Agent Sandbox 后端 | `workspace/agentsandbox` | Kubernetes 中运行隔离工具任务 | Kubernetes、agent-sandbox controller 和 SandboxTemplate |
| Daytona 沙箱 | `workspace/daytona` | 使用 Daytona 托管或自托管沙箱执行文件和 Shell 工具 | Daytona API 与 Go SDK |
| OpenSandbox 沙箱 | `workspace/opensandbox` | 按稳定 Workspace ID 创建、连接或恢复远程沙箱 | OpenSandbox API 与 Go SDK v1.0.4 |

本地沙箱最容易开始。Docker 和 Agent Sandbox 适合隔离更强或运行环境需要复现的场景。OpenSandbox 适合需要托管远程执行、暂停恢复和跨进程 Workspace 持久化的场景。

## 本地沙箱

本地沙箱会初始化一个目录，并创建以下子目录：

- `data/`：工具使用的文件。
- `skills/`：本地 Skill。
- `sessions/`：卸载后的上下文和工具结果。

```go
ws, err := local.NewWorkspace("/tmp/agentscope-sandbox")
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}
```

`Initialize` 是幂等的。使用 `agent.WithWorkspace(ctx, ws)` 时，如果沙箱尚未初始化，Agent 会自动初始化它。

## Docker 沙箱

`workspace/docker.Workspace` 会在 Docker 容器中执行内置工具。它适合本地隔离执行 Shell、文件读写和搜索任务。

```go
ws, err := docker.NewWorkspace(
	docker.WithImage("ubuntu:latest"),
	docker.WithHostWorkdir("/tmp/agentscope-docker-sandbox"),
)
if err != nil {
	panic(err)
}
```

当设置 `WithHostWorkdir` 时，offload、Skill 和 MCP 索引会写入宿主机 mirror 目录。
如果没有设置 `WithHostWorkdir`，Docker 后端仍可执行容器内工具，但 `OffloadContext`、`OffloadToolResult`、`OffloadDataBlock`、`AddSkill` 和 `RemoveSkill` 会返回需要 host workdir 的错误。

## Microsandbox 沙箱

`workspace/microsandbox.Workspace` 会通过 Microsandbox 官方 Go SDK 创建本地 microVM，并在 microVM 中执行 `Bash`、`Read`、`Write`、`Edit`、`Glob` 和 `Grep` 工具。

```go
ws, err := microsandbox.NewWorkspace(
	microsandbox.WithImage("python:3.12"),
	microsandbox.WithHostWorkdir("/tmp/agentscope-microsandbox-sandbox"),
)
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}
```

前置条件：

- Linux 且启用 KVM，或 Apple Silicon macOS。
- Microsandbox runtime 资产可用。默认情况下，`Initialize` 会调用 `EnsureInstalled`，SDK 会把缺失资产下载到 `~/.microsandbox/`。

如果运行时已经安装且启动阶段不能下载资产，可以使用 `WithEnsureInstalled(false)`。如果需要在 `Close` 后保留 microVM 便于排查，可以使用 `WithKeepSandbox(true)`。

## Agent Sandbox 后端

`workspace/agentsandbox.Workspace` 会通过 agent-sandbox Go SDK 创建 Kubernetes `SandboxClaim`，并在 Agent Sandbox runtime 中执行 `Bash`、`Read`、`Write`、`Edit`、`Glob` 和 `Grep` 工具。

```go
ws, err := agentsandbox.NewWorkspace(
	agentsandbox.WithTemplateName("agent-sandbox-template"),
	agentsandbox.WithNamespace("default"),
	agentsandbox.WithHostWorkdir("/tmp/agentscope-agent-sandbox"),
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
- 目标 namespace 中存在 `SandboxTemplate`，模板名称以集群配置为准。

连接模式：

- 默认：port-forward 模式，适合本地和 kind 测试。
- `WithAPIURL`：连接 sandbox-router direct URL。
- `WithGateway`：通过 Kubernetes Gateway API 连接。

`Write` 工具继续接受绝对路径。由于 agent-sandbox Go SDK 的 `Write()` 只接受普通文件名，AgentScope-Go 会先上传临时文件，再在 sandbox 内移动到目标绝对路径。

Agent Sandbox 后端的部署前置条件较多。已经在 Kubernetes 中运行 agent-sandbox，并且需要复用其隔离能力时，才适合选择该后端。普通本地开发优先使用本地或 Docker 沙箱。

## Daytona 沙箱

`workspace/daytona.Workspace` 会通过 Daytona 官方 Go SDK 创建或连接 Daytona sandbox，并在 Daytona runtime 中执行 `Bash`、`Read`、`Write`、`Edit`、`Glob` 和 `Grep` 工具。

```go
ws, err := daytona.NewWorkspace(
	daytona.WithImage("python:3.12"),
	daytona.WithHostWorkdir("/tmp/agentscope-daytona-sandbox"),
)
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}
```

前置条件：

- Daytona 账号，或兼容的自托管 Daytona API。
- `DAYTONA_API_KEY`，或 Daytona SDK 支持的 JWT 环境变量组合。
- 可选 `DAYTONA_API_URL` 与 `DAYTONA_TARGET`，用于自定义 API 地址和 target/region。

默认情况下，新建的 Daytona sandbox 会在 `Close` 时删除。需要保留沙箱用于排查时，使用 `WithKeepSandbox(true)`；需要接入已有沙箱时，使用 `WithSandboxID` 或 `WithSandboxName`，关闭时只断开本地连接，不删除远端沙箱。

## OpenSandbox 远程沙箱

`workspace/opensandbox.Workspace` 使用 OpenSandbox 官方 Go SDK v1.0.4 管理远程 Sandbox。它通过稳定的 Workspace ID 恢复远程状态，并在 Sandbox 内运行文件工具、Shell 工具和 Python MCP gateway。

```go
ws, err := opensandbox.New(
	opensandbox.WithWorkspaceID("review-session-42"),
	opensandbox.WithProtocol("https"),
)
if err != nil {
	panic(err)
}
if err := ws.Initialize(ctx); err != nil {
	panic(err)
}

fmt.Println(ws.WorkspaceID())
fmt.Println(ws.SandboxID())

if err := ws.Close(ctx); err != nil {
	panic(err)
}
```

`New` 和 `NewWorkspace` 只校验配置，不会发起远程请求。需要跨进程恢复文件时，必须通过 `WithWorkspaceID` 使用稳定 ID。未指定 ID 时，构造函数会生成新 ID，后续进程无法自动找到原来的 Sandbox。

### 连接配置

OpenSandbox SDK 默认从以下环境变量读取连接信息：

- `OPEN_SANDBOX_DOMAIN`：OpenSandbox API 域名。
- `OPEN_SANDBOX_API_KEY`：OpenSandbox API key。

也可以通过 `WithDomain` 和 `WithAPIKey` 显式传入。协议默认是 `http`；生产环境通常应使用 `WithProtocol("https")`。默认镜像是 `python:3.11-slim`，默认 Sandbox TTL 和连接就绪超时是 5 分钟，SDK 单次请求超时是 10 分钟。可分别使用 `WithImage`、`WithTimeout` 和 `WithRequestTimeout` 修改。

创建新 Sandbox 时还可以配置环境变量、metadata、资源上限、entrypoint 和网络策略。Workspace 会强制写入 `agentscope.workspace.id` metadata，用于后续查找同一逻辑 Workspace。OpenSandbox SDK 依赖在根模块的 `go.mod` 中固定为 `github.com/alibaba/OpenSandbox/sdks/sandbox/go v1.0.4`。

### 默认目录

| 路径或端口 | 用途 |
| --- | --- |
| `/workspace` | Agent 可见的 Workspace 根目录 |
| `/workspace/data` | 文件和 DataBlock 卸载目录 |
| `/workspace/skills` | Skill 目录 |
| `/workspace/sessions` | 上下文和工具结果卸载目录 |
| `/workspace/.mcp` | 可由 Go 和 AgentScope Python 共同读取的 MCP 索引 |
| `/root/.agentscope/.venv` | Python gateway 独立虚拟环境 |
| `/root/.agentscope/_mcp_gateway_app.py` | 远程 gateway 启动脚本 |
| `/root/.agentscope/gateway.log` | gateway 启动和运行日志 |
| `127.0.0.1:5600` | Sandbox 内 gateway 默认 loopback 地址 |

这些路径目前是 OpenSandbox Workspace 的固定契约。`WithGatewayPort` 可以修改端口，Workspace 根目录和 gateway home 暂不提供 Option。

### 初始化与首次 bootstrap

`Initialize` 是幂等操作。第一次调用会按以下顺序执行：

1. 按 `agentscope.workspace.id` 列出 Running 和 Paused 状态的 Sandbox；没有匹配项时创建新 Sandbox，有匹配项时连接或恢复最新实例。
2. 等待 Sandbox 健康，并创建 `/workspace/data`、`/workspace/skills`、`/workspace/sessions` 与 gateway home。
3. 读取或播种 `/workspace/.mcp`，然后把配置统一写成 Python canonical 格式。
4. 如果 gateway Python、脚本或 bootstrap 指纹缺失或不匹配，安装系统工具、`uv`、`mcp`、`uvicorn`、`fastapi` 和固定版本 `agentscope==2.0.4`，再上传 gateway 脚本并最后写入版本 marker。通过 `WithExtraPythonPackages` 指定的包也会在此阶段安装。
5. 重启 loopback gateway，等待 `/health` 可用，核对恢复后的 MCP 数量，再播种 `WithSkillPaths` 指定的 Skill。

gateway 脚本会在所有安装命令成功后最后写入。因此，首次 bootstrap 中断时，下次 `Initialize` 会重新执行完整引导。默认镜像和首次引导需要访问 Debian 软件源、`astral.sh` 与 Python 包索引；自定义镜像或网络策略必须允许这些请求，或预先提供等价运行环境。

### Python gateway 与 `.mcp` 兼容

远程 gateway 只监听 Sandbox 内的 `127.0.0.1`。Go 侧不会要求 OpenSandbox 暴露 5600 端口，而是通过远程 `python3 -c` shim 请求 loopback 地址。响应不超过 4 MiB 时以内联 Base64 返回；更大的响应先写入 Sandbox 临时文件，再由 SDK 使用有界流下载并删除。Sandbox shim 和宿主下载两侧都会拒绝超过 16 MiB 的响应。

`/workspace/.mcp` 写出时始终使用 AgentScope Python canonical JSON：MCP 类型和连接参数位于嵌套的 `mcp_config` 中，`timeout` 与 `execution_timeout` 使用秒。读取时同时兼容以下两种格式：

- AgentScope Python canonical 格式。
- AgentScope Go 旧版顶层 `type`、`stdio` 或 `http` 格式。

旧格式会在下一次初始化或 MCP 变更时重写为 canonical 格式。这样，同一个远程 Workspace 可以在 AgentScope Go 与 AgentScope Python 2.0.4 之间复用 `.mcp`。Go HTTP MCP 的自定义 `Transport` 和 `ContinuousListening` 在 Python 模型中没有等价字段，持久化这些配置会返回错误，不会静默丢失语义。

### Reset 与 Close

`Reset` 和 `Close` 的作用不同：

- `Reset` 要求 Workspace 已初始化。它保留 Sandbox 和 gateway 进程，移除 gateway 中的 MCP，删除并重建 `data`、`skills`、`sessions`，再把 `.mcp` 写成空数组。
- `Close` 关闭本地 gateway facade，暂停远程 Sandbox，并释放本地 SDK handle。它不会删除远程 Sandbox，也不会清除 Workspace 文件。

使用同一 Workspace ID 再次调用 `Initialize` 时，Paused Sandbox 会被恢复，Running Sandbox 会被重新连接。gateway 会重新启动，并从保留的 `.mcp` 恢复 MCP。需要彻底删除远程资源时，应通过 OpenSandbox 管理面或 SDK 的删除能力显式处理；`Workspace.Close` 的契约是暂停和保留。

同一 Workspace ID 当前采用单活调用约束：一个时刻只能由一个进程持有并修改对应 Sandbox。进程内的 `Close`、`Reset` 和 MCP 变更会等待正在执行的 gateway 请求，但该实现不提供跨进程分布式锁；两个进程同时连接同一 ID 会竞争 gateway、`.mcp` 和远程文件。服务层应按 Workspace ID 串行化租约。

### CI 与 E2E 策略

OpenSandbox 测试统一在 GitHub Actions 的 `.github/workflows/opensandbox.yml` 中运行，本地不执行 `pkg/workspace/opensandbox` 的单元测试或 E2E，以避免占用本地远程 Sandbox 和运行资源。

- Unit job 不读取 OpenSandbox secrets，只对 `pkg/workspace/opensandbox` 执行 race test、覆盖率检查和 `go vet`；覆盖率必须达到 90%。
- E2E job 使用 `integration` build tag，最长运行 25 分钟，job 超时为 30 分钟。
- E2E job 优先从 GitHub environment `opensandbox-e2e` 读取 `OPEN_SANDBOX_DOMAIN` 和 `OPEN_SANDBOX_API_KEY`。未配置 endpoint 时，GitHub runner 会安装固定版本 `opensandbox-server==0.2.1`，使用官方 Docker 示例配置启动一次性服务；本地服务不启用 API key，测试结束后清理 Sandbox 容器。
- 同仓库 PR、push 和手动触发可以运行 E2E；fork PR 只运行不使用 secrets 的 Unit job。
- E2E 使用全局并发组串行执行，等待中的运行不会取消已经开始的 E2E。

工作流由 OpenSandbox、共享远程生命周期、gateway、`go.mod`、`go.sum` 或工作流自身的改动触发。提交前只做静态文件检查；完整的 OpenSandbox 验证结果以 GitHub Actions 为准。

## 工具

`ListTools` 会暴露内置本地文件和 Shell 工具：

```go
tools, err := ws.ListTools(ctx)
```

当智能体需要使用沙箱支持的工具时，把这些工具注册到 Toolkit。

```go
tools, err := ws.ListTools(ctx)
if err != nil {
	panic(err)
}

kit, err := tool.NewToolkit(tools...)
if err != nil {
	panic(err)
}
```

Docker、Microsandbox、Agent Sandbox 与 OpenSandbox 后端会保留和本地沙箱一致的模型可见
`Bash`、`Read`、`Write`、`Edit`、`Glob`、`Grep` schema，但执行时会进入对应
后端运行时：Docker 工具通过 Docker engine 作用于沙箱容器，Microsandbox
工具通过 Microsandbox SDK handle 执行，Agent Sandbox 工具通过 sandbox handle
执行，OpenSandbox 工具通过 OpenSandbox SDK handle 执行。这些工具调用不得回落到宿主机执行。

AgentScope Go 对内置沙箱工具采用类型化 runtime adapter：Docker
工具通过 Docker engine 执行，Microsandbox 工具通过 SDK handle 执行，Agent Sandbox 工具通过 sandbox handle 执行，OpenSandbox 工具通过远程 Backend 执行。
MCP server 仍可通过 `workspace/gateway.Server` 和宿主侧 gateway client 暴露。
依赖 Docker、Microsandbox 或 Agent Sandbox 的测试继续显式门控：
`AGENTSCOPE_TEST_DOCKER=1`、`AGENTSCOPE_TEST_MICROSANDBOX=1` 与 `AGENTSCOPE_TEST_AGENT_SANDBOX=1`。
OpenSandbox 使用独立 GitHub Actions 工作流，不加入本地测试门控变量。

## 与 Agent 组合

最简单的组合方式是使用 `agent.WithWorkspace`：

```go
ws, err := local.NewWorkspace("/tmp/agentscope-sandbox")
if err != nil {
	panic(err)
}

runner, err := agent.NewAgent(
	"Friday",
	"Use the sandbox when files are needed.",
	chat,
	agent.WithWorkspace(ctx, ws),
)
```

这个 Option 会完成以下事情：

1. 初始化沙箱。
2. 读取沙箱系统提示词。
3. 收集沙箱工具、MCP 工具和 Skill。
4. 创建 `Toolkit` 并注入 Agent。
5. 把沙箱设置为上下文、工具结果和 DataBlock 的 offloader。
6. 把沙箱根目录加入权限上下文的工作目录。

如果服务层想先检查资源或与其他工具合并，可以显式调用：

```go
resources, err := workspace.BuildAgentResources(ctx, ws)
if err != nil {
	panic(err)
}

runner, err := agent.NewAgent(
	"Friday",
	"Use prepared resources.",
	chat,
	agent.WithAgentResources(resources),
)
```

## Skills

使用 `local.WithSkillPaths` 预置 Skill：

```go
ws, err := local.NewWorkspace(
	"/tmp/agentscope-sandbox",
	local.WithSkillPaths("./skills/review"),
)
```

沙箱初始化时会把 Skill 复制或链接到 `skills/` 下。`BuildAgentResources` 会把 Skill 摘要写入系统提示词，并额外暴露 `Skill` 查看工具，让模型按需读取完整 `SKILL.md`。

Skill 是操作说明，不是函数调用。需要执行具体动作时，仍应通过工具或业务代码完成。

## MCP 管理

本地沙箱可以保存 MCP 配置，并在后续初始化时恢复：

```go
client, err := mcp.NewHTTPClient(
	"people",
	mcp.HTTPConfig{URL: "https://example.com/mcp"},
	mcp.WithEnabledTools("lookup_profile"),
)
if err != nil {
	panic(err)
}

ws, err := local.NewWorkspace(
	"/tmp/agentscope-sandbox",
	local.WithMCPs(client),
)
```

如果需要从 `.mcp` 索引恢复自定义 MCP 客户端，可以提供 `local.WithMCPClientFactory`。

## 卸载

Sandbox 沙箱可以把对话上下文和工具结果卸载到文件中。这样可以把大内容移出当前模型上下文，同时保留可追溯记录。

```go
path, err := ws.OffloadContext(ctx, "session-1", []*message.Message{user})
if err != nil {
	panic(err)
}
fmt.Println(path)
```

卸载常见于三类内容：

- 旧消息摘要：上下文压缩后保留引用。
- 超长工具结果：模型上下文中只保留摘要或引用。
- base64 数据块：把二进制内容转成 URL-backed `DataBlock`。

## 生命周期

### 通用契约

Sandbox 沙箱生命周期由调用方管理：

- `Initialize`：创建目录、恢复 MCP、加载 Skill。
- `Close`：释放运行时资源或标记为不活跃，不保证删除持久化数据。
- `Reset`：清空沙箱拥有的数据、sessions、skills 和 MCP 索引。

服务端应用通常在请求或会话开始时初始化，在会话结束时关闭。是否重用同一个沙箱取决于隔离需求：共享沙箱可以复用文件和 Skill，独立沙箱更容易避免会话互相影响。

### 远程 Workspace 公共生命周期

`pkg/workspace/internal/sandboxed` 为需要远程 Backend 和 Sandbox 内 Python gateway 的实现提供公共生命周期。这个包是内部实现，不作为用户构造入口；OpenSandbox 通过它统一完成以下流程：

1. Provider 创建、连接或恢复远程运行时，并返回只包含 `Exec`、`ReadFile` 和 `WriteFile` 的 Backend。
2. 生命周期创建 Workspace 目录，恢复或播种 `.mcp`，并通过指定 codec 写回规范格式。
3. gateway 不存在时执行 bootstrap，随后通过 Sandbox 内 loopback 启动并检查健康状态。
4. gateway 返回实际恢复的 MCP 配置；生命周期据此创建代理客户端并播种 Skill。
5. 任一步失败时，生命周期回滚 gateway、本地状态和 Provider handle，不把半初始化实例标记为 alive。

公共生命周期使用互斥锁串行化 `Initialize`、`Reset`、MCP 变更和 `Close`。`Initialize` 成功后再次调用会直接返回。`Reset` 保留 Backend 和 gateway，清除 Workspace 自有状态；`Close` 先释放 gateway facade，再把远程运行时的保留或销毁动作交给 Provider。OpenSandbox Provider 选择暂停，未来远程 Provider 可以按自身契约实现删除或断开。

## 取舍建议

- 本地开发和单机服务：优先 `workspace/local`。
- 需要容器隔离：使用 `workspace/docker`，并配置 `WithHostWorkdir`。
- 需要本地 microVM 隔离：使用 `workspace/microsandbox`。
- 已部署 agent-sandbox：使用 `workspace/agentsandbox`。
- 需要托管远程沙箱、稳定 ID 恢复和 Python MCP 互操作：使用 `workspace/opensandbox`。
- 只需要普通函数工具：不要引入沙箱。
- 需要 Agent 自动装配工具和 offloader：使用 `agent.WithWorkspace`。
- 需要先审查资源再装配：使用 `workspace.BuildAgentResources`。
