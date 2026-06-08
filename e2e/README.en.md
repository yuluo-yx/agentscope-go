# AgentScope Go E2E

This directory contains the end-to-end test entry point for AgentScope Go. It verifies full workflows across package APIs, Agents, tool calls, workspaces, MCP, gateways, and live model services.

## How To Run

Download dependencies for the standalone E2E module before the first run:

```bash
make e2e-deps
```

Run local E2E without external provider keys:

```bash
make e2e-test-no-key
```

This command runs the `local` profile and removes `DASHSCOPE_API_KEY` and `AI_DASHSCOPE_API_KEY` from the child process environment. Use it for deterministic scenarios that do not depend on live model services.

Run live DashScope E2E:

```bash
DASHSCOPE_API_KEY=your-key make e2e-test-dashscope
```

The default chat model is `qwen-plus`. Override it with `AGENTSCOPE_TEST_DASHSCOPE_MODEL` or `AI_DASHSCOPE_MODEL`. The default text embedding model is `text-embedding-v4`. Override it with `AGENTSCOPE_TEST_DASHSCOPE_EMBEDDING_MODEL` or `AI_DASHSCOPE_EMBEDDING_MODEL`.

Run explicit provider smoke tests:

```bash
make test-provider-smoke
```

This command runs the `provider-smoke` profile. Each live provider case requires its matching `AGENTSCOPE_TEST_*` switch and API key. Cases that are not explicitly enabled are reported as `SKIPPED` and do not fail the profile.

Run Docker workspace E2E:

```bash
make test-e2e-docker
```

This command requires a working local Docker environment.

Run Agent Sandbox workspace E2E:

```bash
make test-e2e-agent-sandbox
```

This command prepares Agent Sandbox resources through KinD. It requires Kubernetes, KinD, and a container runtime on the local machine.

Run a specific profile:

```bash
make e2e-test E2E_PROFILE=local
make e2e-test E2E_PROFILE=dashscope-live
```

Run specific testcases:

```bash
make e2e-test-specific E2E_TESTS=agent-tool-loop
make e2e-test-specific E2E_PROFILE=local E2E_TESTS=agent-tool-loop,workspace-local-files
```

Run the runner directly:

```bash
make build-e2e
./bin/e2e -profile=local -tests=agent-tool-loop -verbose=true -report-dir=e2e/reports/local
```

Reports are written to `e2e/reports/<profile>/` in JSON and Markdown formats.

## Design

E2E uses a standalone Go module and a unified runner. It does not copy old unit, integration, or architecture test directories into `e2e/`. Each testcase should represent a complete user workflow or cross-package API contract, with emphasis on real behavior after multiple modules are composed.

Tests are grouped by profile. The `local` profile stays deterministic and does not depend on external model services. The `dashscope-live` profile covers real DashScope calls. The `provider-smoke` profile calls third-party providers only when explicitly enabled. The `docker` and `agent-sandbox` profiles cover workspace scenarios that require real infrastructure.

The runner owns profile registration, testcase selection, timeout handling, serial or parallel execution, and report generation. Profiles own default testcase ordering and environment setup. Testcases focus on workflow behavior instead of scattering environment checks, reporting logic, and execution policy across individual files.

Local no-key verification and live-service verification should be run separately. This keeps default validation stable and repeatable while making live E2E failures clearly attributable to external services, API keys, network conditions, or provider quotas.
