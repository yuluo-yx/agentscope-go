package openai

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	modelopenai "github.com/yuluo-yx/agentscope-go/model/openai"
	"github.com/yuluo-yx/agentscope-go/types"
)

const providerName = "openai"

type (
	// Credential configures OpenAI authentication and endpoint settings.
	Credential = modelopenai.Credential
	// CredentialOption configures OpenAI credentials.
	CredentialOption = modelopenai.CredentialOption
)

// NewCredential creates OpenAI credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	return modelopenai.NewCredential(apiKey, opts...)
}

// WithBaseURL overrides the OpenAI-compatible endpoint.
func WithBaseURL(baseURL string) CredentialOption { return modelopenai.WithBaseURL(baseURL) }

// WithOrganization sets the OpenAI organization.
func WithOrganization(organization string) CredentialOption {
	return modelopenai.WithOrganization(organization)
}

// TextModel is a text embedding provider backed by openai-go.
type TextModel struct {
	credential Credential
	model      string
	dimensions int
	cache      asembedding.EmbeddingCache
	client     sdk.Client
}

// TextModelOption configures OpenAI text embedding.
type TextModelOption func(*textModelOptions)

type textModelOptions struct {
	dimensions int
	cache      asembedding.EmbeddingCache
	maxRetries int
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

// WithMaxRetries sets the SDK maximum retry count.
func WithMaxRetries(maxRetries int) TextModelOption {
	return func(options *textModelOptions) {
		options.maxRetries = maxRetries
	}
}

// NewTextModel creates an OpenAI text embedding provider.
func NewTextModel(credential Credential, model string, opts ...TextModelOption) (*TextModel, error) {
	options := textModelOptions{dimensions: 1024, maxRetries: 3}
	for _, opt := range opts {
		opt(&options)
	}
	if err := credential.Validate(); err != nil {
		return nil, fmt.Errorf("openai embedding credential: %w", err)
	}
	if model == "" {
		return nil, fmt.Errorf("%w: model is empty", asembedding.ErrInvalidEmbeddingInput)
	}
	if options.dimensions <= 0 {
		return nil, fmt.Errorf("%w: dimensions must be positive", asembedding.ErrInvalidEmbeddingDimension)
	}
	if options.maxRetries <= 0 {
		return nil, fmt.Errorf("%w: max retries must be positive", asembedding.ErrInvalidEmbeddingInput)
	}
	clientOptions := []option.RequestOption{
		option.WithAPIKey(credential.APIKey),
		option.WithMaxRetries(options.maxRetries),
	}
	if credential.BaseURL != "" {
		clientOptions = append(clientOptions, option.WithBaseURL(credential.BaseURL))
	}
	if credential.Organization != "" {
		clientOptions = append(clientOptions, option.WithOrganization(credential.Organization))
	}
	return &TextModel{
		credential: credential,
		model:      model,
		dimensions: options.dimensions,
		cache:      options.cache,
		client:     sdk.NewClient(clientOptions...),
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

// Embed calls the OpenAI embedding API.
func (m *TextModel) Embed(ctx context.Context, request asembedding.EmbeddingRequest) (*asembedding.EmbeddingResponse, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: nil OpenAI text model", asembedding.ErrInvalidEmbeddingInput)
	}
	texts, err := textInputs(request)
	if err != nil {
		return nil, err
	}
	cacheKey := asembedding.CacheIdentifier(providerName, m.model, m.dimensions, request)
	if cached, ok, err := retrieveCache(ctx, m.cache, cacheKey); err != nil {
		return nil, err
	} else if ok {
		return cached, nil
	}

	params := sdk.EmbeddingNewParams{
		Input:          sdk.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Model:          sdk.EmbeddingModel(m.model),
		Dimensions:     sdk.Int(int64(m.dimensions)),
		EncodingFormat: sdk.EmbeddingNewParamsEncodingFormatFloat,
	}
	start := time.Now()
	resp, err := m.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, normalizeError(err)
	}
	embeddings := make([]types.Embedding, 0, len(resp.Data))
	for _, item := range resp.Data {
		embeddings = append(embeddings, append(types.Embedding(nil), item.Embedding...))
	}
	tokens := int(resp.Usage.TotalTokens)
	out := asembedding.NewEmbeddingResponse(
		embeddings,
		asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Time: time.Since(start), Tokens: &tokens}),
	)
	if err := storeCache(ctx, m.cache, cacheKey, embeddings); err != nil {
		return nil, err
	}
	return out, nil
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
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		return asmodel.NormalizeError(providerName, err, asmodel.WithStatusCode(apiErr.StatusCode), asmodel.WithErrorCode(apiErr.Code))
	}
	return asmodel.NormalizeError(providerName, err)
}

var _ asembedding.EmbeddingModel = (*TextModel)(nil)
