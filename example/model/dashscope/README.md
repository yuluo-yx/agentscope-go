# DashScope ChatModel Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example covers the current DashScope capability boundary in the Go implementation:

- Construct an OpenAI-compatible Chat Completions model through `model/dashscope`.
- Use `qwen3.7-max` by default, or override it with `AI_DASHSCOPE_MODEL`.
- Configure both non-streaming and streaming model instances.
- Generate a Function Calling tool schema.
- Run a live Function Calling round for `GetWeather` and send the tool result back to the model.
- Run a live `ChatModel.Stream` call and print streamed deltas.
- Build a text + image URL data-block input and run local token estimation.
- Optionally make a real text call with `AI_DASHSCOPE_API_KEY`.

According to the official Alibaba Cloud documentation, Model Studio text generation offers OpenAI-compatible Chat Completions. This Go provider example demonstrates the implemented OpenAI-compatible ChatModel path.

## Prerequisites

- Go 1.26.3.
- The default offline run does not require an API key.
- A live call requires `AI_DASHSCOPE_API_KEY`; without it, the example runs the offline path.

## Run

Offline run:

```bash
cd example/model/dashscope
go run .
```

Live call:

```bash
cd example/model/dashscope
AI_DASHSCOPE_API_KEY=your-key go run .
```

Choose a model:

```bash
AI_DASHSCOPE_MODEL=qwen3.6-plus AI_DASHSCOPE_API_KEY=your-key go run .
```

## Expected Output

Offline output includes:

```text
chat_model=dashscope:
dashscope_live=skipped
```

Live success output includes:

```text
chat_model=dashscope:
dashscope_live=ok
dashscope_weather=ok
dashscope_stream=ok
```

## Official References

- API reference: https://help.aliyun.com/zh/model-studio/model-api-reference/
- Text generation: https://help.aliyun.com/zh/model-studio/qwen-api-reference/
- Model list: https://help.aliyun.com/zh/model-studio/models
