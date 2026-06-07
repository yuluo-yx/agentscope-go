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
	"fmt"
	"net/http"
	"net/url"
	"time"

	ollamaapi "github.com/ollama/ollama/api"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	modelollama "github.com/yuluo-yx/agentscope-go/model/ollama"
	"github.com/yuluo-yx/agentscope-go/types"
)

const providerName = "ollama"

type (
	// Credential configures the Ollama connection.
	Credential = modelollama.Credential
	// CredentialOption configures Ollama credentials.
	CredentialOption = modelollama.CredentialOption
)

// NewCredential creates Ollama credentials.
func NewCredential(opts ...CredentialOption) Credential {
	return modelollama.NewCredential(opts...)
}

// WithHost sets the Ollama host.
func WithHost(host string) CredentialOption { return modelollama.WithHost(host) }

// TextModel is an Ollama text embedding provider.
type TextModel struct {
	credential Credential
	model      string
	dimensions int
	cache      asembedding.EmbeddingCache
	client     *ollamaapi.Client
}

// TextModelOption configures Ollama text embedding.
type TextModelOption func(*textModelOptions)

type textModelOptions struct {
	dimensions int
	cache      asembedding.EmbeddingCache
	client     *ollamaapi.Client
}

// WithDimensions sets the output vector dimensions.
func WithDimensions(dimensions int) TextModelOption {
	return func(options *textModelOptions) {
		options.dimensions = dimensions
	}
}

// WithCache sets the embedding cache.
func WithCache(cache asembedding.EmbeddingCache) TextModelOption {
	return func(options *textModelOptions) {
		options.cache = cache
	}
}

// WithClient injects an Ollama SDK client, primarily for tests.
func WithClient(client *ollamaapi.Client) TextModelOption {
	return func(options *textModelOptions) {
		options.client = client
	}
}

// NewTextModel creates an Ollama text embedding provider.
func NewTextModel(credential Credential, model string, opts ...TextModelOption) (*TextModel, error) {
	options := textModelOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	if model == "" {
		return nil, fmt.Errorf("%w: model is empty", asembedding.ErrInvalidEmbeddingInput)
	}
	if options.dimensions < 0 {
		return nil, fmt.Errorf("%w: dimensions must be non-negative", asembedding.ErrInvalidEmbeddingDimension)
	}
	client := options.client
	if client == nil {
		var err error
		client, err = newClient(credential)
		if err != nil {
			return nil, err
		}
	}
	return &TextModel{
		credential: credential,
		model:      model,
		dimensions: options.dimensions,
		cache:      options.cache,
		client:     client,
	}, nil
}

// Name returns the provider-qualified model name.
func (m *TextModel) Name() string {
	if m == nil {
		return providerName + ":<nil>"
	}
	return providerName + ":" + m.model
}

// Dimensions returns the output vector dimensions.
func (m *TextModel) Dimensions() int {
	if m == nil {
		return 0
	}
	return m.dimensions
}

// SupportedModalities returns the supported input modalities.
func (m *TextModel) SupportedModalities() []asembedding.Modality {
	return []asembedding.Modality{asembedding.ModalityText}
}

// Embed calls the Ollama embedding API.
func (m *TextModel) Embed(ctx context.Context, request asembedding.EmbeddingRequest) (*asembedding.EmbeddingResponse, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("%w: nil Ollama text model", asembedding.ErrInvalidEmbeddingInput)
	}
	texts, err := textInputs(request)
	if err != nil {
		return nil, err
	}
	cacheKey := asembedding.CacheIdentifier(providerName, m.model, m.dimensions, request)
	cached, ok, cacheErr := retrieveCache(ctx, m.cache, cacheKey)
	if cacheErr != nil {
		return nil, cacheErr
	} else if ok {
		return cached, nil
	}

	req := &ollamaapi.EmbedRequest{
		Model:      m.model,
		Input:      texts,
		Dimensions: m.dimensions,
	}
	start := time.Now()
	resp, err := m.client.Embed(ctx, req)
	if err != nil {
		return nil, normalizeError(err)
	}
	embeddings := make([]types.Embedding, 0, len(resp.Embeddings))
	for _, embedding := range resp.Embeddings {
		out := make(types.Embedding, 0, len(embedding))
		for _, value := range embedding {
			out = append(out, float64(value))
		}
		embeddings = append(embeddings, out)
	}
	tokens := resp.PromptEvalCount
	out := asembedding.NewEmbeddingResponse(
		embeddings,
		asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Time: time.Since(start), Tokens: &tokens}),
	)
	if err := storeCache(ctx, m.cache, cacheKey, embeddings); err != nil {
		return nil, err
	}
	return out, nil
}

func newClient(credential Credential) (*ollamaapi.Client, error) {
	if credential.Host == "" {
		return ollamaapi.ClientFromEnvironment()
	}
	parsed, err := url.Parse(credential.Host)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%w: invalid Ollama host %q", asembedding.ErrInvalidEmbeddingInput, credential.Host)
	}
	return ollamaapi.NewClient(parsed, http.DefaultClient), nil
}

func textInputs(request asembedding.EmbeddingRequest) ([]string, error) {
	if err := request.Validate(asembedding.ModalityText); err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(request.Inputs))
	for _, input := range request.Inputs {
		texts = append(texts, input.Text)
	}
	return texts, nil
}

func retrieveCache(ctx context.Context, cache asembedding.EmbeddingCache, key string) (*asembedding.EmbeddingResponse, bool, error) {
	if cache == nil {
		return nil, false, nil
	}
	embeddings, ok, err := cache.Retrieve(ctx, key)
	if err != nil || !ok {
		return nil, ok, err
	}
	tokens := 0
	return asembedding.NewEmbeddingResponse(
		embeddings,
		asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Tokens: &tokens}),
		asembedding.WithEmbeddingSource(asembedding.SourceCache),
	), true, nil
}

func storeCache(ctx context.Context, cache asembedding.EmbeddingCache, key string, embeddings []types.Embedding) error {
	if cache == nil {
		return nil
	}
	return cache.Store(ctx, key, embeddings, asembedding.StoreOptions{Overwrite: true})
}

func normalizeError(err error) error {
	var statusErr ollamaapi.StatusError
	if errors.As(err, &statusErr) {
		return asmodel.NormalizeError(providerName, err, asmodel.WithStatusCode(statusErr.StatusCode))
	}
	return asmodel.NormalizeError(providerName, err)
}

var _ asembedding.EmbeddingModel = (*TextModel)(nil)
