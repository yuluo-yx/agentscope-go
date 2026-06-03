# AgentScope Go 与 Gin 集成

项目主页：[README-zh.md](../../../README-zh.md)。

英文文档：[README.md](README.md)。

## Run

```shell
go run .
```

Gin 默认监听 `127.0.0.1:8080`。需要先配置 DashScope AK：

```shell
export AI_DASHSCOPE_API_KEY=your-dashscope-api-key
```

## Curl

```shell
curl -v '127.0.0.1:8080/ping'
curl -v '127.0.0.1:8080/chat?prompt=hello'
curl -N '127.0.0.1:8080/stream-chat?prompt=hello'
curl -N '127.0.0.1:8080/stream-chat-tool?prompt=杭州天气怎么样？'
curl -v '127.0.0.1:8080/agent/chat'
curl -N '127.0.0.1:8080/agent/stream-chat'
curl -v '127.0.0.1:8080/structured?prompt=杭州一日游'
```

## 说明

- `/stream-chat-tool` 演示 ChatModel 的底层工具调用流程：模型先决定是否调用工具，应用执行工具，再把工具结果交回模型生成最终回答。
- `/agent/chat` 和 `/agent/stream-chat` 演示 Agent 自动执行已允许的只读工具。
- `/structured` 演示结构化输出：让模型返回紧凑 JSON，并在 Go 代码中解析为结构化对象。
