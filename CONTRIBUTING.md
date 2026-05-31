# Contributing to AgentScope Go

Chinese version: [`CONTRIBUTING_zh.md`](CONTRIBUTING_zh.md).

Thank you for helping improve AgentScope Go. This guide explains how to set up
the repository, make focused changes, and submit contributions that are easy to
review.

## Before You Start

- Check existing issues and pull requests before starting a large change.
- Keep pull requests focused on one behavior, package, or documentation topic.
- Do not document unsupported APIs as available features.
- Do not add release automation or version bump workflows unless the maintainer
  explicitly requests them.
- If you use AI assistance, you are still responsible for reviewing, testing,
  and explaining the final change.

## Development Environment

Required tools:

| Tool | Version |
| --- | --- |
| Go | `1.26.3+` |
| GNU Make | Any recent version |
| Python | `3.13+`, for local lint tooling |
| Node.js | `22+`, for documentation tooling |
| npm | `10+`, for markdown tooling |

Bootstrap the repository:

```bash
make setup
make install-tools
```

The setup target configures the local Git hooks path and prepares the toolchain
used by the Makefile targets. The install target installs the pinned local
linters used by the project.

## Common Commands

| Command | Purpose |
| --- | --- |
| `make download` | Download Go module dependencies. |
| `make tidy` | Run `go mod tidy`. |
| `make fmt` | Format Go code. |
| `make fmt-check` | Verify Go formatting without modifying files. |
| `make vet` | Run `go vet ./...`. |
| `make lint-go` | Run `golangci-lint`. |
| `make test` | Run `go test ./... -v -race`. |
| `make test-unit` | Run package tests without the race detector. |
| `make coverage` | Generate coverage output. |
| `make docs-check` | Build and lint documentation. |
| `make security-check` | Run the local gitleaks secret scan. |
| `.venv/bin/pre-commit run --all-files` | Run all configured pre-commit hooks. |

`govulncheck` is available through `make govulncheck` as an explicit local
security check. It is not part of the default pre-commit path.

## Repository Conventions

- Keep public APIs small, explicit, and Go idiomatic.
- Match the existing package boundaries before introducing a new package.
- Keep Go comments and public API documentation in English.
- Put Chinese user-facing documentation in `README-zh.md`, `docs/zh/`, or other
  `*-zh.md` files.
- Use the project license header format:

```go
// Copyright The AgentScope Go Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
```

- Prefer standard Go libraries and mature ecosystem libraries over custom
  implementations.
- Wrap errors with enough context for callers to understand the failing package,
  operation, and external dependency.
- Keep examples independent and runnable from their own directories.
- Never commit API keys, model credentials, generated secrets, or private MCP
  server configuration.
- Contributor SVG updates are handled by
  `.github/workflows/update-contributors.yml`; do not update release automation
  when changing the contributor list.

## Testing Expectations

For code changes, run:

```bash
make fmt
make lint-go
make test
```

For documentation-only changes, run:

```bash
make docs-check
.venv/bin/pre-commit run --all-files
```

Provider examples that require live model access should use environment
variables such as `AI_DASHSCOPE_API_KEY`. Offline tests and examples must still
give a useful result when those environment variables are absent.

## Documentation Expectations

- Update README files when a change affects the first-run developer experience.
- Update `docs/en/` and `docs/zh/` together for user-facing documentation.
- Update example READMEs when changing example behavior or prerequisites.
- Keep feature descriptions aligned with implemented Go packages.
- Mention required environment variables and external services explicitly.

## Pull Request Checklist

Before opening a pull request, confirm that:

- The change has a clear motivation and scope.
- Code, tests, examples, and documentation are updated together when needed.
- Relevant local checks pass.
- Any skipped checks are explained in the pull request.
- Security-sensitive behavior, such as tool execution or file access, is called
  out clearly.

Use the repository pull request template and keep the title compatible with the
project PR title check.

## Reporting Security Issues

Do not open a public issue for a vulnerability. Follow
[`SECURITY.md`](SECURITY.md) for private reporting instructions. The Chinese
version is [`SECURITY_zh.md`](SECURITY_zh.md).

## Code of Conduct

All project spaces follow [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md). The
Chinese version is [`CODE_OF_CONDUCT_zh.md`](CODE_OF_CONDUCT_zh.md).
