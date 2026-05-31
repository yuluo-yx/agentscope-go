# 贡献指南

English version: [`CONTRIBUTING.md`](CONTRIBUTING.md).

感谢你参与 AgentScope Go。本指南说明如何准备本地环境、提交聚焦的变更，并让 Review PR 更容易。

## 开始之前

- 开始较大改动前，先查看已有 issue 和 pull request。
- 每个 PR 尽量只解决一个行为、一个包或一个文档主题。
- 不要把尚未实现的 API 写成已可用功能。
- 除非维护者明确要求，不要新增 release 自动化或版本号发布流程。
- 如果使用 AI 辅助开发，贡献者仍需自行审阅、测试并解释最终变更。

## 开发环境

推荐工具版本：

| 工具 | 版本 |
| --- | --- |
| Go | `1.26.3+` |
| GNU Make | 近期版本即可 |
| Python | `3.13+`，用于本地 lint 工具 |
| Node.js | `22+`，用于文档工具 |
| npm | `10+`，用于 markdown 工具 |

初始化仓库：

```bash
make setup
make install-tools
```

`make setup` 会配置本地 Git hooks 路径并准备 Makefile 依赖的基础环境；
`make install-tools` 会安装项目固定版本的本地检查工具。

## 常用命令

| 命令 | 作用 |
| --- | --- |
| `make download` | 下载 Go module 依赖。 |
| `make tidy` | 执行 `go mod tidy`。 |
| `make fmt` | 格式化 Go 代码。 |
| `make fmt-check` | 检查 Go 格式但不改写文件。 |
| `make vet` | 执行 `go vet ./...`。 |
| `make lint-go` | 执行 `golangci-lint`。 |
| `make test` | 执行 `go test ./... -v -race`。 |
| `make test-unit` | 执行不带 race detector 的包测试。 |
| `make coverage` | 生成覆盖率结果。 |
| `make docs-check` | 构建并检查文档。 |
| `make security-check` | 执行本地 gitleaks 密钥扫描。 |
| `.venv/bin/pre-commit run --all-files` | 执行全部 pre-commit hooks。 |

`make govulncheck` 可用于主动运行 Go 依赖漏洞检查，但它不是默认 pre-commit
流程的一部分。

## 仓库约定

- 公共 API 保持小而明确，符合 Go 习惯。
- 新增包前先匹配现有包边界，避免把无关能力堆到根包。
- Go 注释和 public API 文档统一使用英文。
- 面向中文用户的文档写入 `README-zh.md`、`docs/zh/` 或其他 `*-zh.md` 文件。
- 新增 Go 文件使用项目统一 license header：

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

- 能使用标准库或成熟生态库时，不自研替代实现。
- 错误需要带足上下文，便于调用方判断失败的包、操作和外部依赖。
- 示例目录保持独立，每个示例都应能在自己的目录下运行。
- 不要提交 API Key、模型凭据、生成密钥或私有 MCP server 配置。
- 贡献者 SVG 由 `.github/workflows/update-contributors.yml` 更新。调整贡献者
  名单时，不要改动 release 自动化。

## 测试要求

代码改动建议至少运行：

```bash
make fmt
make lint-go
make test
```

仅文档改动建议运行：

```bash
make docs-check
.venv/bin/pre-commit run --all-files
```

需要在线模型访问的 Provider 示例应通过 `AI_DASHSCOPE_API_KEY` 等环境变量读取
凭据。缺少这些环境变量时，离线测试和示例仍应给出有意义的结果。

## 文档要求

- 影响首次使用体验时，同步更新 README。
- 面向用户的文档需要同步维护 `docs/en/` 和 `docs/zh/`。
- 示例行为或前置条件变化时，同步更新示例 README。
- 功能描述必须与 Go 包的实际实现保持一致。
- 明确说明所需环境变量和外部服务。

## PR 检查清单

提交 PR 前请确认：

- 变更动机和范围清楚。
- 需要时已同步更新代码、测试、示例和文档。
- 相关本地检查已通过。
- 跳过的检查已在 PR 中说明原因。
- 涉及工具执行、文件访问等安全敏感行为时，已明确写出影响。

请使用仓库 PR 模板，并保持 PR 标题符合项目的标题检查规则。

## 安全问题

请不要通过公开 issue 报告漏洞。私密报告方式见 [`SECURITY_zh.md`](SECURITY_zh.md)。

## 行为准则

项目内所有协作空间均遵守 [`CODE_OF_CONDUCT_zh.md`](CODE_OF_CONDUCT_zh.md)。
