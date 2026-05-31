# 安装

AgentScope Go 是标准 Go Module。你可以在自己的项目中通过 `go get` 引入。

## 环境要求

- Go 1.26.3 或更高版本。
- 下载 Go Module 时需要网络访问。
- 运行真实模型调用时，需要对应模型服务的 API Key。

## 添加依赖

```bash
go mod init example.com/my-agent
go get github.com/yuluo-yx/agentscope-go
```

## 配置模型 Key

DashScope 示例默认使用 `AI_DASHSCOPE_API_KEY`：

```bash
export AI_DASHSCOPE_API_KEY="your-key"
```

可以通过环境变量覆盖示例中的 DashScope 模型：

```bash
export AI_DASHSCOPE_MODEL="qwen3.7-max"
```

## 验证仓库

在本仓库内开发时运行：

```bash
go test ./...
```

仓库也提供 Makefile 目标：

```bash
make test
make lint
make docs-build
```

## 运行示例

每个示例目录都是独立 Go Module：

```bash
cd example/tool/mcp
go run .
```

多数模型相关示例支持离线路径。未配置 API Key 时，示例会输出工具 Schema、Token 估算或本地结果，保证目录可以直接运行。
