# AgentScope Go E2E

本目录提供 AgentScope Go 的端到端测试入口，用于验证跨包 API、Agent 工作流、工具调用、workspace、MCP、网关和真实模型服务在完整链路中的行为。

## 运行方式

初次运行前下载 E2E 独立模块依赖：

```bash
make e2e-deps
```

运行无外部 key 的本地 E2E：

```bash
make e2e-test-no-key
```

该命令运行 `local` profile，并在子进程中清空 `DASHSCOPE_API_KEY` 和 `AI_DASHSCOPE_API_KEY`，用于验证不依赖真实模型服务的确定性场景。

运行 DashScope 真实服务 E2E：

```bash
DASHSCOPE_API_KEY=your-key make e2e-test-dashscope
```

默认 chat 模型为 `qwen-plus`，可通过 `AGENTSCOPE_TEST_DASHSCOPE_MODEL` 或 `AI_DASHSCOPE_MODEL` 覆盖。默认 text embedding 模型为 `text-embedding-v4`，可通过 `AGENTSCOPE_TEST_DASHSCOPE_EMBEDDING_MODEL` 或 `AI_DASHSCOPE_EMBEDDING_MODEL` 覆盖。

运行显式 provider smoke：

```bash
make test-provider-smoke
```

该命令运行 `provider-smoke` profile。每个真实 provider 用例都需要设置对应的 `AGENTSCOPE_TEST_*` 开关和 API key；未开启的用例会记为 `SKIPPED`，不会导致整组失败。

运行 Docker workspace E2E：

```bash
make test-e2e-docker
```

该命令需要本机可用的 Docker 环境。

运行 Agent Sandbox workspace E2E：

```bash
make test-e2e-agent-sandbox
```

该命令会通过 Kind 准备 Agent Sandbox 测试资源，需要本机具备 Kubernetes、Kind 和容器运行环境。

运行指定 profile：

```bash
make e2e-test E2E_PROFILE=local
make e2e-test E2E_PROFILE=dashscope-live
```

运行指定 testcase：

```bash
make e2e-test-specific E2E_TESTS=agent-tool-loop
make e2e-test-specific E2E_PROFILE=local E2E_TESTS=agent-tool-loop,workspace-local-files
```

直接运行 runner：

```bash
make build-e2e
./bin/e2e -profile=local -tests=agent-tool-loop -verbose=true -report-dir=e2e/reports/local
```

测试报告输出到 `e2e/reports/<profile>/`，包含 JSON 和 Markdown 两种格式。

## 设计思路

E2E 使用独立 Go module 和统一 runner 组织，不把旧的单元测试、集成测试或架构测试目录原样复制到 `e2e/`。每个 testcase 应体现一个完整用户链路或跨包 API 合同，重点验证多个模块组合后的真实行为。

测试按 profile 划分运行环境。`local` profile 保持确定性，不依赖外部模型服务；`dashscope-live` profile 专门覆盖 DashScope 真实调用；`provider-smoke` profile 只在显式开启后调用第三方 provider；`docker` 和 `agent-sandbox` profile 负责需要真实基础设施的 workspace 场景。

runner 负责 profile 注册、testcase 选择、超时控制、串行或并行执行，以及报告生成。profile 负责编排默认 testcase 顺序和环境准备；testcase 只关注业务链路本身，避免把环境判断、报告写入和执行策略散落在各个测试文件中。

本地无 key 和真实服务两套验证需要分开运行。这样可以保证默认验证稳定可重复，同时让 live E2E 明确暴露外部服务、API key、网络和配额带来的不确定性。
