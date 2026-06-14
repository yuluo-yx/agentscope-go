# Anthropic ChatModel Example

Project home: [README.md](../../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows live Anthropic `ChatModel` usage:

- Construct an Anthropic Messages model through `model/anthropic`.
- Use `claude-sonnet-4-5` by default, or override it with `AI_ANTHROPIC_MODEL`.
- Use the SDK default Anthropic API base URL, or override it with `AI_ANTHROPIC_BASE_URL`.
- Run a local token estimate without making a network request.
- When an API key is set, run a real non-streaming `ChatModel.Call`.
- When an API key is set, run a real streaming `ChatModel.Stream` and print streamed deltas.

## Prerequisites

- Go 1.26.3.
- The default offline run does not require an API key.
- A live call requires `AI_ANTHROPIC_API_KEY`, `ANTHROPIC_API_KEY`, or `AI_API_KEY`.

## Run

Offline run:

```bash
cd example/model/anthropic/chat
go run .
```

Live call:

```bash
cd example/model/anthropic/chat
AI_ANTHROPIC_API_KEY=your-key go run .
```

Choose a model:

```bash
AI_ANTHROPIC_MODEL=claude-sonnet-4-5 AI_ANTHROPIC_API_KEY=your-key go run .
```

Override the base URL:

```bash
AI_ANTHROPIC_BASE_URL=https://api.anthropic.com AI_ANTHROPIC_API_KEY=your-key go run .
```

## Expected Output

Offline output includes:

```text
anthropic_live=skipped
```

Live success output includes:

```text
anthropic_live=ok
anthropic_stream=ok
```
