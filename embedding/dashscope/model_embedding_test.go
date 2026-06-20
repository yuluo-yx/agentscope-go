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

package dashscope

import (
	"context"
	"errors"
	"net/http"
	"testing"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestTextModelConstructorsMetadataValidationAndCache(t *testing.T) {
	t.Parallel()

	cache := &fakeEmbeddingCache{embeddings: []types.Embedding{{0.1, 0.2}}, ok: true}
	model, err := NewTextModel(
		NewCredential("dash-key", WithBaseURL(" https://dashscope.example.com/ ")),
		"text-embedding-v1",
		WithCache(cache),
		WithHTTPClient(http.DefaultClient),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	if model.Name() != "dashscope:text-embedding-v1" || (*TextModel)(nil).Name() != "dashscope:<nil>" {
		t.Fatalf("Name mismatch: %q", model.Name())
	}
	if model.Dimensions() != defaultTextEmbeddingDim || (*TextModel)(nil).Dimensions() != 0 {
		t.Fatalf("Dimensions mismatch: %d", model.Dimensions())
	}
	if model.batchSizeLimit != defaultTextEmbeddingV1V2Size || len(model.SupportedModalities()) != 1 {
		t.Fatalf("text model defaults mismatch: batch=%d modalities=%#v", model.batchSizeLimit, model.SupportedModalities())
	}
	resp, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("cached")}})
	if err != nil {
		t.Fatalf("Embed should read cache: %v", err)
	}
	if resp.Source != asembedding.SourceCache || len(resp.Embeddings) != 1 || cache.retrieveCalls != 1 {
		t.Fatalf("cached response mismatch: resp=%#v cache=%#v", resp, cache)
	}

	invalidCases := []struct {
		name string
		fn   func() error
	}{
		{name: "empty api key", fn: func() error { _, err := NewTextModel(NewCredential(""), "m"); return err }},
		{name: "empty base url", fn: func() error { _, err := NewTextModel(NewCredential("k", WithBaseURL(" ")), "m"); return err }},
		{name: "empty model", fn: func() error { _, err := NewTextModel(NewCredential("k"), ""); return err }},
		{name: "non-positive dimensions", fn: func() error { _, err := NewTextModel(NewCredential("k"), "m", WithDimensions(0)); return err }},
		{name: "non-positive batch", fn: func() error { _, err := NewTextModel(NewCredential("k"), "m", WithBatchSizeLimit(0)); return err }},
	}
	for _, tt := range invalidCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.fn(); err == nil {
				t.Fatalf("expected constructor error")
			}
		})
	}

	cacheErr := errors.New("cache failed")
	if _, _, err := retrieveCache(context.Background(), &fakeEmbeddingCache{err: cacheErr}, "k"); !errors.Is(err, cacheErr) {
		t.Fatalf("retrieveCache should return cache error, got %v", err)
	}
	if err := storeCache(context.Background(), &fakeEmbeddingCache{err: cacheErr}, "k", nil); !errors.Is(err, cacheErr) {
		t.Fatalf("storeCache should return cache error, got %v", err)
	}
}

func TestListModelsAndMultiModalCacheBranches(t *testing.T) {
	t.Parallel()

	cards, err := ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("ListModels should load embedded DashScope model cards")
	}

	cache := &fakeEmbeddingCache{embeddings: []types.Embedding{{0.3, 0.4}}, ok: true}
	model, err := NewMultiModalModel(
		NewCredential("dash-key"),
		"multimodal-embedding-v1",
		WithCache(cache),
		WithHTTPClient(nil),
	)
	if err != nil {
		t.Fatalf("NewMultiModalModel returned error: %v", err)
	}
	resp, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{
		asembedding.NewTextInput("cached"),
	}})
	if err != nil {
		t.Fatalf("cached multimodal Embed returned error: %v", err)
	}
	if resp.Source != asembedding.SourceCache || len(resp.Embeddings) != 1 || cache.retrieveCalls != 1 {
		t.Fatalf("cached multimodal response mismatch: resp=%#v cache=%#v", resp, cache)
	}
	if _, err := (*MultiModalModel)(nil).Embed(context.Background(), asembedding.EmbeddingRequest{}); !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("nil multimodal Embed should return invalid input, got %v", err)
	}
}

func TestMultiModalDefaultsFormattingAndValidationBranches(t *testing.T) {
	t.Parallel()

	vision, err := NewMultiModalModel(NewCredential("dash-key"), "tongyi-embedding-vision-plus", WithBatchSizeLimit(8))
	if err != nil {
		t.Fatalf("NewMultiModalModel vision-plus returned error: %v", err)
	}
	if vision.Name() != "dashscope:tongyi-embedding-vision-plus" || vision.Dimensions() != 1152 || vision.batchSizeLimit != 8 {
		t.Fatalf("vision-plus defaults mismatch: %#v", vision)
	}
	if (*MultiModalModel)(nil).Name() != "dashscope:<nil>" || (*MultiModalModel)(nil).Dimensions() != 0 {
		t.Fatalf("nil multimodal metadata mismatch")
	}
	if len(vision.SupportedModalities()) != 3 {
		t.Fatalf("multimodal supported modalities mismatch: %#v", vision.SupportedModalities())
	}
	if _, err := NewMultiModalModel(NewCredential("dash-key"), ""); err == nil {
		t.Fatalf("empty multimodal model should be rejected")
	}
	if _, err := NewMultiModalModel(NewCredential("dash-key"), "tongyi-embedding-vision-flash", WithBatchSizeLimit(-1)); err == nil {
		t.Fatalf("negative multimodal batch should be rejected")
	}

	formatted, err := formatMultimodalInputs([]asembedding.EmbeddingInput{
		asembedding.NewTextInput("hello"),
		asembedding.NewImageURLInput("https://example.com/image.png", "image/png"),
	})
	if err != nil {
		t.Fatalf("formatMultimodalInputs returned error: %v", err)
	}
	if formatted[0]["text"] != "hello" || formatted[1]["image"] != "https://example.com/image.png" {
		t.Fatalf("formatted multimodal input mismatch: %#v", formatted)
	}
	errorCases := []struct {
		name   string
		inputs []asembedding.EmbeddingInput
	}{
		{name: "image missing source", inputs: []asembedding.EmbeddingInput{{Type: asembedding.ModalityImage}}},
		{name: "image unsupported source", inputs: []asembedding.EmbeddingInput{{
			Type:   asembedding.ModalityImage,
			Source: &asembedding.EmbeddingSource{Type: "file"},
		}}},
		{name: "video missing url", inputs: []asembedding.EmbeddingInput{{Type: asembedding.ModalityVideo}}},
		{name: "unsupported modality", inputs: []asembedding.EmbeddingInput{{Type: "audio"}}},
	}
	for _, tt := range errorCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := formatMultimodalInputs(tt.inputs); err == nil {
				t.Fatalf("expected formatting error")
			}
		})
	}

	body := map[string]any{"dimension": 1024}
	mergeExtraParameters(body, map[string]any{"dimension": 2048, "text_type": "query"})
	if body["dimension"] != 1024 || body["text_type"] != "query" {
		t.Fatalf("mergeExtraParameters should preserve existing keys: %#v", body)
	}
}

type fakeEmbeddingCache struct {
	embeddings    []types.Embedding
	ok            bool
	err           error
	retrieveCalls int
	storeCalls    int
}

func (c *fakeEmbeddingCache) Store(context.Context, any, []types.Embedding, asembedding.StoreOptions) error {
	c.storeCalls++
	return c.err
}

func (c *fakeEmbeddingCache) Retrieve(context.Context, any) ([]types.Embedding, bool, error) {
	c.retrieveCalls++
	if c.err != nil {
		return nil, false, c.err
	}
	return c.embeddings, c.ok, nil
}

func (c *fakeEmbeddingCache) Remove(context.Context, any) error { return nil }

func (c *fakeEmbeddingCache) Clear(context.Context) error { return nil }
