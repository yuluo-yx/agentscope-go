# Embedding 基础示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示如何通过统一的 `EmbeddingModel` 接口调用文本 embedding，并使用 `FileCache` 缓存同一请求的向量结果。

默认运行使用本地 mock provider，不需要 API Key，也不会访问外部服务。设置 `EMBEDDING_PROVIDER` 后可切换到真实 provider。

## 前置条件

- Go 1.26.3。
- 默认模式不需要外部服务。
- 真实 provider 只在显式设置环境变量后调用。

## 运行本地 mock

```bash
cd example/embedding/basic
go run .
```

预期输出包含：

```text
provider=mock
second_source=cache
```

## 运行真实 Provider

OpenAI：

```bash
EMBEDDING_PROVIDER=openai \
OPENAI_API_KEY=sk-... \
go run .
```

Gemini：

```bash
EMBEDDING_PROVIDER=gemini \
GEMINI_API_KEY=... \
go run .
```

Ollama：

```bash
EMBEDDING_PROVIDER=ollama \
OLLAMA_HOST=http://localhost:11434 \
OLLAMA_EMBEDDING_MODEL=nomic-embed-text \
go run .
```

DashScope 文本 embedding：

```bash
EMBEDDING_PROVIDER=dashscope \
DASHSCOPE_API_KEY=sk-... \
go run .
```

## 常用环境变量

- `EMBEDDING_PROVIDER`：`mock`、`openai`、`gemini`、`ollama` 或 `dashscope`，默认 `mock`。
- `EMBEDDING_CACHE_DIR`：缓存目录，默认 `.cache/embeddings`。
- `EMBEDDING_EXAMPLE_TEXT`：示例文本。
- `OPENAI_BASE_URL`：OpenAI-compatible endpoint。
- `DASHSCOPE_BASE_URL`：DashScope endpoint。
- `*_EMBEDDING_MODEL`：覆盖 provider 的 embedding 模型名。
- `*_EMBEDDING_DIMENSIONS`：覆盖输出向量维度。
