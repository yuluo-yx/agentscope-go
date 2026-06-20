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

package openai

import (
	"context"
	"errors"
	"testing"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestTextModelConstructorCacheAndNilBranches(t *testing.T) {
	t.Parallel()

	cache := &openAIEmbeddingCache{embeddings: []types.Embedding{{0.1, 0.2, 0.3}}, ok: true}
	model, err := NewTextModel(
		NewCredential("key", WithBaseURL("http://127.0.0.1"), WithOrganization("org")),
		"text-embedding-3-small",
		WithDimensions(3),
		WithCache(cache),
		WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	if model.Name() != "openai:text-embedding-3-small" || (*TextModel)(nil).Name() != "openai:<nil>" {
		t.Fatalf("Name mismatch: %q", model.Name())
	}
	if model.Dimensions() != 3 || (*TextModel)(nil).Dimensions() != 0 {
		t.Fatalf("Dimensions mismatch: %d", model.Dimensions())
	}
	if got := model.SupportedModalities(); len(got) != 1 || got[0] != asembedding.ModalityText {
		t.Fatalf("SupportedModalities mismatch: %#v", got)
	}

	resp, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("cached")},
	})
	if err != nil {
		t.Fatalf("Embed cache hit returned error: %v", err)
	}
	if resp.Source != asembedding.SourceCache || cache.retrieveCalls != 1 {
		t.Fatalf("cache response mismatch: resp=%#v cache=%#v", resp, cache)
	}

	cases := []struct {
		name string
		fn   func() error
	}{
		{name: "empty credential", fn: func() error { _, err := NewTextModel(NewCredential(""), "m"); return err }},
		{name: "empty model", fn: func() error { _, err := NewTextModel(NewCredential("k"), ""); return err }},
		{name: "zero dimensions", fn: func() error { _, err := NewTextModel(NewCredential("k"), "m", WithDimensions(0)); return err }},
		{name: "zero retries", fn: func() error { _, err := NewTextModel(NewCredential("k"), "m", WithMaxRetries(0)); return err }},
		{name: "nil model embed", fn: func() error {
			_, err := (*TextModel)(nil).Embed(context.Background(), asembedding.EmbeddingRequest{
				Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("x")},
			})
			return err
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.fn(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestListModelsLoadsOpenAIEmbeddingCards(t *testing.T) {
	t.Parallel()

	cards, err := ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatalf("expected embedded OpenAI embedding cards")
	}
	found := false
	for _, card := range cards {
		if card.Name == "text-embedding-3-small" {
			found = true
			if card.Type != asembedding.ModelCardTypeEmbedding || len(card.InputTypes) == 0 || len(card.OutputTypes) == 0 {
				t.Fatalf("OpenAI embedding card metadata mismatch: %#v", card)
			}
		}
	}
	if !found {
		t.Fatalf("missing text-embedding-3-small card: %#v", cards)
	}
}

func TestCacheHelpersAndNormalizeErrorBranches(t *testing.T) {
	t.Parallel()

	if cached, ok, err := retrieveCache(context.Background(), nil, "k"); cached != nil || ok || err != nil {
		t.Fatalf("nil retrieveCache mismatch: cached=%#v ok=%v err=%v", cached, ok, err)
	}
	if err := storeCache(context.Background(), nil, "k", nil); err != nil {
		t.Fatalf("nil storeCache returned error: %v", err)
	}
	cacheErr := errors.New("cache failed")
	if _, _, err := retrieveCache(context.Background(), &openAIEmbeddingCache{errOnRetrieve: cacheErr}, "k"); !errors.Is(err, cacheErr) {
		t.Fatalf("retrieveCache should return error, got %v", err)
	}
	if err := storeCache(context.Background(), &openAIEmbeddingCache{errOnStore: cacheErr}, "k", nil); !errors.Is(err, cacheErr) {
		t.Fatalf("storeCache should return error, got %v", err)
	}

	err := normalizeError(errors.New("plain"))
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Provider != "openai" {
		t.Fatalf("normalizeError should return ProviderError, got %#v err=%v", providerErr, err)
	}
}

type openAIEmbeddingCache struct {
	embeddings    []types.Embedding
	ok            bool
	errOnRetrieve error
	errOnStore    error
	retrieveCalls int
	storeCalls    int
}

func (c *openAIEmbeddingCache) Store(context.Context, any, []types.Embedding, asembedding.StoreOptions) error {
	c.storeCalls++
	return c.errOnStore
}

func (c *openAIEmbeddingCache) Retrieve(context.Context, any) ([]types.Embedding, bool, error) {
	c.retrieveCalls++
	if c.errOnRetrieve != nil {
		return nil, false, c.errOnRetrieve
	}
	return c.embeddings, c.ok, nil
}

func (c *openAIEmbeddingCache) Remove(context.Context, any) error { return nil }

func (c *openAIEmbeddingCache) Clear(context.Context) error { return nil }
