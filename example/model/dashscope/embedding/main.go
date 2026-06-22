// Copyright The AgentScope Go Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	asembedding "github.com/yuluo-yx/agentscope-go/pkg/embedding"
	"github.com/yuluo-yx/agentscope-go/pkg/embedding/dashscope"
)

func main() {
	model, err := dashscope.NewTextModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).EmbeddingCredential(),
		"text-embedding-v4",
		dashscope.WithDimensions(1024),
	)
	if err != nil {
		panic(err)
	}

	response, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{
			asembedding.NewTextInput("AgentScope Go makes agent applications easier to compose."),
			asembedding.NewTextInput("Credential adapters keep provider examples consistent."),
		},
	})
	if err != nil {
		panic(err)
	}

	firstDimensions := 0
	if len(response.Embeddings) > 0 {
		firstDimensions = len(response.Embeddings[0])
	}
	fmt.Printf(
		"dashscope_embedding=ok model=%s embeddings=%d dimensions=%d\n",
		model.Name(),
		len(response.Embeddings),
		firstDimensions,
	)
}
