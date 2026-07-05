package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yuluo-yx/agentscope-go/extensions/ragparser/pdf"
	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	dashscopeembedding "github.com/yuluo-yx/agentscope-go/pkg/embedding/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/middleware"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

func main() {

	ctx := context.Background()
	kb, err := knowledge(ctx)
	if err != nil {
		panic(err)
	}

	if err := agentChat(ctx, kb); err != nil {
		panic(err)
	}
}

func agentChat(ctx context.Context, kb *rag.KnowledgeBase) error {

	question := "简单介绍下 AgentScope Go。"

	model, err := dashscope.NewChatModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).ChatCredential(),
		"glm-5.2",
		dashscope.WithStream(true),
	)
	if err != nil {
		panic(err)
	}

	ragmw := middleware.NewRAGMiddleware([]*rag.KnowledgeBase{kb}, middleware.WithRAGTopK(5))

	agent, err := agentpkg.NewAgent(
		"Example RAG Agent",
		"你是基于知识库的问答助手。当用户问题可能依赖知识库内容时，调用 search_knowledge 工具检索后再回答；若知识库无相关内容，明确告知用户",
		model,
		agentpkg.WithMiddlewares(ragmw),
	)

	userMsg, err := message.NewUserMessage("user", question)
	if err != nil {
		panic(err)
	}

	if err := agent.ReplyStream(ctx, userMsg, func(event message.Event) error {
		switch e := event.(type) {
		case *message.ToolCallStartEvent:
			fmt.Fprintf(os.Stderr, "\n[tool] 调用 %s\n", e.ToolCallName)
		case *message.ToolCallEndEvent:
			fmt.Fprintf(os.Stderr, "[tool] 调用结束 id=%s\n", e.ToolCallID)
		case *message.ToolResultEndEvent:
			fmt.Fprintf(os.Stderr, "\n[tool] 检索完成 state=%s\n", e.State)
		case *message.TextBlockDeltaEvent:
			fmt.Print(e.Delta)
		default:
			fmt.Fprintf(os.Stderr, "[event] %T\n", e)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("agent reply stream: %w", err) // 别 panic，让 main 打印
	}
	fmt.Println()

	// $ go run .
	//[event] *message.ReplyStartEvent
	//[event] *message.ModelCallStartEvent
	//
	//[tool] 调用 search_knowledge
	//[event] *message.ToolCallDeltaEvent
	//[event] *message.ToolCallDeltaEvent
	//[event] *message.ToolCallDeltaEvent
	//[event] *message.ToolCallDeltaEvent
	//[event] *message.ToolCallDeltaEvent
	//[tool] 调用结束 id=call_2323b3b71c434aeda0c93ff2
	//[event] *message.ModelCallEndEvent
	//[event] *message.ToolResultStartEvent
	//[event] *message.ToolResultTextDeltaEvent
	//
	//[tool] 检索完成 state=success
	//[event] *message.ModelCallStartEvent
	//##[event] *message.TextBlockStartEvent
	// AgentScope Go 简介
	//
	//**AgentScope Go** 是一个用 **Go 语言**构建 AI Agent 系统的开发框架。它的核心组成包括：
	//
	//- **Agents**（智能体）
	//- **Messages**（消息）
	//- **Model Adapters**（模型适配器）
	//- **Tools**（工具）
	//- **State**（状态管理）
	//- **Workspace Resources**（工作区资源）
	//- **Event-based Execution**（基于事件的执行机制）
	//
	//### 设计理念
	//
	//专为希望在 Go 中构建 AI Agent 系统的开发者设计，强调运行时的 **显式性**、**可测试性**，以及与现有 Go 服务的 **易集成性**。
	//
	//### 环境要求
	//
	//- Go 1.26.3 或更新版本
	//- GNU Make（用于开发目标）
	//- Python 3.13+、Node.js 22+、npm 10+（如需运行本地 lint 和文档工具）
	//
	//### 快速上手
	//
	//1. **安装模块：**
	//   ```bash
	//   go get github.com/yuluo-yx/agentscope-go@latest
	//   ```
	//
	//2. **导入核心包：**
	//   ```go
	//   import (
	//       "github.com/yuluo-yx/agentscope-go/pkg/agent"
	//       "github.com/yuluo-yx/agentscope-go/pkg/message"
	//       "github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	//   )
	//   ```
	//
	//3. **运行示例：**
	//   ```bash
	//   # 无需 API Key 的本地示例
	//   go run ./example/message
	//
	//   # DashScope 对话示例
	//   export AI_DASHSCOPE_API_KEY="your-key"
	//   go run ./example/model/dashscope
	//   ```
	//
	//4. **最小化使用示例：**
	//   ```go
	//   chat, err := dashscope.NewChatModel(
	//       dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
	//       "qwen-plus",
	//   )
	//   user, _ := message.NewUserMessage("user", "Say hello in one short sentence.")
	//   response, _ := chat.Call(context.Background(), asmodel.CallRequest{
	//       Messages: []*message.Message{user},
	//   })
	//   fmt.Println(response.Content)
	//   ```
	//
	//总而言之，AgentScope Go 为 Go 开发者提供了一套结构清晰、易于测试和集成的 AI Agent 开发工具链，核心 API 组织在 `pkg/` 导入根下，模块根目录还保留了常用核心 API 的别名门面包包。
	//[event] *message.TextBlockEndEvent
	//[event] *message.ModelCallEndEvent
	//[event] *message.ReplyEndEvent

	return nil
}

func knowledge(ctx context.Context) (*rag.KnowledgeBase, error) {

	model, err := dashscopeembedding.NewTextModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).EmbeddingCredential(),
		"text-embedding-v4",
		dashscopeembedding.WithDimensions(1024),
	)
	if err != nil {
		panic(err)
	}

	store := rag.NewMemoryVectorStore()
	kb, err := rag.NewKnowledgeBase(
		"example-pdf",
		"Example pdf",
		model,
		store,
		"c-1",
	)
	if err != nil {
		panic(err)
	}

	parser := pdf.NewParser()
	sections, err := rag.ParseFile(ctx, parser, "resources/agentscope-go.pdf")
	if err != nil {
		panic(err)
	}
	chunker, err := rag.NewApproxTokenChunker(rag.WithChunkSize(512), rag.WithChunkOverlap(64))
	if err != nil {
		panic(err)
	}
	chunk, err := chunker.Chunk(ctx, sections)
	if err != nil {
		panic(err)
	}
	if _, err := kb.InsertDocument(ctx, chunk); err != nil {
		panic(err)
	}

	return kb, nil
}
