# AgentScope Go with Kratos

Project home: [README.md](../../../README.md).

Chinese docs: [README-zh.md](README-zh.md).

This example exposes the same AgentScope Go flows as the Gin example through a Kratos project layout:

- `api/chat/v1/chat.proto` defines the gRPC service and HTTP annotations.
- `internal/biz` owns AgentScope Go ChatModel, tool, Agent, and structured-output orchestration.
- `internal/service` adapts the biz layer to protobuf messages.
- `internal/server` registers generated gRPC/HTTP handlers and manual SSE endpoints for HTTP streaming.

## Prerequisites

- Go 1.26.3 or later.
- `AI_DASHSCOPE_API_KEY` for live DashScope calls.

```shell
export AI_DASHSCOPE_API_KEY=your-dashscope-api-key
```

## Run

Run the Kratos app entry with both HTTP and gRPC servers:

```shell
go run ./cmd/kratos -conf ./configs
```

HTTP listens on `127.0.0.1:8000`, and gRPC listens on `127.0.0.1:9000`.

## HTTP Curl

```shell
curl -v '127.0.0.1:8000/ping'
curl -v '127.0.0.1:8000/chat?prompt=hello'
curl -N '127.0.0.1:8000/stream-chat?prompt=hello'
curl -N '127.0.0.1:8000/stream-chat-tool?prompt=杭州天气怎么样？'
curl -v '127.0.0.1:8000/agent/chat'
curl -N '127.0.0.1:8000/agent/stream-chat'
curl -v '127.0.0.1:8000/structured?prompt=杭州一日游'
```

## Proto

Regenerate protobuf code after editing `api/chat/v1/chat.proto`:

```shell
PATH="$(go env GOPATH)/bin:$PATH" protoc \
  --proto_path=. \
  --proto_path=third_party \
  --go_out=paths=source_relative:. \
  --go-grpc_out=paths=source_relative:. \
  --go-http_out=paths=source_relative:. \
  api/chat/v1/chat.proto
```

Required generators:

```shell
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
go install github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@latest
```

The Kratos HTTP generator only registers unary RPC HTTP handlers. The three HTTP streaming endpoints are SSE routes registered in `internal/server/http.go` and still call the same `ChatService` methods as gRPC streaming.
