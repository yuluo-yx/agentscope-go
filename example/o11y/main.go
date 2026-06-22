package main

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	asagent "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/middleware"
	oteltracer "github.com/yuluo-yx/agentscope-go/pkg/middleware/otel"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
)

func initTracer() func(context.Context) {

	// 1. stdout exporter 导出
	expr, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		panic(err)
	}

	// 2. withSyncer 同步写 exporter
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(expr))

	// 3. 设置为全局的 provider
	otel.SetTracerProvider(tp)

	return func(_ context.Context) {
		if err := tp.Shutdown(context.Background()); err != nil {
			_, err := fmt.Fprintf(os.Stderr, "tracer shutdown: %v\n\n", err)
			if err != nil {
				return
			}
		}
	}
}

func main() {

	cleanup := initTracer()
	defer cleanup(context.Background())

	// chat
	chat()

	fmt.Println("==================")

	// chat with agent
	agent()
}

func chat() {

	ctx := context.Background()

	model, err := dashscope.NewChatModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).ChatCredential(),
		"qwen3.7-max",
	)
	if err != nil {
		panic(err)
	}

	user, err := message.NewUserMessage("user", "你好，介绍你自己。")
	if err != nil {
		panic(err)
	}
	streamResp, err := model.Stream(
		ctx,
		asmodel.CallRequest{
			Messages: []*message.Message{user},
			Stream:   true,
		},
	)
	if err != nil {
		panic(err)
	}
	for response := range streamResp {
		if text := response.GetTextContent(); text != nil {
			println(*text)
		}
	}

}

func agent() {

	ctx := context.Background()

	demoAgent, err := asagent.NewAgent(
		"DemoAgent",
		"Hi, you is helpful assistant.",
		func() asmodel.ChatModel {
			model, err := dashscope.NewChatModel(credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).ChatCredential(), "qwen3.7-max")
			if err != nil {
				panic(err)
			}
			return model
		}(),
		asagent.WithMiddlewares(
			func() asagent.Middleware {
				return middleware.NewTracingMiddleware(
					// ot 中间件会自己获取当前的全局 provider
					oteltracer.NewTracer(nil),
				)
			}(),
		),
	)

	if err != nil {
		panic(err)
	}

	if err := demoAgent.ReplyStream(
		ctx,
		func() *message.Message {
			user, err := message.NewUserMessage("user", "hello, 介绍下牛顿力学")
			if err != nil {
				panic(err)
			}
			return user
		}(),
		func(ee message.Event) error {
			switch e := ee.(type) {
			case *message.TextBlockDeltaEvent:
				println(e.Delta)
			}
			return nil
		},
	); err != nil {
		panic(err)
	}
}
