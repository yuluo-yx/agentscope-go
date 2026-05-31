# 示例

示例位于 `example/`。每个子目录都是独立 Go Module，包含自己的 `go.mod`、`main.go`、`README.md` 和 `README-zh.md`。

## 运行示例

```bash
cd example/tool/mcp
go run .
```

## 示例矩阵

| 目录 | 用途 |
| --- | --- |
| `message` | 构造系统、用户和助手消息 |
| `model/providers` | 构造模型供应商和估算 Token |
| `model/dashscope` | DashScope 聊天、工具 Schema、数据块输入和可选真实调用 |
| `agent/basic` | 使用脚本模型和任务工具的智能体示例 |
| `tool/function` | 自定义函数工具 |
| `tool/builtin` | 内置本地工具 |
| `tool/mcp` | MCP 客户端和通过 Toolkit 执行 MCP 工具 |
| `tool/task` | 任务工具用法 |
| `tool/skill` | 加载本地 `SKILL.md` |
| `workspace/local` | 本地 Workspace 工具、Skill 和卸载能力 |

## 真实模型调用

设置 `AI_DASHSCOPE_API_KEY` 可以运行 DashScope 真实调用路径：

```bash
AI_DASHSCOPE_API_KEY=your-key go run .
```

没有 Key 时，示例会尽可能保留本地或离线路径。
