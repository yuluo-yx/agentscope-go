# AgentScope Go with Gin

Chinese docs: [README-zh.md](README-zh.md).

This example exposes AgentScope Go ChatModel, ChatModel streaming, ChatModel tool calling, Agent auto tool execution, Agent streaming events, and structured JSON output through Gin.

## Prerequisites

- Go 1.26.3 or later.
- `AI_DASHSCOPE_API_KEY` for live DashScope calls.

```shell
export AI_DASHSCOPE_API_KEY=your-dashscope-api-key
```

## Run

```shell
go run .
```

Gin listens on `127.0.0.1:8080`.

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

## What To Notice

- `/stream-chat-tool` shows the low-level ChatModel tool-call flow: the model emits a tool call, the application executes the tool, then sends the tool result back to the model for the final answer.
- `/agent/chat` and `/agent/stream-chat` show the Agent path: the Agent owns the toolkit and executes allowed read-only tools automatically.
- `/structured` asks the model for compact JSON and parses it into a Go map before returning it as JSON.
