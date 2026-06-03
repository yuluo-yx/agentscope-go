# Agent Configuration Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows common Agent configuration points:

- `WithModelConfig` sets retry count and a fallback model.
- `WithContextConfig` controls local tool-result truncation during context cleanup.
- `WithReActConfig` sets the maximum reasoning/action loop count.

The primary scripted model intentionally fails. The Agent retries according to `ModelConfig`, then uses the fallback model. The example also seeds a long tool result in state so context cleanup truncates it before the model call.

## Prerequisites

- Go 1.26.3 or newer.
- No API key is required.

## Run

```bash
cd example/agent/configuration
go run .
```

Expected output:

```text
reply=fallback model replied primary_stream_calls=1 fallback_stream_calls=1 compressed=true
```

## Test

```bash
go test .
```
