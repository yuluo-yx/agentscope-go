# Docker Workspace 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示 `workspace/docker.Workspace`：

- 使用 Ubuntu 镜像启动 Docker workspace。
- 将临时宿主机 workspace 目录 bind mount 到容器内。
- 使用 workspace `Write` 和 `Read` 工具操作 `/workspace` 下的容器路径。
- 基于 Docker workspace instructions 和工具读取结果构造 system、user、assistant 消息。
- 在存在 DashScope API Key 时，把这些消息发送给 DashScope ChatModel。

## 前置条件

- Go 1.26.3。
- Docker daemon 已启动，当前用户有 Docker 访问权限。
- 本地已有 Ubuntu 镜像。示例默认使用 `ubuntu:latest`。
- 可选：DashScope API Key，用于真实 ChatModel 调用。

Docker workspace 使用的镜像需要包含 `/bin/bash`、`sleep`、`find` 和 `grep`。

## 运行

```bash
cd example/workspace/docker
go run .
```

使用其他本地镜像 tag：

```bash
AGENTSCOPE_DOCKER_IMAGE=ubuntu:24.04 go run .
```

运行真实 ChatModel 路径：

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

## 预期输出

没有 API Key 时，输出包含：

```text
docker_workspace_alive=true
read_has_brief=true
dashscope_live=skipped
```

有 API Key 时，输出还包含：

```text
dashscope_live=ok
```
