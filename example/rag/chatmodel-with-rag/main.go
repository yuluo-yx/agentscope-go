package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/yuluo-yx/agentscope-go/extensions/ragparser/docx"
	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	dashscopeE "github.com/yuluo-yx/agentscope-go/pkg/embedding/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

func main() {

	ctx := context.Background()

	kb, err := processRAG(ctx)
	if err != nil {
		panic(err)
	}

	if err := chat(ctx, kb); err != nil {
		panic(err)
	}

	// 如果一切正常，将得到以下输出：
	// go run .
	//根据知识库内容，AgentScope Go 是一个专为希望在 Go 语言中构建 AI 智能体系统的开发者设计的框架。
	//
	//它的主要特点和组成如下：
	//1. **核心构成**：包含智能体、消息、模型适配器、工具、状态、工作区资源以及基于事件的执行机制。
	//2. **设计目标**：旨在让运行时保持明确且具备可测试性。
	//3. **集成便利**：能够轻松地与现有的 Go 服务进行集成。根据知识库内容，AgentScope Go 是一个专为希望在 Go 语言中构建 AI 智能体系统的开发者设计的框架。
	//
	//它的主要特点和组成如下：
	//1. **核心构成**：包含智能体、消息、模型适配器、工具、状态、工作区资源以及基于事件的执行机制。
	//2. **设计目标**：旨在让运行时保持明确且具备可测试性。
	//3. **集成便利**：能够轻松地与现有的 Go 服务进行集成
}

func chat(ctx context.Context, kb *rag.KnowledgeBase) error {

	model, err := dashscope.NewChatModel(credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).ChatCredential(), "glm-5.2")
	if err != nil {
		panic(err)
	}

	// 用户问题
	prompt := "简单介绍下 agentscope go。"

	// 检索 kb
	res, err := kb.Search(
		ctx,
		message.ContentBlockList{message.NewTextBlock(prompt)},
		// 返回最相关的
		rag.WithSearchTopK(5),
	)
	if err != nil {
		panic(err)
	}
	if len(res) == 0 {
		panic(errors.New("未检索到知识库内容"))
	}

	// 把结果拼接进去
	var sb strings.Builder
	for i, r := range res {
		text := ""
		if tb, ok := r.Chunk.Content.(*message.TextBlock); ok {
			text = tb.Text
		}
		_, _ = fmt.Fprintf(&sb, "[%d] (source: %s, score: %.4f)\n%s\n\n", i+1, r.Chunk.Source, r.Score, text)
	}
	contextText := sb.String()

	// 注入到 system prompt
	systemMessage, err := message.NewSystemMessage(
		"system",
		fmt.Sprintf("你是一个知识库问答助手，以下是知识库内容：\n%s，如果知识库里没有对应内容，明确说明。", contextText),
	)
	if err != nil {
		panic(err)
	}
	userMessage, err := message.NewUserMessage("user", prompt)
	if err != nil {
		panic(err)
	}

	// 调用下
	resp, err := model.Stream(ctx, asmodel.CallRequest{
		Messages: []*message.Message{systemMessage, userMessage},
	})
	if err != nil {
		panic(err)
	}
	for chunk := range resp {
		if chunk.Error != nil {
			_, _ = fmt.Fprintf(os.Stderr, "stream error: %v\n", chunk.Error)
			return fmt.Errorf("stream chat: %w", chunk.Error)
		}
		if t := chunk.Content.GetTextContent(); t != nil {
			fmt.Print(*t)
		}
	}

	return nil
}

func processRAG(ctx context.Context) (*rag.KnowledgeBase, error) {

	// 1. 向量模型
	model, err := dashscopeE.NewTextModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).EmbeddingCredential(),
		"text-embedding-v4",
		dashscopeE.WithDimensions(1024),
	)
	if err != nil {
		panic(err)
	}

	// 2. 用基于内存的 vector 实现，可以替换 extensions 中的其他 vs 实现
	store := rag.NewMemoryVectorStore()

	// 3. knowledge
	kb, err := rag.NewKnowledgeBase(
		"example-docs",
		"Example RAG",
		model,
		store,
		"collection-1",
	)
	if err != nil {
		panic(err)
	}

	// 4. 解析 docx + 分块
	// 用户处理文本，这里使用 docx.Parser 解析 resource 下的 docx 文档。
	// parser := rag.NewTextParser()
	parser := docx.NewParser()
	sections, err := rag.ParseFile(ctx, parser, "resources/agentscope-go.docx")
	if err != nil {
		panic(err)
	}
	chunker, err := rag.NewApproxTokenChunker(rag.WithChunkSize(512), rag.WithChunkOverlap(64))
	if err != nil {
		panic(err)
	}
	chunks, err := chunker.Chunk(ctx, sections)
	if err != nil {
		panic(err)
	}

	if _, err := kb.InsertDocument(ctx, chunks); err != nil {
		panic(err)
	}

	// 返回给别的 caller 使用
	return kb, nil
}
