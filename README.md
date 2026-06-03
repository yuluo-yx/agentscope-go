<p align="center">
  <img src="docs/_static/logo.svg" alt="AgentScope Go logo" width="260">
</p>

<h1 align="center">AgentScope Go</h1>

<p align="center">
  A Go-native multi-agent application framework.
</p>

<p align="center">
  <a href="README.md">English</a> | <a href="README-zh.md">简体中文</a>
</p>

<p align="center">
  <a href="https://github.com/yuluo-yx/agentscope-go/actions/workflows/ci.yml"><img src="https://github.com/yuluo-yx/agentscope-go/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/go-1.26.3-00ADD8?logo=go" alt="Go 1.26.3"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue" alt="Apache 2.0"></a>
  <a href=".pre-commit-config.yaml"><img src="https://img.shields.io/badge/pre--commit-enabled-brightgreen" alt="pre-commit enabled"></a>
</p>

## What Is AgentScope Go?

AgentScope Go is agents, messages, model adapters, tools, state, workspace resources, and event-based
execution. It is designed for developers who want to build AI agent systems in
Go while keeping the runtime explicit, testable, and easy to integrate with
existing Go services.

The project currently focuses on text chat agents and tool-using workflows. It
does not claim feature parity with the Python AgentScope project yet;
unsupported capabilities should not be documented as available APIs.

## Requirements

- Go `1.26.3` or newer.
- GNU Make for the provided development targets.
- Python `3.13+`, Node.js `22+`, and npm `10+` if you want to run all local
  linting and documentation tools through `make setup`.

## Quick Start

Install the module in your Go project:

```bash
go get github.com/yuluo-yx/agentscope-go@latest
```

Run a local example without a model API key:

```bash
go run ./example/message
```

Run a live DashScope chat example:

```bash
export AI_DASHSCOPE_API_KEY="<your DashScope API key>"
go run ./example/model/dashscope
```

Minimal ChatModel usage:

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/yuluo-yx/agentscope-go/message"
    asmodel "github.com/yuluo-yx/agentscope-go/model"
    "github.com/yuluo-yx/agentscope-go/model/dashscope"
)

func main() {
    chat, err := dashscope.NewChatModel(
        dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
        "qwen-plus",
    )
    if err != nil {
        panic(err)
    }

    user, err := message.NewUserMessage("user", "Say hello in one short sentence.")
    if err != nil {
        panic(err)
    }

    response, err := chat.Call(context.Background(), asmodel.CallRequest{
        Messages: []*message.Message{user},
    })
    if err != nil {
        panic(err)
    }

    fmt.Println(response.Content)
}
```

## Examples

Each example is an independent Go module with its own `go.mod`, English
`README.md`, and Chinese `README-zh.md`.

| Example | Purpose |
| --- | --- |
| [`example/agent/basic`](example/agent/basic) | Build a basic agent backed by a ChatModel. |
| [`example/agent/configuration`](example/agent/configuration) | Configure model fallback, ReAct limits, and context cleanup. |
| [`example/agent/external`](example/agent/external) | Pause and resume an Agent around externally executed tools. |
| [`example/agent/hooks`](example/agent/hooks) | Implement Agent middleware hooks around reply, reasoning, model calls, acting, and system prompts. |
| [`example/agent/permission`](example/agent/permission) | Handle permission confirmation and resume a pending tool call. |
| [`example/message`](example/message) | Compose user, assistant, system, and tool messages. |
| [`example/model/dashscope`](example/model/dashscope) | Call DashScope models and demonstrate model tool calling. |
| [`example/model/providers`](example/model/providers) | Compare the available model provider adapters. |
| [`example/integration/gin`](example/integration/gin) | Expose direct ChatModel streaming and Agent event streaming through Gin. |
| [`example/integration/kratos`](example/integration/kratos) | Expose direct ChatModel streaming and Agent event streaming through Kratos. |
| [`example/tool/function`](example/tool/function) | Register and execute function tools from model tool calls. |
| [`example/tool/builtin`](example/tool/builtin) | Use builtin shell and filesystem tools with permissions. |
| [`example/tool/mcp`](example/tool/mcp) | Connect to MCP servers and expose MCP tools to an agent. |
| [`example/tool/skill`](example/tool/skill) | Load local `SKILL.md` resources as agent tools. |
| [`example/tool/task`](example/tool/task) | Wrap task-style work as tools. |
| [`example/workspace/local`](example/workspace/local) | Provide local workspace files for AI-assisted tool workflows. |
| [`example/workspace/docker`](example/workspace/docker) | Run workspace tools inside a Docker container and send the result through a ChatModel. |

Start with the examples if you want the quickest path to a working agent.

## Documentation

- English docs: [`docs/en/intro.md`](docs/en/intro.md)
- Chinese docs: [`docs/zh/intro.md`](docs/zh/intro.md)
- Example index: [`example/README.md`](example/README.md)

## Development

Bootstrap the local toolchain:

```bash
make setup
make install-tools
```

Common targets:

| Command | Purpose |
| --- | --- |
| `make fmt` | Format Go code. |
| `make lint-go` | Run `golangci-lint`. |
| `make test` | Run the full Go test suite with the race detector. |
| `make coverage` | Generate full coverage output. |
| `make docs-check` | Build and lint documentation. |
| `make security-check` | Run the local secret scan. |
| `.venv/bin/pre-commit run --all-files` | Run the same local checks used before commit. |

Release automation is intentionally not documented here. Follow the repository
maintainer's release instructions when a release is planned.

## Security

Read [`SECURITY.md`](SECURITY.md) before reporting a vulnerability. A Chinese
version is available at [`SECURITY_zh.md`](SECURITY_zh.md). AgentScope Go is a
library framework, not a hosted service; application authors control which
tools, model providers, workspaces, MCP servers, and credentials are available
at runtime.

## Contributing

Contributions are welcome. Read [`CONTRIBUTING.md`](CONTRIBUTING.md) before
opening a pull request, and follow the project
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md). Chinese versions are available at
[`CONTRIBUTING_zh.md`](CONTRIBUTING_zh.md) and
[`CODE_OF_CONDUCT_zh.md`](CODE_OF_CONDUCT_zh.md).

## Contributors

<p align="center">
  <a href="https://github.com/yuluo-yx/agentscope-go/graphs/contributors">
    <img src=".github/CONTRIBUTORS.svg" alt="AgentScope Go contributors">
  </a>
</p>

## License

AgentScope Go is licensed under the [Apache License 2.0](LICENSE).
