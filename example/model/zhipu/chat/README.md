# Zhipu AI ChatModel Example

Project home: [README.md](../../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows live Zhipu AI `ChatModel` usage:

- Construct a Zhipu AI OpenAI-compatible Chat Completions model through `model/zhipu`.
- Use `glm-5.1` by default, or override it with `AI_ZHIPU_MODEL`.
- Use the default base URL `https://open.bigmodel.cn/api/paas/v4`, or override it with `AI_ZHIPU_BASE_URL`.
- Run a local token estimate without making a network request.
- When an API key is set, run a real non-streaming `ChatModel.Call`.
- When an API key is set, run a real streaming `ChatModel.Stream` and print streamed deltas.

## Prerequisites

- Go 1.26.3.
- The default offline run does not require an API key.
- A live call requires `AI_ZHIPU_API_KEY`, `ZHIPU_API_KEY`, `ZHIPUAI_API_KEY`, `BIGMODEL_API_KEY`, or `AI_API_KEY`.

## Run

Offline run:

```bash
cd example/model/zhipu/chat
go run .
```

Live call:

```bash
cd example/model/zhipu/chat
AI_ZHIPU_API_KEY=your-key go run .
```

Choose a model:

```bash
AI_ZHIPU_MODEL=glm-5.1 AI_ZHIPU_API_KEY=your-key go run .
```

Override the base URL:

```bash
AI_ZHIPU_BASE_URL=https://open.bigmodel.cn/api/paas/v4 AI_ZHIPU_API_KEY=your-key go run .
```

## Expected Output

Offline output includes:

```text
zhipu_live=skipped
```

Live success output includes:

```text
zhipu_live=ok
zhipu_stream=ok
```
