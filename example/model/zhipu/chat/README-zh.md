# 智谱 AI ChatModel 示例

项目主页：[README-zh.md](../../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示智谱 AI `ChatModel` 真实调用：

- 通过 `model/zhipu` 构造智谱 AI OpenAI-compatible Chat Completions 模型。
- 默认模型为 `glm-5.1`，可用 `AI_ZHIPU_MODEL` 覆盖。
- 默认 base URL 为 `https://open.bigmodel.cn/api/paas/v4`，可用 `AI_ZHIPU_BASE_URL` 覆盖。
- 未发起网络请求时先做本地 token 估算。
- 设置 API Key 后真实运行非流式 `ChatModel.Call`。
- 设置 API Key 后真实运行带本地 `GetWeather` 工具的 tool-call 往返。
- 设置 API Key 后真实运行流式 `ChatModel.Stream` 并输出流式增量。

## 前置条件

- Go 1.26.3。
- 默认离线运行不需要 API Key。
- 真实调用需要设置 `AI_ZHIPU_API_KEY`、`ZHIPU_API_KEY`、`ZHIPUAI_API_KEY`、`BIGMODEL_API_KEY` 或 `AI_API_KEY`。

## 运行

离线运行：

```bash
cd example/model/zhipu/chat
go run .
```

真实调用：

```bash
cd example/model/zhipu/chat
AI_ZHIPU_API_KEY=your-key go run .
```

指定模型：

```bash
AI_ZHIPU_MODEL=glm-5.1 AI_ZHIPU_API_KEY=your-key go run .
```

使用中转站或自定义 base URL：

```bash
AI_ZHIPU_BASE_URL=https://your-proxy.example.com/api/paas/v4 AI_ZHIPU_API_KEY=your-key go run .
```

## 预期输出

离线输出包含：

```text
zhipu_live=skipped
```

真实调用成功时输出包含：

```text
zhipu_live=ok
zhipu_weather=ok
zhipu_stream=ok
```
