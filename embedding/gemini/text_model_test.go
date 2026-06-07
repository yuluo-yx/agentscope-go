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

package gemini_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"google.golang.org/genai"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/embedding/gemini"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestTextModelUsesInjectedSDKClient(t *testing.T) {
	t.Parallel()

	client := &fakeEmbedContentClient{}
	model, err := gemini.NewTextModel(
		gemini.NewCredential("test-key"),
		"gemini-embedding-001",
		gemini.WithDimensions(2),
		gemini.WithClient(client),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	resp, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("hello")}})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if client.model != "gemini-embedding-001" {
		t.Fatalf("model not forwarded: %q", client.model)
	}
	if len(client.contents) != 1 || client.contents[0].Parts[0].Text != "hello" {
		t.Fatalf("contents not formatted: %#v", client.contents)
	}
	if client.config == nil || client.config.OutputDimensionality == nil || *client.config.OutputDimensionality != 2 {
		t.Fatalf("output dimensionality not forwarded: %#v", client.config)
	}
	if len(resp.Embeddings) != 1 || math.Abs(resp.Embeddings[0][0]-0.1) > 1e-6 || resp.Source != asembedding.SourceAPI {
		t.Fatalf("response mismatch: %#v", resp)
	}
}

func TestTextModelMapsSDKError(t *testing.T) {
	t.Parallel()

	client := &fakeEmbedContentClient{err: errors.New("gemini failed")}
	model, err := gemini.NewTextModel(gemini.NewCredential("test-key"), "gemini-embedding-001", gemini.WithClient(client))
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	_, err = model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("hello")}})
	if err == nil {
		t.Fatal("Embed should return provider error")
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Provider != "gemini" {
		t.Fatalf("error should expose Gemini ProviderError, got %T %v", err, err)
	}
}

func TestTextModelRejectsDimensionsOutsideSDKRange(t *testing.T) {
	t.Parallel()

	_, err := gemini.NewTextModel(
		gemini.NewCredential("test-key"),
		"gemini-embedding-001",
		gemini.WithDimensions(math.MaxInt32+1),
		gemini.WithClient(&fakeEmbedContentClient{}),
	)
	if !errors.Is(err, asembedding.ErrInvalidEmbeddingDimension) {
		t.Fatalf("NewTextModel should reject dimensions outside int32 range, got %v", err)
	}
}

type fakeEmbedContentClient struct {
	model    string
	contents []*genai.Content
	config   *genai.EmbedContentConfig
	err      error
}

func (f *fakeEmbedContentClient) EmbedContent(ctx context.Context, model string, contents []*genai.Content, config *genai.EmbedContentConfig) (*genai.EmbedContentResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.model = model
	f.contents = contents
	f.config = config
	if f.err != nil {
		return nil, f.err
	}
	return &genai.EmbedContentResponse{
		Embeddings: []*genai.ContentEmbedding{{Values: []float32{0.1, 0.2}}},
	}, nil
}
