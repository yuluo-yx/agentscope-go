# Provider Construction Example

Project home: [README.md](../../../README.md).

Chinese documentation: [README-zh.md](README-zh.md).

This example shows how to construct the implemented ChatModel providers:

- OpenAI.
- Anthropic.
- DeepSeek.
- DashScope.
- Gemini.
- Moonshot.
- xAI.
- Zhipu AI.
- Ollama.

It only constructs models and runs local token estimation for each provider. It does not make live network calls.

## Prerequisites

- Go 1.26.3.
- No API key is required.
- A local Ollama server is not required because the example does not call the model.

## Run

```bash
cd example/model/providers
go run .
```

## Expected Output

Output includes:

```text
providers=9
```
