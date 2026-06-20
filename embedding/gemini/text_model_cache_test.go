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

package gemini

import (
	"context"
	"errors"
	"math"
	"testing"

	"google.golang.org/genai"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestTextModelMetadataParametersAndCacheBranches(t *testing.T) {
	t.Parallel()

	model, err := NewTextModel(
		NewCredential(""),
		"gemini-embedding-001",
		WithDimensions(2),
		WithClient(&fakeGeminiEmbedClient{}),
	)
	if err != nil {
		t.Fatalf("NewTextModel with injected client returned error: %v", err)
	}
	if model.Name() != "gemini:gemini-embedding-001" || (*TextModel)(nil).Name() != "gemini:<nil>" {
		t.Fatalf("Name mismatch: %q", model.Name())
	}
	if model.Dimensions() != 2 || (*TextModel)(nil).Dimensions() != 0 {
		t.Fatalf("Dimensions mismatch: %d", model.Dimensions())
	}
	if len(model.SupportedModalities()) != 1 {
		t.Fatalf("SupportedModalities mismatch: %#v", model.SupportedModalities())
	}

	config := &genai.EmbedContentConfig{}
	applyParameters(config, map[string]any{
		"task_type":     "RETRIEVAL_QUERY",
		"taskType":      "SEMANTIC_SIMILARITY",
		"title":         "doc",
		"auto_truncate": true,
		"autoTruncate":  false,
	})
	if config.TaskType != "SEMANTIC_SIMILARITY" || config.Title != "doc" || config.AutoTruncate {
		t.Fatalf("applyParameters mismatch: %#v", config)
	}
	applyParameters(nil, map[string]any{"task_type": "ignored"})

	cache := &fakeEmbeddingCache{embeddings: []types.Embedding{{0.4, 0.5}}, ok: true}
	model.cache = cache
	resp, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("cached")}})
	if err != nil {
		t.Fatalf("Embed cache hit returned error: %v", err)
	}
	if resp.Source != asembedding.SourceCache || cache.retrieveCalls != 1 {
		t.Fatalf("cache response mismatch: resp=%#v cache=%#v", resp, cache)
	}

	storeErr := errors.New("store failed")
	model.cache = &fakeEmbeddingCache{errOnStore: storeErr}
	if _, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("api")}}); !errors.Is(err, storeErr) {
		t.Fatalf("Embed should return store error, got %v", err)
	}
}

func TestListModelsLoadsGeminiEmbeddingCards(t *testing.T) {
	t.Parallel()

	cards, err := ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatalf("expected embedded Gemini embedding cards")
	}
	found := false
	for _, card := range cards {
		if card.Name == "gemini-embedding-001" {
			found = true
			if card.Type != asembedding.ModelCardTypeEmbedding || len(card.InputTypes) == 0 || len(card.OutputTypes) == 0 {
				t.Fatalf("Gemini embedding card metadata mismatch: %#v", card)
			}
		}
	}
	if !found {
		t.Fatalf("missing gemini-embedding-001 card: %#v", cards)
	}
}

func TestTextModelConstructorAndNilBranches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		fn   func() error
	}{
		{name: "empty api key without client", fn: func() error { _, err := NewTextModel(NewCredential(""), "m"); return err }},
		{name: "empty model", fn: func() error {
			_, err := NewTextModel(NewCredential("k"), "", WithClient(&fakeGeminiEmbedClient{}))
			return err
		}},
		{name: "non-positive dimensions", fn: func() error {
			_, err := NewTextModel(NewCredential("k"), "m", WithDimensions(0), WithClient(&fakeGeminiEmbedClient{}))
			return err
		}},
		{name: "dimensions overflow int32", fn: func() error {
			_, err := NewTextModel(NewCredential("k"), "m", WithDimensions(math.MaxInt32+1), WithClient(&fakeGeminiEmbedClient{}))
			return err
		}},
		{name: "nil model embed", fn: func() error {
			_, err := (*TextModel)(nil).Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("x")}})
			return err
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.fn(); err == nil {
				t.Fatalf("expected error")
			}
		})
	}

	cacheErr := errors.New("cache failed")
	if _, _, err := retrieveCache(context.Background(), &fakeEmbeddingCache{errOnRetrieve: cacheErr}, "k"); !errors.Is(err, cacheErr) {
		t.Fatalf("retrieveCache should return error, got %v", err)
	}
	if err := storeCache(context.Background(), &fakeEmbeddingCache{errOnStore: cacheErr}, "k", nil); !errors.Is(err, cacheErr) {
		t.Fatalf("storeCache should return error, got %v", err)
	}
}

type fakeGeminiEmbedClient struct {
	config *genai.EmbedContentConfig
}

func (c *fakeGeminiEmbedClient) EmbedContent(_ context.Context, _ string, _ []*genai.Content, config *genai.EmbedContentConfig) (*genai.EmbedContentResponse, error) {
	c.config = config
	return &genai.EmbedContentResponse{
		Embeddings: []*genai.ContentEmbedding{{
			Values:     []float32{0.1, 0.2},
			Statistics: &genai.ContentEmbeddingStatistics{TokenCount: 3},
		}},
	}, nil
}

type fakeEmbeddingCache struct {
	embeddings    []types.Embedding
	ok            bool
	errOnRetrieve error
	errOnStore    error
	retrieveCalls int
	storeCalls    int
}

func (c *fakeEmbeddingCache) Store(context.Context, any, []types.Embedding, asembedding.StoreOptions) error {
	c.storeCalls++
	return c.errOnStore
}

func (c *fakeEmbeddingCache) Retrieve(context.Context, any) ([]types.Embedding, bool, error) {
	c.retrieveCalls++
	if c.errOnRetrieve != nil {
		return nil, false, c.errOnRetrieve
	}
	return c.embeddings, c.ok, nil
}

func (c *fakeEmbeddingCache) Remove(context.Context, any) error { return nil }

func (c *fakeEmbeddingCache) Clear(context.Context) error { return nil }
