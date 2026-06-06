# 测试目录说明

项目主页：[README-zh.md](../README-zh.md)。

英文文档：[README.md](README.md)。

`test` 目录按测试范围组织：

| 目录 | 用途 |
| --- | --- |
| `architecture/` | 静态架构约束和包结构检查。 |
| `integration/` | 组件集成测试，覆盖多个包协作，但不模拟完整用户工作流。 |
| `e2e/` | 端到端框架工作流，串起 Agent、Model、Tool、Workspace 和事件。 |

## 命令

运行常规本地测试：

```bash
make test
```

只运行集成测试：

```bash
make test-integration
```

只运行 E2E 测试：

```bash
make test-e2e
```

运行 Docker 支撑的 E2E 和集成测试：

```bash
make test-e2e-docker
```

`test-e2e-docker` 使用 `DOCKER_TEST_IMAGE`，默认值是 `ubuntu:latest`。该 target 会为 Go 测试设置 `AGENTSCOPE_E2E_DOCKER=1`、`AGENTSCOPE_TEST_DOCKER=1` 和 `AGENTSCOPE_DOCKER_IMAGE`。

运行 Agent Sandbox 支撑的 E2E 和集成测试：

```bash
make test-e2e-agent-sandbox
```

`test-e2e-agent-sandbox` 会先执行 `agent-sandbox-kind-setup`，创建或复用名为 `agentscope-agent-sandbox` 的 kind 集群，安装 agent-sandbox controller、extensions、sandbox-router、AgentScope runtime 镜像和 `python-sandbox-template`，然后设置 `AGENTSCOPE_TEST_AGENT_SANDBOX=1` 与 `AGENTSCOPE_E2E_AGENT_SANDBOX=1` 运行测试。如果 Go 测试失败，该 target 会输出 sandbox claim、sandbox、pod、router 日志、controller 日志和近期 events 诊断。

CI 管理的 Kubernetes 清单位于 `tools/agentsandbox/*.yml`；`setup-kind.sh` 会在 apply 前注入镜像、namespace 和 template 变量。

可配置变量：

- `AGENT_SANDBOX_VERSION`：agent-sandbox release 版本，默认 `v0.4.6`。
- `AGENT_SANDBOX_KIND_CLUSTER`：kind 集群名称，默认 `agentscope-agent-sandbox`。
- `AGENT_SANDBOX_ROUTER_IMAGE`：sandbox-router 镜像名称，默认 `agentscope-agent-sandbox-router:<version>`。
- `AGENT_SANDBOX_BUILD_ROUTER_IMAGE`：是否从所选 agent-sandbox release 构建 router 镜像并加载到 kind，默认 `true`。
- `AGENT_SANDBOX_RUNTIME_IMAGE`：AgentScope runtime 镜像名称，默认 `agentscope-agent-sandbox-runtime:<version>`。
- `AGENT_SANDBOX_BUILD_RUNTIME_IMAGE`：是否构建 AgentScope runtime 镜像并加载到 kind，默认 `true`。
- `AGENTSCOPE_AGENT_SANDBOX_TEMPLATE`：SandboxTemplate 名称，默认 `python-sandbox-template`。
- `AGENTSCOPE_AGENT_SANDBOX_NAMESPACE`：测试 namespace，默认 `default`。
