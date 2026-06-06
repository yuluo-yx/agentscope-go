# Agent Sandbox Workspace 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示 `workspace/agentsandbox.Workspace`：

- 通过 agent-sandbox Go SDK 创建 Kubernetes `SandboxClaim`。
- 使用 `SandboxTemplate` 启动 Python runtime sandbox。
- 使用 workspace `Write` 和 `Read` 工具操作 sandbox 内 `/home/user` 下的文件。
- 将 offload、Skill 和 MCP 索引写入宿主机临时 mirror 目录。
- 在存在 DashScope API Key 时，把工具读取结果发送给 DashScope ChatModel。

## 前置条件

- Go 1.26.3。
- 可访问的 Kubernetes 集群。
- 已安装 agent-sandbox controller、extensions、sandbox-router。
- 当前 kubeconfig 有权限创建 `SandboxClaim`。
- 目标 namespace 中存在 `SandboxTemplate`，默认名称为 `python-sandbox-template`。
- 可选：DashScope API Key，用于真实 ChatModel 调用。

可以用仓库根目录的 kind target 准备测试环境：

```bash
make test-e2e-agent-sandbox
```

该 target 会创建 kind 集群并安装 agent-sandbox 资源，然后运行集成和 E2E 测试。

## 运行

```bash
cd example/workspace/agentsandbox
go run .
```

指定 template 或 namespace：

```bash
AGENTSCOPE_AGENT_SANDBOX_TEMPLATE=python-sandbox-template \
AGENTSCOPE_AGENT_SANDBOX_NAMESPACE=default \
go run .
```

使用 direct URL 模式：

```bash
AGENTSCOPE_AGENT_SANDBOX_API_URL=http://sandbox-router-svc.default.svc.cluster.local:8080 go run .
```

使用 Gateway 模式：

```bash
AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAME=kind-gateway \
AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAMESPACE=default \
go run .
```

运行真实 ChatModel 路径：

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

## 预期输出

没有 API Key 时，输出包含：

```text
agent_sandbox_workspace_alive=true
read_has_brief=true
dashscope_live=skipped
```

有 API Key 时，输出还包含：

```text
dashscope_live=ok
```
