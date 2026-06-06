package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/embedding/dashscope"
)

func main() {

	ctx := context.Background()

	// Text embedding
	fmt.Println("Text embedding: ------------------------------------")
	textModel, err := dashscope.NewTextModel(dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")), "text-embedding-v4")
	if err != nil {
		panic(err)
	}
	embeddingResponse, err := textModel.Embed(ctx, embedding.EmbeddingRequest{
		Inputs: []embedding.EmbeddingInput{
			embedding.EmbeddingInput{
				Type: embedding.ModalityText,
				Text: "hi, AgentScope Go!",
			},
		},
	})
	if err != nil {
		panic(err)
	}
	for _, res := range embeddingResponse.Embeddings {
		fmt.Print(res)
	}

	// MultiModel embedding
	fmt.Println("\nImage embedding: ------------------------------------")
	multiModalModel, err := dashscope.NewMultiModalModel(dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")), "qwen3-vl-embedding")
	if err != nil {
		panic(err)
	}
	imageResponse, err := multiModalModel.Embed(ctx, embedding.EmbeddingRequest{
		Inputs: []embedding.EmbeddingInput{
			embedding.EmbeddingInput{
				Type: embedding.ModalityImage,
				Source: &embedding.EmbeddingSource{
					Type: embedding.SourceURL,
					URL:  "https://img.alicdn.com/imgextra/i3/O1CN01rdstgY1uiZWt8gqSL_!!6000000006071-0-tps-1970-356.jpg",
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	for _, res := range imageResponse.Embeddings {
		fmt.Print(res)
	}

	fmt.Println("\nVideo embedding: ------------------------------------")
	videoResponse, err := multiModalModel.Embed(ctx, embedding.EmbeddingRequest{
		Inputs: []embedding.EmbeddingInput{
			embedding.EmbeddingInput{
				Type: embedding.ModalityVideo,
				Source: &embedding.EmbeddingSource{
					Type: embedding.SourceURL,
					URL:  "https://help-static-aliyun-doc.aliyuncs.com/file-manage-files/zh-CN/20250107/lbcemt/new+video.mp4",
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	for _, res := range videoResponse.Embeddings {
		fmt.Print(res)
	}
}
