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
	"fmt"
	"math"
	"time"

	"google.golang.org/genai"

	asembedding "github.com/yuluo-yx/agentscope-go/pkg/embedding"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

const providerName = "gemini"

// Credential configures the Gemini API key.
type Credential struct {
	APIKey string
}

// NewCredential creates Gemini credentials.
func NewCredential(apiKey string) Credential {
	return Credential{APIKey: apiKey}
}

// EmbedContentClient abstracts the Gemini SDK embedding call for offline test injection.
type EmbedContentClient interface {
	EmbedContent(context.Context, string, []*genai.Content, *genai.EmbedContentConfig) (*genai.EmbedContentResponse, error)
}

// TextModel is a Gemini text embedding provider.
type TextModel struct {
	credential Credential
	model      string
	dimensions int
	cache      asembedding.EmbeddingCache
	client     EmbedContentClient
}

// TextModelOption configures Gemini text embedding.
type TextModelOption func(*textModelOptions)

type textModelOptions struct {
	dimensions int
	cache      asembedding.EmbeddingCache
	client     EmbedContentClient
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

// WithClient injects a Gemini SDK client, primarily for tests.
func WithClient(client EmbedContentClient) TextModelOption {
	return func(options *textModelOptions) {
		options.client = client
	}
}

// NewTextModel creates a Gemini text embedding provider.
func NewTextModel(credential Credential, model string, opts ...TextModelOption) (*TextModel, error) {
	options := textModelOptions{dimensions: 3072}
	for _, opt := range opts {
		opt(&options)
	}
	if credential.APIKey == "" && options.client == nil {
		return nil, fmt.Errorf("%w: Gemini API key is empty", asembedding.ErrInvalidEmbeddingInput)
	}
	if model == "" {
		return nil, fmt.Errorf("%w: model is empty", asembedding.ErrInvalidEmbeddingInput)
	}
	if options.dimensions <= 0 {
		return nil, fmt.Errorf("%w: dimensions must be positive", asembedding.ErrInvalidEmbeddingDimension)
	}
	if options.dimensions > math.MaxInt32 {
		return nil, fmt.Errorf("%w: dimensions must fit int32", asembedding.ErrInvalidEmbeddingDimension)
	}
	client := options.client
	if client == nil {
		sdkClient, err := genai.NewClient(context.Background(), &genai.ClientConfig{
			APIKey:  credential.APIKey,
			Backend: genai.BackendGeminiAPI,
		})
		if err != nil {
			return nil, asmodel.NormalizeError(providerName, err)
		}
		client = sdkClient.Models
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

// Embed calls the Gemini embedding API.
func (m *TextModel) Embed(ctx context.Context, request asembedding.EmbeddingRequest) (*asembedding.EmbeddingResponse, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("%w: nil Gemini text model", asembedding.ErrInvalidEmbeddingInput)
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

	contents := make([]*genai.Content, 0, len(texts))
	for _, text := range texts {
		contents = append(contents, genai.NewContentFromText(text, genai.RoleUser))
	}
	dimensions := int32(m.dimensions) // #nosec G115 -- NewTextModel rejects values outside the int32 SDK range.
	config := &genai.EmbedContentConfig{OutputDimensionality: &dimensions}
	applyParameters(config, request.Parameters)
	start := time.Now()
	resp, err := m.client.EmbedContent(ctx, m.model, contents, config)
	if err != nil {
		return nil, asmodel.NormalizeError(providerName, err)
	}
	embeddings := make([]types.Embedding, 0, len(resp.Embeddings))
	var tokenSum int
	var hasTokens bool
	for _, embedding := range resp.Embeddings {
		out := make(types.Embedding, 0, len(embedding.Values))
		for _, value := range embedding.Values {
			out = append(out, float64(value))
		}
		embeddings = append(embeddings, out)
		if embedding.Statistics != nil {
			tokenSum += int(embedding.Statistics.TokenCount)
			hasTokens = true
		}
	}
	usage := &asembedding.EmbeddingUsage{Time: time.Since(start)}
	if hasTokens {
		usage.Tokens = &tokenSum
	}
	out := asembedding.NewEmbeddingResponse(embeddings, asembedding.WithEmbeddingUsage(usage))
	if err := storeCache(ctx, m.cache, cacheKey, embeddings); err != nil {
		return nil, err
	}
	return out, nil
}

func applyParameters(config *genai.EmbedContentConfig, parameters map[string]any) {
	if config == nil {
		return
	}
	if value, ok := parameters["task_type"].(string); ok {
		config.TaskType = value
	}
	if value, ok := parameters["taskType"].(string); ok {
		config.TaskType = value
	}
	if value, ok := parameters["title"].(string); ok {
		config.Title = value
	}
	if value, ok := parameters["auto_truncate"].(bool); ok {
		config.AutoTruncate = value
	}
	if value, ok := parameters["autoTruncate"].(bool); ok {
		config.AutoTruncate = value
	}
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

var _ asembedding.EmbeddingModel = (*TextModel)(nil)
