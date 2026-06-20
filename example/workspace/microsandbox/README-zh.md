# Microsandbox Workspace 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示 `workspace/microsandbox.Workspace`：

- 使用 Python 镜像启动 Microsandbox workspace。
- 通过 workspace `Write` 和 `Read` 工具操作 `/workspace` 下的文件。
- 使用临时宿主机 mirror 目录保存 offload、Skill 和 MCP 索引。
- 如果提供 DashScope API Key，则把 workspace 工具结果交给 DashScope ChatModel 总结。

## 前置条件

- Go 1.26.4。
- Linux 且启用 KVM，或 Apple Silicon macOS。
- Microsandbox runtime 能正常运行。首次初始化时，如果运行时资产不存在，Go SDK 会下载到 `~/.microsandbox/`。
- 可选 DashScope API Key，用于真实 ChatModel 调用。

默认镜像是 `python:3.12`。

## 运行

```bash
cd example/workspace/microsandbox
go run .
```

使用其他镜像：

```bash
AGENTSCOPE_MICROSANDBOX_IMAGE=python:3.13 go run .
```

使用固定 sandbox 名称：

```bash
AGENTSCOPE_MICROSANDBOX_NAME=agentscope-msb-demo go run .
```

运行真实 DashScope ChatModel 路径：

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

## 预期输出

未设置 API Key 时，输出包含：

```text
microsandbox_workspace_alive=true
read_has_brief=true
dashscope_live=skipped
```

设置 API Key 后，输出还会包含：

```text
dashscope_live=ok
```
