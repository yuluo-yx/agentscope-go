# Test Layout

Project home: [README.md](../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

The `test` directory is organized by test scope:

| Directory | Purpose |
| --- | --- |
| `architecture/` | Static architecture and package-structure checks. |
| `integration/` | Component integration tests that exercise multiple packages without full user workflows. |
| `e2e/` | End-to-end framework workflows that drive agents, models, tools, workspace, and events together. |

## Commands

Run the regular local test suite:

```bash
make test
```

Run integration tests only:

```bash
make test-integration
```

Run E2E tests only:

```bash
make test-e2e
```

Run Docker-backed E2E and integration tests:

```bash
make test-e2e-docker
```

`test-e2e-docker` uses `DOCKER_TEST_IMAGE`, defaulting to `ubuntu:latest`. It sets `AGENTSCOPE_E2E_DOCKER=1`, `AGENTSCOPE_TEST_DOCKER=1`, and `AGENTSCOPE_DOCKER_IMAGE` for the Go tests.

Run Agent Sandbox-backed E2E and integration tests:

```bash
make test-e2e-agent-sandbox
```

`test-e2e-agent-sandbox` first runs `agent-sandbox-kind-setup`, creating or reusing a KinD cluster named `agentscope-agent-sandbox`, installing the agent-sandbox controller, extensions, sandbox-router, an AgentScope runtime image, and `python-sandbox-template`, then setting `AGENTSCOPE_TEST_AGENT_SANDBOX=1` and `AGENTSCOPE_E2E_AGENT_SANDBOX=1` for the Go tests. If the Go tests fail, the target prints Kubernetes diagnostics for sandbox claims, sandboxes, pods, router logs, controller logs, and recent events.

The CI-managed Kubernetes manifests live in `tools/agentsandbox/*.yml`; `setup-kind.sh` injects image, namespace, and template variables before applying them.

Configurable variables:

- `AGENT_SANDBOX_VERSION`: agent-sandbox release version, default `v0.4.6`.
- `AGENT_SANDBOX_KIND_CLUSTER`: KinD cluster name, default `agentscope-agent-sandbox`.
- `AGENT_SANDBOX_ROUTER_IMAGE`: sandbox-router image name, default `agentscope-agent-sandbox-router:<version>`.
- `AGENT_SANDBOX_BUILD_ROUTER_IMAGE`: build the router image from the selected agent-sandbox release and load it into KinD, default `true`.
- `AGENT_SANDBOX_RUNTIME_IMAGE`: AgentScope runtime image name, default `agentscope-agent-sandbox-runtime:<version>`.
- `AGENT_SANDBOX_BUILD_RUNTIME_IMAGE`: build the AgentScope runtime image and load it into KinD, default `true`.
- `AGENTSCOPE_AGENT_SANDBOX_TEMPLATE`: SandboxTemplate name, default `python-sandbox-template`.
- `AGENTSCOPE_AGENT_SANDBOX_NAMESPACE`: test namespace, default `default`.
