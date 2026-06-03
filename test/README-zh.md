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
