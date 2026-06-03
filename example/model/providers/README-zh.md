# Provider 构造示例

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

本示例展示当前已实现的 ChatModel provider 如何构造：

- OpenAI。
- Anthropic。
- DeepSeek。
- DashScope。
- Moonshot。
- xAI。
- Ollama。

示例只构造模型并做本地 token 估算，不发起真实网络请求。

## 前置条件

- Go 1.26.3。
- 不需要 API Key。
- 不需要本地 Ollama 服务，因为示例不调用模型。

## 运行

```bash
cd example/model/providers
go run .
```

## 预期输出

输出包含：

```text
providers=7
```
