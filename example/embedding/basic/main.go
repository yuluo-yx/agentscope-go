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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/embedding/dashscope"
	"github.com/yuluo-yx/agentscope-go/embedding/gemini"
	"github.com/yuluo-yx/agentscope-go/embedding/ollama"
	"github.com/yuluo-yx/agentscope-go/embedding/openai"
	"github.com/yuluo-yx/agentscope-go/types"
)

func main() {
	ctx := context.Background()
	cacheDir := envOr("EMBEDDING_CACHE_DIR", filepath.Join(".cache", "embeddings"))
	cache, err := asembedding.NewFileCache(cacheDir)
	if err != nil {
		panic(err)
	}

	provider := strings.ToLower(envOr("EMBEDDING_PROVIDER", "mock"))
	model, err := newEmbeddingModel(provider, cache)
	if err != nil {
		panic(err)
	}

	request := asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{
			asembedding.NewTextInput(envOr("EMBEDDING_EXAMPLE_TEXT", "AgentScope Go embedding example")),
		},
	}
	first, err := model.Embed(ctx, request)
	if err != nil {
		panic(err)
	}
	second, err := model.Embed(ctx, request)
	if err != nil {
		panic(err)
	}

	vectorDimensions := 0
	if len(first.Embeddings) > 0 {
		vectorDimensions = len(first.Embeddings[0])
	}
	fmt.Printf("provider=%s model=%s configured_dimensions=%d\n", provider, model.Name(), model.Dimensions())
	fmt.Printf("first_source=%s second_source=%s embeddings=%d vector_dimensions=%d\n", first.Source, second.Source, len(first.Embeddings), vectorDimensions)
	fmt.Printf("cache_dir=%s\n", cache.Dir())
}

func newEmbeddingModel(provider string, cache asembedding.EmbeddingCache) (asembedding.EmbeddingModel, error) {
	switch provider {
	case "", "mock":
		return newMockEmbeddingModel(cache), nil
	case "openai":
		return openai.NewTextModel(
			openai.NewCredential(requireEnv("OPENAI_API_KEY"), openAICredentialOptions()...),
			envOr("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"),
			openai.WithDimensions(envIntOr("OPENAI_EMBEDDING_DIMENSIONS", 1024)),
			openai.WithCache(cache),
			openai.WithMaxRetries(1),
		)
	case "gemini":
		return gemini.NewTextModel(
			gemini.NewCredential(requireEnv("GEMINI_API_KEY")),
			envOr("GEMINI_EMBEDDING_MODEL", "gemini-embedding-001"),
			gemini.WithDimensions(envIntOr("GEMINI_EMBEDDING_DIMENSIONS", 3072)),
			gemini.WithCache(cache),
		)
	case "ollama":
		options := []ollama.TextModelOption{ollama.WithCache(cache)}
		if dimensions, ok := optionalEnvInt("OLLAMA_EMBEDDING_DIMENSIONS"); ok {
			options = append(options, ollama.WithDimensions(dimensions))
		}
		return ollama.NewTextModel(
			ollama.NewCredential(ollama.WithHost(envOr("OLLAMA_HOST", "http://localhost:11434"))),
			envOr("OLLAMA_EMBEDDING_MODEL", "nomic-embed-text"),
			options...,
		)
	case "dashscope":
		return dashscope.NewTextModel(
			dashscope.NewCredential(requireEnv("DASHSCOPE_API_KEY"), dashScopeCredentialOptions()...),
			envOr("DASHSCOPE_TEXT_EMBEDDING_MODEL", "text-embedding-v4"),
			dashscope.WithDimensions(envIntOr("DASHSCOPE_TEXT_EMBEDDING_DIMENSIONS", 1024)),
			dashscope.WithCache(cache),
		)
	default:
		return nil, fmt.Errorf("unsupported EMBEDDING_PROVIDER %q, use mock, openai, gemini, ollama, or dashscope", provider)
	}
}

func openAICredentialOptions() []openai.CredentialOption {
	options := []openai.CredentialOption{}
	if baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
		options = append(options, openai.WithBaseURL(baseURL))
	}
	return options
}

func dashScopeCredentialOptions() []dashscope.CredentialOption {
	options := []dashscope.CredentialOption{}
	if baseURL := strings.TrimSpace(os.Getenv("DASHSCOPE_BASE_URL")); baseURL != "" {
		options = append(options, dashscope.WithBaseURL(baseURL))
	}
	return options
}

type mockEmbeddingModel struct {
	cache asembedding.EmbeddingCache
}

func newMockEmbeddingModel(cache asembedding.EmbeddingCache) *mockEmbeddingModel {
	return &mockEmbeddingModel{cache: cache}
}

func (m *mockEmbeddingModel) Name() string {
	return "mock:deterministic-embedding"
}

func (m *mockEmbeddingModel) Dimensions() int {
	return 4
}

func (m *mockEmbeddingModel) SupportedModalities() []asembedding.Modality {
	return []asembedding.Modality{asembedding.ModalityText}
}

func (m *mockEmbeddingModel) Embed(ctx context.Context, request asembedding.EmbeddingRequest) (*asembedding.EmbeddingResponse, error) {
	if err := request.Validate(asembedding.ModalityText); err != nil {
		return nil, err
	}
	cacheKey := asembedding.CacheIdentifier("mock", "deterministic-embedding", m.Dimensions(), request)
	if m.cache != nil {
		if embeddings, ok, err := m.cache.Retrieve(ctx, cacheKey); err != nil {
			return nil, err
		} else if ok {
			tokens := 0
			return asembedding.NewEmbeddingResponse(
				embeddings,
				asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Tokens: &tokens}),
				asembedding.WithEmbeddingSource(asembedding.SourceCache),
			), nil
		}
	}

	start := time.Now()
	embeddings := make([]types.Embedding, 0, len(request.Inputs))
	for i, input := range request.Inputs {
		embeddings = append(embeddings, mockEmbedding(input.Text, i))
	}
	tokens := len(request.Inputs)
	if m.cache != nil {
		if err := m.cache.Store(ctx, cacheKey, embeddings, asembedding.StoreOptions{Overwrite: true}); err != nil {
			return nil, err
		}
	}
	return asembedding.NewEmbeddingResponse(
		embeddings,
		asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Time: time.Since(start), Tokens: &tokens}),
	), nil
}

func mockEmbedding(text string, index int) types.Embedding {
	sum := 0
	for _, r := range text {
		sum += int(r)
	}
	length := len([]rune(text))
	return types.Embedding{
		float64(length),
		float64(sum%1000) / 1000,
		float64(index),
		1,
	}
}

func requireEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic(fmt.Sprintf("%s is required", name))
	}
	return value
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envIntOr(name string, fallback int) int {
	if value, ok := optionalEnvInt(name); ok {
		return value
	}
	return fallback
}

func optionalEnvInt(name string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		panic(fmt.Sprintf("%s must be an integer: %v", name, err))
	}
	return value, true
}

var _ asembedding.EmbeddingModel = (*mockEmbeddingModel)(nil)
