# DashScope ChatModel 示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例覆盖当前 Go 实现中的 DashScope 能力边界：

- 通过 `model/dashscope` 构造 OpenAI-compatible Chat Completions 模型。
- 默认模型为 `qwen3.7-max`，可用 `AI_DASHSCOPE_MODEL` 覆盖。
- 配置非流式与流式模型实例。
- 生成 Function Calling 工具 schema。
- 真实运行 `GetWeather` Function Calling 回合，并把工具结果回填给模型。
- 真实运行 `ChatModel.Stream` 并输出流式增量。
- 构造文本 + 图片 URL 的数据块输入并做本地 token 估算。
- 可选使用 `AI_DASHSCOPE_API_KEY` 发起真实文本调用。

根据阿里云官方文档，百炼文本生成提供 OpenAI 兼容 Chat Completions。本 Go provider 示例演示已实现的 OpenAI-compatible ChatModel 路径。

## 前置条件

- Go 1.26.3。
- 默认离线运行不需要 API Key。
- 真实调用需要设置 `AI_DASHSCOPE_API_KEY`；未设置时示例走离线路径。

## 运行

离线运行：

```bash
cd example/model/dashscope
go run .
```

真实调用：

```bash
cd example/model/dashscope
AI_DASHSCOPE_API_KEY=your-key go run .
```

指定模型：

```bash
AI_DASHSCOPE_MODEL=qwen3.6-plus AI_DASHSCOPE_API_KEY=your-key go run .
```

## 预期输出

离线输出包含：

```text
chat_model=dashscope:
dashscope_live=skipped
```

真实调用成功时输出包含：

```text
chat_model=dashscope:
dashscope_live=ok
dashscope_weather=ok
dashscope_stream=ok
```

## 官方参考

- API 参考：https://help.aliyun.com/zh/model-studio/model-api-reference/
- 文本生成：https://help.aliyun.com/zh/model-studio/qwen-api-reference/
- 模型列表：https://help.aliyun.com/zh/model-studio/models
