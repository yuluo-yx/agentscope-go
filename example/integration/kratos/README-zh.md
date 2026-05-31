# AgentScope Go 与 Kratos 集成

英文文档：[README.md](README.md)。

该示例用 Kratos 项目结构暴露与 Gin 示例一致的 AgentScope Go 流程：

- `api/chat/v1/chat.proto` 定义 gRPC service 和 HTTP annotation。
- `internal/biz` 负责 AgentScope Go 的 ChatModel、tool、Agent 和结构化输出编排。
- `internal/service` 将 biz 层结果适配为 protobuf message。
- `internal/server` 注册生成的 gRPC/HTTP handler，并为 HTTP 流式接口注册 SSE 路由。

## 前置条件

- Go 1.26.3 或更高版本。
- 配置 `AI_DASHSCOPE_API_KEY` 以调用 DashScope。

```shell
export AI_DASHSCOPE_API_KEY=your-dashscope-api-key
```

## Run

运行简化入口：

```shell
go run .
```

服务监听 `127.0.0.1:8000`。

运行 Kratos 应用入口，同时启动 HTTP 和 gRPC：

```shell
go run ./cmd/kratos -conf ./configs
```

HTTP 监听 `127.0.0.1:8000`，gRPC 监听 `127.0.0.1:9000`。

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

修改 `api/chat/v1/chat.proto` 后使用以下命令重新生成 protobuf 代码：

```shell
PATH="$(go env GOPATH)/bin:$PATH" protoc \
  --proto_path=. \
  --proto_path=third_party \
  --go_out=paths=source_relative:. \
  --go-grpc_out=paths=source_relative:. \
  --go-http_out=paths=source_relative:. \
  api/chat/v1/chat.proto
```

需要先安装生成器：

```shell
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
go install github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@latest
```

Kratos HTTP 生成器只会注册 unary RPC 的 HTTP handler。三个 HTTP 流式接口使用 `internal/server/http.go` 中的 SSE 路由注册，仍然调用同一组 `ChatService` 方法，与 gRPC 流式接口共享 service/biz 层逻辑。
