# Agent Configuration Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows common Agent configuration points:

- `WithModelConfig` sets retry count and a fallback model.
- `WithContextConfig` controls local tool-result truncation during context cleanup.
- `WithReActConfig` sets the maximum reasoning/action loop count.

The primary failing ChatModel intentionally returns an error. The Agent retries according to `ModelConfig`, then uses a DashScope fallback model. The example also seeds a long tool result in state so context cleanup truncates it before the model call.

## Prerequisites

- Go 1.26.3 or newer.
- `AI_DASHSCOPE_API_KEY` for the fallback DashScope ChatModel.

## Run

```bash
cd example/agent/configuration
go run .
```

Expected output:

```text
reply=...
primary_stream_calls=1
fallback_model=dashscope/qwen3.7-max
compressed=true
```

## Test

```bash
go test .
```
