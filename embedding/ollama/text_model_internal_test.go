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

package ollama

import (
	"context"
	"errors"
	"net/http"
	"testing"

	ollamaapi "github.com/ollama/ollama/api"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestTextModelMetadataCacheAndErrorBranches(t *testing.T) {
	t.Parallel()

	cache := &internalEmbeddingCache{embeddings: []types.Embedding{{1, 2}}, ok: true}
	model, err := NewTextModel(NewCredential(), "nomic-embed-text", WithDimensions(2), WithCache(cache))
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	if model.Name() != "ollama:nomic-embed-text" || (*TextModel)(nil).Name() != "ollama:<nil>" {
		t.Fatalf("Name mismatch: %q", model.Name())
	}
	if model.Dimensions() != 2 || (*TextModel)(nil).Dimensions() != 0 {
		t.Fatalf("Dimensions mismatch: %d", model.Dimensions())
	}
	if len(model.SupportedModalities()) != 1 {
		t.Fatalf("SupportedModalities mismatch: %#v", model.SupportedModalities())
	}
	resp, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("cached")}})
	if err != nil {
		t.Fatalf("Embed cache hit returned error: %v", err)
	}
	if resp.Source != asembedding.SourceCache || cache.retrieveCalls != 1 {
		t.Fatalf("cache hit response mismatch: resp=%#v cache=%#v", resp, cache)
	}

	cases := []struct {
		name string
		fn   func() error
	}{
		{name: "empty model", fn: func() error { _, err := NewTextModel(NewCredential(), ""); return err }},
		{name: "negative dimensions", fn: func() error { _, err := NewTextModel(NewCredential(), "m", WithDimensions(-1)); return err }},
		{name: "invalid host", fn: func() error { _, err := NewTextModel(NewCredential(WithHost("://bad")), "m"); return err }},
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
}

func TestCacheAndNormalizeErrorBranches(t *testing.T) {
	t.Parallel()

	cacheErr := errors.New("cache failed")
	if _, _, err := retrieveCache(context.Background(), &internalEmbeddingCache{errOnRetrieve: cacheErr}, "k"); !errors.Is(err, cacheErr) {
		t.Fatalf("retrieveCache should return error, got %v", err)
	}
	if err := storeCache(context.Background(), &internalEmbeddingCache{errOnStore: cacheErr}, "k", nil); !errors.Is(err, cacheErr) {
		t.Fatalf("storeCache should return error, got %v", err)
	}
	err := normalizeError(ollamaapi.StatusError{StatusCode: http.StatusServiceUnavailable, ErrorMessage: "offline"})
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Provider != "ollama" || providerErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("normalizeError should expose provider status: %#v err=%v", providerErr, err)
	}
}

type internalEmbeddingCache struct {
	embeddings    []types.Embedding
	ok            bool
	errOnRetrieve error
	errOnStore    error
	retrieveCalls int
	storeCalls    int
}

func (c *internalEmbeddingCache) Store(context.Context, any, []types.Embedding, asembedding.StoreOptions) error {
	c.storeCalls++
	return c.errOnStore
}

func (c *internalEmbeddingCache) Retrieve(context.Context, any) ([]types.Embedding, bool, error) {
	c.retrieveCalls++
	if c.errOnRetrieve != nil {
		return nil, false, c.errOnRetrieve
	}
	return c.embeddings, c.ok, nil
}

func (c *internalEmbeddingCache) Remove(context.Context, any) error { return nil }

func (c *internalEmbeddingCache) Clear(context.Context) error { return nil }
