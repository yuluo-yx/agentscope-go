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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/types"
)

const (
	providerName                 = "dashscope"
	defaultBaseURL               = "https://dashscope.aliyuncs.com"
	textEmbeddingPath            = "/api/v1/services/embeddings/text-embedding/text-embedding"
	multimodalEmbeddingPath      = "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding"
	defaultTextEmbeddingDim      = 1024
	defaultTextEmbeddingBatch    = 10
	defaultTextEmbeddingV1V2Size = 25
)

// Credential configures the DashScope API key and endpoint.
type Credential struct {
	APIKey  string
	BaseURL string
}

// CredentialOption configures DashScope credentials.
type CredentialOption func(*Credential)

// NewCredential creates DashScope credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	credential := Credential{APIKey: apiKey, BaseURL: defaultBaseURL}
	for _, opt := range opts {
		opt(&credential)
	}
	credential.BaseURL = strings.TrimRight(strings.TrimSpace(credential.BaseURL), "/")
	return credential
}

// WithBaseURL overrides the DashScope endpoint.
func WithBaseURL(baseURL string) CredentialOption {
	return func(credential *Credential) {
		credential.BaseURL = baseURL
	}
}

// ModelOption configures a DashScope embedding provider.
type ModelOption func(*modelOptions)

type modelOptions struct {
	dimensions     int
	batchSizeLimit int
	cache          asembedding.EmbeddingCache
	httpClient     *http.Client
}

// WithDimensions sets the output vector dimensions.
func WithDimensions(dimensions int) ModelOption {
	return func(options *modelOptions) {
		options.dimensions = dimensions
	}
}

// WithBatchSizeLimit sets the batch size limit.
func WithBatchSizeLimit(limit int) ModelOption {
	return func(options *modelOptions) {
		options.batchSizeLimit = limit
	}
}

// WithCache sets the embedding cache.
func WithCache(cache asembedding.EmbeddingCache) ModelOption {
	return func(options *modelOptions) {
		options.cache = cache
	}
}

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(client *http.Client) ModelOption {
	return func(options *modelOptions) {
		options.httpClient = client
	}
}

// TextModel is a DashScope text embedding provider.
type TextModel struct {
	credential     Credential
	model          string
	dimensions     int
	batchSizeLimit int
	cache          asembedding.EmbeddingCache
	httpClient     *http.Client
}

// NewTextModel creates a DashScope text embedding provider.
func NewTextModel(credential Credential, model string, opts ...ModelOption) (*TextModel, error) {
	options := modelOptions{
		dimensions:     defaultTextEmbeddingDim,
		batchSizeLimit: defaultTextBatchLimit(model),
		httpClient:     http.DefaultClient,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := validateCredential(credential); err != nil {
		return nil, err
	}
	if model == "" {
		return nil, fmt.Errorf("%w: model is empty", asembedding.ErrInvalidEmbeddingInput)
	}
	if options.dimensions <= 0 {
		return nil, fmt.Errorf("%w: dimensions must be positive", asembedding.ErrInvalidEmbeddingDimension)
	}
	if options.batchSizeLimit <= 0 {
		return nil, fmt.Errorf("%w: batch size limit must be positive", asembedding.ErrInvalidEmbeddingInput)
	}
	return &TextModel{
		credential:     credential,
		model:          model,
		dimensions:     options.dimensions,
		batchSizeLimit: options.batchSizeLimit,
		cache:          options.cache,
		httpClient:     options.httpClient,
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

// Embed calls the DashScope text embedding API.
func (m *TextModel) Embed(ctx context.Context, request asembedding.EmbeddingRequest) (*asembedding.EmbeddingResponse, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: nil DashScope text model", asembedding.ErrInvalidEmbeddingInput)
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

	start := time.Now()
	var embeddings []types.Embedding
	totalTokens := 0
	for from := 0; from < len(texts); from += m.batchSizeLimit {
		to := min(from+m.batchSizeLimit, len(texts))
		parameters := map[string]any{"dimension": m.dimensions}
		mergeExtraParameters(parameters, request.Parameters)
		body := map[string]any{
			"model":      m.model,
			"input":      map[string]any{"texts": texts[from:to]},
			"parameters": parameters,
		}
		res, err := m.call(ctx, textEmbeddingPath, body)
		if err != nil {
			return nil, err
		}
		embeddings = append(embeddings, res.embeddings...)
		totalTokens += res.tokens
	}
	out := asembedding.NewEmbeddingResponse(
		embeddings,
		asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Time: time.Since(start), Tokens: &totalTokens}),
	)
	if err := storeCache(ctx, m.cache, cacheKey, embeddings); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *TextModel) call(ctx context.Context, path string, body map[string]any) (*parsedResponse, error) {
	return callDashScope(ctx, m.httpClient, m.credential, path, body)
}

// MultiModalModel is a DashScope multimodal embedding provider.
type MultiModalModel struct {
	credential     Credential
	model          string
	dimensions     int
	batchSizeLimit int
	cache          asembedding.EmbeddingCache
	httpClient     *http.Client
}

// NewMultiModalModel creates a DashScope multimodal embedding provider.
func NewMultiModalModel(credential Credential, model string, opts ...ModelOption) (*MultiModalModel, error) {
	options := modelOptions{httpClient: http.DefaultClient}
	for _, opt := range opts {
		opt(&options)
	}
	if err := validateCredential(credential); err != nil {
		return nil, err
	}
	dimensions, batchLimit, err := multimodalDefaults(model, options.dimensions, options.batchSizeLimit)
	if err != nil {
		return nil, err
	}
	return &MultiModalModel{
		credential:     credential,
		model:          model,
		dimensions:     dimensions,
		batchSizeLimit: batchLimit,
		cache:          options.cache,
		httpClient:     options.httpClient,
	}, nil
}

// Name returns the provider-qualified model name.
func (m *MultiModalModel) Name() string {
	if m == nil {
		return providerName + ":<nil>"
	}
	return providerName + ":" + m.model
}

// Dimensions returns the output vector dimensions.
func (m *MultiModalModel) Dimensions() int {
	if m == nil {
		return 0
	}
	return m.dimensions
}

// SupportedModalities returns the supported input modalities.
func (m *MultiModalModel) SupportedModalities() []asembedding.Modality {
	return []asembedding.Modality{asembedding.ModalityText, asembedding.ModalityImage, asembedding.ModalityVideo}
}

// Embed calls the DashScope multimodal embedding API.
func (m *MultiModalModel) Embed(ctx context.Context, request asembedding.EmbeddingRequest) (*asembedding.EmbeddingResponse, error) {
	if m == nil {
		return nil, fmt.Errorf("%w: nil DashScope multimodal model", asembedding.ErrInvalidEmbeddingInput)
	}
	if err := request.Validate(asembedding.ModalityText, asembedding.ModalityImage, asembedding.ModalityVideo); err != nil {
		return nil, err
	}
	formatted, err := formatMultimodalInputs(request.Inputs)
	if err != nil {
		return nil, err
	}
	cacheKey := asembedding.CacheIdentifier(providerName, m.model, m.dimensions, request)
	if cached, ok, err := retrieveCache(ctx, m.cache, cacheKey); err != nil {
		return nil, err
	} else if ok {
		return cached, nil
	}

	start := time.Now()
	var embeddings []types.Embedding
	totalTokens := 0
	for from := 0; from < len(formatted); from += m.batchSizeLimit {
		to := min(from+m.batchSizeLimit, len(formatted))
		body := map[string]any{
			"model": m.model,
			"input": map[string]any{"contents": formatted[from:to]},
		}
		if len(request.Parameters) > 0 {
			parameters := map[string]any{}
			mergeExtraParameters(parameters, request.Parameters)
			body["parameters"] = parameters
		}
		res, err := callDashScope(ctx, m.httpClient, m.credential, multimodalEmbeddingPath, body)
		if err != nil {
			return nil, err
		}
		embeddings = append(embeddings, res.embeddings...)
		totalTokens += res.tokens
	}
	out := asembedding.NewEmbeddingResponse(
		embeddings,
		asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Time: time.Since(start), Tokens: &totalTokens}),
	)
	if err := storeCache(ctx, m.cache, cacheKey, embeddings); err != nil {
		return nil, err
	}
	return out, nil
}

func validateCredential(credential Credential) error {
	if credential.APIKey == "" {
		return fmt.Errorf("%w: DashScope API key is empty", asembedding.ErrInvalidEmbeddingInput)
	}
	if strings.TrimSpace(credential.BaseURL) == "" {
		return fmt.Errorf("%w: DashScope base URL is empty", asembedding.ErrInvalidEmbeddingInput)
	}
	return nil
}

func defaultTextBatchLimit(model string) int {
	if strings.Contains(model, "text-embedding-v1") || strings.Contains(model, "text-embedding-v2") {
		return defaultTextEmbeddingV1V2Size
	}
	return defaultTextEmbeddingBatch
}

func multimodalDefaults(model string, dimensions, batchLimit int) (int, int, error) {
	if model == "" {
		return 0, 0, fmt.Errorf("%w: model is empty", asembedding.ErrInvalidEmbeddingInput)
	}
	expectedDimensions := 1024
	expectedBatchLimit := 1
	switch {
	case strings.HasPrefix(model, "tongyi-embedding-vision-plus"):
		expectedDimensions = 1152
		expectedBatchLimit = 8
	case strings.HasPrefix(model, "tongyi-embedding-vision-flash"):
		expectedDimensions = 768
		expectedBatchLimit = 8
	case strings.HasPrefix(model, "multimodal-embedding-v"):
		expectedDimensions = 1024
		expectedBatchLimit = 1
	}
	if dimensions == 0 {
		dimensions = expectedDimensions
	}
	if dimensions != expectedDimensions {
		return 0, 0, fmt.Errorf("%w: model %s requires dimension %d", asembedding.ErrInvalidEmbeddingDimension, model, expectedDimensions)
	}
	if batchLimit == 0 {
		batchLimit = expectedBatchLimit
	}
	if batchLimit <= 0 {
		return 0, 0, fmt.Errorf("%w: batch size limit must be positive", asembedding.ErrInvalidEmbeddingInput)
	}
	return dimensions, batchLimit, nil
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

func formatMultimodalInputs(inputs []asembedding.EmbeddingInput) ([]map[string]any, error) {
	formatted := make([]map[string]any, 0, len(inputs))
	for _, input := range inputs {
		switch input.Type {
		case asembedding.ModalityText:
			formatted = append(formatted, map[string]any{"text": input.Text})
		case asembedding.ModalityImage:
			if input.Source == nil {
				return nil, fmt.Errorf("%w: image source is required", asembedding.ErrInvalidEmbeddingInput)
			}
			switch input.Source.Type {
			case asembedding.SourceURL:
				formatted = append(formatted, map[string]any{"image": input.Source.URL})
			case asembedding.SourceBase64:
				formatted = append(formatted, map[string]any{"image": fmt.Sprintf("data:%s;base64,%s", input.Source.MediaType, input.Source.Data)})
			default:
				return nil, fmt.Errorf("%w: unsupported image source %q", asembedding.ErrInvalidEmbeddingInput, input.Source.Type)
			}
		case asembedding.ModalityVideo:
			if input.Source == nil || input.Source.Type != asembedding.SourceURL {
				return nil, fmt.Errorf("%w: video only supports URL source", asembedding.ErrInvalidEmbeddingInput)
			}
			formatted = append(formatted, map[string]any{"video": input.Source.URL})
		default:
			return nil, fmt.Errorf("%w: %s", asembedding.ErrUnsupportedModality, input.Type)
		}
	}
	return formatted, nil
}

type rawDashScopeResponse struct {
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Output    struct {
		Embeddings []struct {
			Embedding types.Embedding `json:"embedding"`
		} `json:"embeddings"`
	} `json:"output"`
	Usage map[string]int `json:"usage"`
}

type parsedResponse struct {
	embeddings []types.Embedding
	tokens     int
}

func callDashScope(ctx context.Context, client *http.Client, credential Credential, path string, body map[string]any) (*parsedResponse, error) {
	if client == nil {
		client = http.DefaultClient
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, credential.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil, asmodel.NormalizeError(providerName, err)
	}
	defer resp.Body.Close()

	var raw rawDashScopeResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, asmodel.NormalizeError(providerName, err, asmodel.WithStatusCode(resp.StatusCode))
	}
	if resp.StatusCode >= http.StatusBadRequest {
		message := raw.Message
		if message == "" {
			message = resp.Status
		}
		return nil, &asmodel.ProviderError{
			Provider:   providerName,
			Code:       raw.Code,
			StatusCode: resp.StatusCode,
			Message:    message,
			Err:        fmt.Errorf("%s", message),
		}
	}
	embeddings := make([]types.Embedding, 0, len(raw.Output.Embeddings))
	for _, item := range raw.Output.Embeddings {
		embeddings = append(embeddings, append(types.Embedding(nil), item.Embedding...))
	}
	tokens := 0
	for _, key := range []string{"total_tokens", "input_tokens", "image_tokens"} {
		tokens += raw.Usage[key]
	}
	return &parsedResponse{embeddings: embeddings, tokens: tokens}, nil
}

func mergeExtraParameters(body, parameters map[string]any) {
	for key, value := range parameters {
		if _, exists := body[key]; exists {
			continue
		}
		body[key] = value
	}
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

var (
	_ asembedding.EmbeddingModel = (*TextModel)(nil)
	_ asembedding.EmbeddingModel = (*MultiModalModel)(nil)
)
