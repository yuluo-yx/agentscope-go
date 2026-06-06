# Agent Sandbox Workspace Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example demonstrates `workspace/agentsandbox.Workspace`:

- Create a Kubernetes `SandboxClaim` through the agent-sandbox Go SDK.
- Start a Python runtime sandbox from a `SandboxTemplate`.
- Use workspace `Write` and `Read` tools against sandbox paths under `/home/user`.
- Store offload, skill, and MCP indexes in a temporary host mirror directory.
- Send the tool output to a DashScope ChatModel when an API key is available.

## Prerequisites

- Go 1.26.3.
- A reachable Kubernetes cluster.
- agent-sandbox controller, extensions, and sandbox-router installed.
- Current kubeconfig can create `SandboxClaim` resources.
- A `SandboxTemplate` exists in the target namespace. The default name is `python-sandbox-template`.
- Optional DashScope API key for the live ChatModel call.

You can prepare a test environment with the repository-level KinD target:

```bash
make test-e2e-agent-sandbox
```

The target creates a KinD cluster, installs agent-sandbox resources, and runs integration plus E2E tests.

## Run

```bash
cd example/workspace/agentsandbox
go run .
```

Set a template or namespace:

```bash
AGENTSCOPE_AGENT_SANDBOX_TEMPLATE=python-sandbox-template \
AGENTSCOPE_AGENT_SANDBOX_NAMESPACE=default \
go run .
```

Use direct URL mode:

```bash
AGENTSCOPE_AGENT_SANDBOX_API_URL=http://sandbox-router-svc.default.svc.cluster.local:8080 go run .
```

Use Gateway mode:

```bash
AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAME=kind-gateway \
AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAMESPACE=default \
go run .
```

Run the live ChatModel path:

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

## Expected Output

Without an API key, output includes:

```text
agent_sandbox_workspace_alive=true
read_has_brief=true
dashscope_live=skipped
```

With an API key, output also includes:

```text
dashscope_live=ok
```
