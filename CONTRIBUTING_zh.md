# 贡献指南

感谢你关注 agentscope-go。本文介绍如何搭建本地开发环境、配置代码检查工具、提交代码和发起 Pull Request。

## 环境要求

开始开发前，请确认本机已安装下列工具：

| 工具 | 最低版本 | 用途 |
| --- | --- | --- |
| Go | 1.25.8 | 编译和测试 |
| Python | 3.13 | pre-commit 框架和 codespell 的运行时 |
| Node.js | 22 | markdownlint 的运行时 |
| npm | 10 | 安装 markdownlint 依赖 |
| Git | 2.30+ | 版本管理 |

推荐使用 [mise](https://github.com/jdx/mise) 或类似工具管理多版本运行时。

## 快速开始

### 克隆仓库

```bash
git clone https://github.com/yuluo-yx/agentscope-go.git
cd agentscope-go
```

### 一键初始化

项目提供 `make setup` 命令完成全部本地配置：

```bash
make setup
```

该命令依次执行以下操作：

1. 创建项目级 Python 虚拟环境（`.venv/`）。
2. 安装 pre-commit 框架（v4.0.1）到虚拟环境。
3. 将 Git hooks 路径设置为 `.githook/`。

执行完成后，pre-commit hooks 会在每次 `git commit` 时自动运行。

### 安装全部开发工具

如果需要手动运行各项检查，可以安装全部工具：

```bash
make install-tools
```

该命令安装以下工具：

| 工具 | 版本 | 说明 |
| --- | --- | --- |
| pre-commit | 4.0.1 | Hook 管理框架 |
| golangci-lint | v2.11.4 | Go 静态检查 |
| govulncheck | v1.3.0 | Go 漏洞扫描 |
| gitleaks | v8.30.1 | 密钥泄露检测 |
| codespell | 2.4.2 | 拼写检查 |
| markdownlint-cli | latest | Markdown 格式检查 |

## Pre-commit Hooks

### Hook 列表

项目配置了以下 pre-commit hooks：

| Hook | 来源 | 说明 |
| --- | --- | --- |
| trailing-whitespace | pre-commit-hooks | 删除行尾空格 |
| end-of-file-fixer | pre-commit-hooks | 确保文件以换行结尾 |
| check-yaml | pre-commit-hooks | 校验 YAML 语法 |
| check-json | pre-commit-hooks | 校验 JSON 语法 |
| check-merge-conflict | pre-commit-hooks | 检查未解决的合并冲突标记 |
| golangci-lint | golangci-lint | Go 代码静态检查 |
| codespell | codespell | 拼写检查 |
| gitleaks | gitleaks | 扫描提交中的密钥和凭据 |
| go-mod-tidy | local | 自动执行 `go mod tidy` |
| markdownlint | local | Markdown 格式检查 |
| govulncheck | local | Go 依赖漏洞扫描 |

### 手动运行

对所有文件运行全部 hooks：

```bash
.venv/bin/pre-commit run --all-files
```

对暂存区文件运行（默认行为，`git commit` 时自动触发）：

```bash
.venv/bin/pre-commit run
```

运行单个 hook：

```bash
.venv/bin/pre-commit run golangci-lint --all-files
.venv/bin/pre-commit run codespell --all-files
```

### 首次运行说明

首次运行 pre-commit 时，框架会自动下载各 hook 所需的运行环境。这个过程可能需要几分钟，后续运行会使用缓存。

如果项目中尚无 `.go` 源文件，`govulncheck` 会报告 "no packages matched" 错误。这是预期行为，添加 Go 源文件后会自动恢复正常。

## 开发工作流

### 分支管理

1. 从 `main` 分支创建功能分支：

```bash
git checkout -b feat/your-feature main
```

2. 分支命名建议使用 `feat/`、`fix/`、`refactor/`、`docs/` 等前缀，后接简短描述。

### 编码与测试

编写代码后，运行以下命令进行本地检查：

```bash
# 格式化代码
make fmt

# 静态检查
make vet

# 运行测试
make test

# 运行全部 CI 对齐检查（格式、lint、拼写、安全、测试）
make ci
```

### 常用 Make 命令

| 命令 | 说明 |
| --- | --- |
| `make help` | 显示所有可用命令 |
| `make fmt` | 格式化 Go 代码 |
| `make fmt-check` | 检查格式是否符合规范（不修改文件） |
| `make vet` | 运行 `go vet` |
| `make test` | 运行测试（含竞态检测） |
| `make test-unit` | 运行单元测试（排除 E2E 测试） |
| `make lint` | 运行拼写和 Markdown 检查 |
| `make lint-go` | 运行 golangci-lint |
| `make ci` | 运行与 CI 一致的完整检查流程 |
| `make coverage` | 生成测试覆盖率报告 |
| `make clean` | 清理构建产物 |

### 提交代码

pre-commit hooks 会在每次 `git commit` 时自动运行。如果检查失败，提交会被阻止，请根据输出修复问题后重新提交。

commit message 遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范，使用祈使句，首行不超过 72 个字符。格式如下：

```text
<type>(<scope>): <subject>

<body>

<footer>
```

其中 `<type>` 必须是以下值之一：

| 类型 | 说明 |
| --- | --- |
| `feat` | 新功能 |
| `fix` | 修复缺陷 |
| `refactor` | 重构（不改变外部行为） |
| `test` | 添加或修改测试 |
| `docs` | 文档变更 |
| `chore` | 构建、工具链等杂项 |
| `perf` | 性能优化 |
| `style` | 代码格式调整（不改变逻辑） |
| `ci` | CI/CD 配置变更 |

示例：

```text
feat(client): add retry logic for HTTP requests
```

## Pull Request

### 提交前检查

在发起 Pull Request 前，请确认：

1. 本地测试全部通过（`make test`）。
2. CI 对齐检查全部通过（`make ci`）。
3. 每个 PR 只包含一个逻辑变更。
4. 如果涉及行为变更，已更新相关文档或注释。

### PR 标题格式

PR 标题同样遵循 Conventional Commits 规范。CI 会自动校验标题格式，不符合规范的 PR 会被阻止合并。

标题要求：

- 使用上述规定的 `<type>` 前缀。
- 主题部分（subject）以小写字母开头。
- 不强制要求 scope。

### PR 描述

请使用仓库提供的 PR 模板填写以下内容：

- **Summary**：简述变更目的和解决的问题。
- **Related issue**：使用 `Closes #...` 或 `Fixes #...` 关联相关 Issue。
- **What changed**：列出具体变更项。
- **Testing performed**：说明执行的验证命令和结果。
- **Checklist**：逐项确认模板中的检查清单。

## 持续集成

项目在 GitHub Actions 上运行以下 CI 流水线：

| 流水线 | 触发条件 | 说明 |
| --- | --- | --- |
| CI（ci.yml） | Go 相关文件变更 | 运行 `make ci`，包含格式检查、lint、拼写、安全扫描和测试 |
| Markdown Linter（linter.yml） | Markdown 文件变更 | 运行 markdownlint 检查文档格式 |
| PR Title（pr-title.yml） | PR 创建或更新 | 校验 PR 标题是否符合 Conventional Commits |
| CodeQL（codeql.yml） | Go 相关文件变更或每周定时 | GitHub CodeQL 安全分析 |

所有 CI 检查通过后，PR 才能合并。

## 代码规范

### Go 代码

- 格式化使用 `gofmt` 和 `goimports`，保持导入排序。
- 遵循项目 `.golangci.yml` 中启用的 linter 规则，包括 `errcheck`、`govet`、`staticcheck`、`gosec` 等。
- 错误处理必须携带上下文，使用 `fmt.Errorf("...: %w", err)` 保留错误链。
- 新增 Go 文件需要包含 Apache 2.0 许可证头，golangci-lint 的 `goheader` 规则会自动检查。

### 文档

- Markdown 文件使用 markdownlint 检查格式，配置文件位于 `tools/linter/markdownlint/markdown_lint_config.yml`。
- 拼写检查使用 codespell，自定义忽略词表位于 `tools/linter/codespell/.codespell.ignorewords`。

## 常见问题

### pre-commit 提示 "pre-commit is not installed in .venv"

运行 `make setup` 重新安装 pre-commit 框架。

### golangci-lint 版本不匹配

运行 `make install-golangcilint` 安装项目指定版本（v2.11.4）。

### markdownlint 报错

运行 `make install-markdownlint` 安装 npm 依赖。如果 `tools/node/node_modules/` 不存在，该命令会自动创建。

### govulncheck 报告 "no packages matched"

项目中尚无 `.go` 源文件时会出现此错误，属于预期行为。添加 Go 源文件后自动恢复。

### 跳过某个 hook（不推荐）

如果确实需要临时跳过某个 hook，可以使用 `--no-verify` 参数：

```bash
git commit --no-verify -m "your message"
```

请仅在紧急情况下使用，并在后续提交中补回被跳过的检查。
