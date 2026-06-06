# Embedding Basic Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows how to call text embedding through the unified `EmbeddingModel` interface and cache repeated requests with `FileCache`.

The default run uses a local mock provider. It does not require an API key and does not access external services. Set `EMBEDDING_PROVIDER` to switch to a real provider.

## Prerequisites

- Go 1.26.3.
- No external service is required in the default mode.
- Real providers are called only when their environment variables are set explicitly.

## Run Local Mock

```bash
cd example/embedding/basic
go run .
```

Expected output includes:

```text
provider=mock
second_source=cache
```

## Run Real Providers

OpenAI:

```bash
EMBEDDING_PROVIDER=openai \
OPENAI_API_KEY=sk-... \
go run .
```

Gemini:

```bash
EMBEDDING_PROVIDER=gemini \
GEMINI_API_KEY=... \
go run .
```

Ollama:

```bash
EMBEDDING_PROVIDER=ollama \
OLLAMA_HOST=http://localhost:11434 \
OLLAMA_EMBEDDING_MODEL=nomic-embed-text \
go run .
```

DashScope text embedding:

```bash
EMBEDDING_PROVIDER=dashscope \
DASHSCOPE_API_KEY=sk-... \
go run .
```

## Common Environment Variables

- `EMBEDDING_PROVIDER`: `mock`, `openai`, `gemini`, `ollama`, or `dashscope`. Defaults to `mock`.
- `EMBEDDING_CACHE_DIR`: cache directory. Defaults to `.cache/embeddings`.
- `EMBEDDING_EXAMPLE_TEXT`: example input text.
- `OPENAI_BASE_URL`: OpenAI-compatible endpoint.
- `DASHSCOPE_BASE_URL`: DashScope endpoint.
- `*_EMBEDDING_MODEL`: override the embedding model name.
- `*_EMBEDDING_DIMENSIONS`: override output vector dimensions.
