# Anthropic ChatModel 示例

项目主页：[README-zh.md](../../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示 Anthropic `ChatModel` 真实调用：

- 通过 `model/anthropic` 构造 Anthropic Messages 模型。
- 默认模型为 `claude-sonnet-4-5`，可用 `AI_ANTHROPIC_MODEL` 覆盖。
- 默认使用 SDK 内置的 Anthropic API base URL，可用 `AI_ANTHROPIC_BASE_URL` 覆盖。
- 未发起网络请求时先做本地 token 估算。
- 设置 API Key 后真实运行非流式 `ChatModel.Call`。
- 设置 API Key 后真实运行流式 `ChatModel.Stream` 并输出流式增量。

## 前置条件

- Go 1.26.3。
- 默认离线运行不需要 API Key。
- 真实调用需要设置 `AI_ANTHROPIC_API_KEY`、`ANTHROPIC_API_KEY` 或 `AI_API_KEY`。

## 运行

离线运行：

```bash
cd example/model/anthropic/chat
go run .
```

真实调用：

```bash
cd example/model/anthropic/chat
AI_ANTHROPIC_API_KEY=your-key go run .
```

指定模型：

```bash
AI_ANTHROPIC_MODEL=claude-sonnet-4-5 AI_ANTHROPIC_API_KEY=your-key go run .
```

覆盖 base URL：

```bash
AI_ANTHROPIC_BASE_URL=https://api.anthropic.com AI_ANTHROPIC_API_KEY=your-key go run .
```

## 预期输出

离线输出包含：

```text
anthropic_live=skipped
```

真实调用成功时输出包含：

```text
anthropic_live=ok
anthropic_stream=ok
```
