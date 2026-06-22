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

package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/types"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// Modality represents an embedding input modality.
type Modality string

const (
	// ModalityText represents text input.
	ModalityText Modality = "text"
	// ModalityImage represents image input.
	ModalityImage Modality = "image"
	// ModalityVideo represents video input.
	ModalityVideo Modality = "video"
)

// SourceType represents a binary or remote media source type.
type SourceType string

const (
	// SourceURL represents a URL media source.
	SourceURL SourceType = "url"
	// SourceBase64 represents a base64 media source.
	SourceBase64 SourceType = "base64"
)

const (
	// ResponseTypeEmbedding is the embedding response type.
	ResponseTypeEmbedding = "embedding"
	// UsageTypeEmbedding is the embedding usage type.
	UsageTypeEmbedding = "embedding"
)

// ResponseSource represents the source of an embedding response.
type ResponseSource string

const (
	// SourceAPI indicates that the response came from a provider API.
	SourceAPI ResponseSource = "api"
	// SourceCache indicates that the response came from cache.
	SourceCache ResponseSource = "cache"
)

// EmbeddingSource describes an image or video input source.
type EmbeddingSource struct {
	Type      SourceType `json:"type"`
	URL       string     `json:"url,omitempty"`
	Data      string     `json:"data,omitempty"`
	MediaType string     `json:"media_type,omitempty"`
}

// Clone returns a deep copy of the source.
func (s *EmbeddingSource) Clone() *EmbeddingSource {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

// EmbeddingInput is a unified embedding input item.
type EmbeddingInput struct {
	Type   Modality         `json:"type"`
	Text   string           `json:"text,omitempty"`
	Source *EmbeddingSource `json:"source,omitempty"`
}

// NewTextInput creates a text input.
func NewTextInput(text string) EmbeddingInput {
	return EmbeddingInput{Type: ModalityText, Text: text}
}

// NewImageURLInput creates an image URL input.
func NewImageURLInput(url, mediaType string) EmbeddingInput {
	return EmbeddingInput{
		Type: ModalityImage,
		Source: &EmbeddingSource{
			Type:      SourceURL,
			URL:       url,
			MediaType: mediaType,
		},
	}
}

// NewImageBase64Input creates an image base64 input.
func NewImageBase64Input(data, mediaType string) EmbeddingInput {
	return EmbeddingInput{
		Type: ModalityImage,
		Source: &EmbeddingSource{
			Type:      SourceBase64,
			Data:      data,
			MediaType: mediaType,
		},
	}
}

// NewVideoURLInput creates a video URL input.
func NewVideoURLInput(url, mediaType string) EmbeddingInput {
	return EmbeddingInput{
		Type: ModalityVideo,
		Source: &EmbeddingSource{
			Type:      SourceURL,
			URL:       url,
			MediaType: mediaType,
		},
	}
}

// Clone returns a deep copy of the input item.
func (i EmbeddingInput) Clone() EmbeddingInput {
	cp := i
	cp.Source = i.Source.Clone()
	return cp
}

// EmbeddingRequest is the unified request for an embedding call.
type EmbeddingRequest struct {
	Inputs     []EmbeddingInput `json:"inputs"`
	Parameters map[string]any   `json:"parameters,omitempty"`
	Metadata   map[string]any   `json:"metadata,omitempty"`
}

// Clone returns a deep copy of the request.
func (r EmbeddingRequest) Clone() EmbeddingRequest {
	cp := r
	if r.Inputs != nil {
		cp.Inputs = make([]EmbeddingInput, 0, len(r.Inputs))
		for _, input := range r.Inputs {
			cp.Inputs = append(cp.Inputs, input.Clone())
		}
	}
	cp.Parameters = utils.CloneAnyMap(r.Parameters)
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	return cp
}

// Validate checks whether the request contains only provider-supported modalities.
func (r EmbeddingRequest) Validate(supported ...Modality) error {
	if len(r.Inputs) == 0 {
		return fmt.Errorf("%w: inputs must not be empty", ErrInvalidEmbeddingInput)
	}
	allowed := map[Modality]struct{}{}
	for _, modality := range supported {
		allowed[modality] = struct{}{}
	}
	for _, input := range r.Inputs {
		if len(allowed) > 0 {
			if _, ok := allowed[input.Type]; !ok {
				return fmt.Errorf("%w: %s", ErrUnsupportedModality, input.Type)
			}
		}
		if err := validateInput(input); err != nil {
			return err
		}
	}
	return nil
}

func validateInput(input EmbeddingInput) error {
	switch input.Type {
	case ModalityText:
		if input.Text == "" {
			return fmt.Errorf("%w: text must not be empty", ErrInvalidEmbeddingInput)
		}
	case ModalityImage:
		return validateMediaSource(input.Source, false)
	case ModalityVideo:
		if input.Source != nil && input.Source.Type == SourceBase64 {
			return fmt.Errorf("%w: video only supports URL source", ErrInvalidEmbeddingInput)
		}
		return validateMediaSource(input.Source, true)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedModality, input.Type)
	}
	return nil
}

func validateMediaSource(source *EmbeddingSource, requireURL bool) error {
	if source == nil {
		return fmt.Errorf("%w: media source is required", ErrInvalidEmbeddingInput)
	}
	switch source.Type {
	case SourceURL:
		if source.URL == "" {
			return fmt.Errorf("%w: source URL must not be empty", ErrInvalidEmbeddingInput)
		}
	case SourceBase64:
		if requireURL {
			return fmt.Errorf("%w: URL source is required", ErrInvalidEmbeddingInput)
		}
		if source.Data == "" || source.MediaType == "" {
			return fmt.Errorf("%w: base64 data and media type are required", ErrInvalidEmbeddingInput)
		}
	default:
		return fmt.Errorf("%w: unsupported source type %q", ErrInvalidEmbeddingInput, source.Type)
	}
	return nil
}

// EmbeddingModel is the unified interface for all embedding providers.
type EmbeddingModel interface {
	Name() string
	Dimensions() int
	SupportedModalities() []Modality
	Embed(ctx context.Context, request EmbeddingRequest) (*EmbeddingResponse, error)
}

// EmbeddingUsage records usage for an embedding call.
type EmbeddingUsage struct {
	Time   time.Duration `json:"time"`
	Tokens *int          `json:"tokens,omitempty"`
	Type   string        `json:"type"`
}

// Clone returns a deep copy of the usage.
func (u *EmbeddingUsage) Clone() *EmbeddingUsage {
	if u == nil {
		return nil
	}
	cp := *u
	if cp.Type == "" {
		cp.Type = UsageTypeEmbedding
	}
	if u.Tokens != nil {
		tokens := *u.Tokens
		cp.Tokens = &tokens
	}
	return &cp
}

// EmbeddingResponse is the unified embedding response.
type EmbeddingResponse struct {
	Embeddings []types.Embedding `json:"embeddings"`
	ID         string            `json:"id"`
	CreatedAt  string            `json:"created_at"`
	Type       string            `json:"type"`
	Usage      *EmbeddingUsage   `json:"usage,omitempty"`
	Source     ResponseSource    `json:"source"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
}

// EmbeddingResponseOption configures response fields.
type EmbeddingResponseOption func(*EmbeddingResponse)

// WithEmbeddingUsage sets usage.
func WithEmbeddingUsage(usage *EmbeddingUsage) EmbeddingResponseOption {
	return func(resp *EmbeddingResponse) {
		resp.Usage = usage.Clone()
	}
}

// WithEmbeddingSource sets the response source.
func WithEmbeddingSource(source ResponseSource) EmbeddingResponseOption {
	return func(resp *EmbeddingResponse) {
		resp.Source = source
	}
}

// WithEmbeddingMetadata sets response metadata.
func WithEmbeddingMetadata(metadata map[string]any) EmbeddingResponseOption {
	return func(resp *EmbeddingResponse) {
		resp.Metadata = utils.CloneAnyMap(metadata)
	}
}

// NewEmbeddingResponse creates an embedding response with default envelope fields.
func NewEmbeddingResponse(embeddings []types.Embedding, opts ...EmbeddingResponseOption) *EmbeddingResponse {
	resp := &EmbeddingResponse{
		Embeddings: cloneEmbeddings(embeddings),
		ID:         utils.NewID(),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Type:       ResponseTypeEmbedding,
		Source:     SourceAPI,
		Metadata:   map[string]any{},
	}
	for _, opt := range opts {
		opt(resp)
	}
	if resp.ID == "" {
		resp.ID = utils.NewID()
	}
	if resp.CreatedAt == "" {
		resp.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if resp.Type == "" {
		resp.Type = ResponseTypeEmbedding
	}
	if resp.Source == "" {
		resp.Source = SourceAPI
	}
	if resp.Metadata == nil {
		resp.Metadata = map[string]any{}
	}
	if resp.Usage != nil && resp.Usage.Type == "" {
		resp.Usage.Type = UsageTypeEmbedding
	}
	return resp
}

// Clone returns a deep copy of the response.
func (r *EmbeddingResponse) Clone() *EmbeddingResponse {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Embeddings = cloneEmbeddings(r.Embeddings)
	cp.Usage = r.Usage.Clone()
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	return &cp
}

func cloneEmbeddings(embeddings []types.Embedding) []types.Embedding {
	if embeddings == nil {
		return nil
	}
	out := make([]types.Embedding, 0, len(embeddings))
	for _, embedding := range embeddings {
		out = append(out, append(types.Embedding(nil), embedding...))
	}
	return out
}

// CacheIdentifier generates a stable cache key from provider, model, dimensions, and request content.
func CacheIdentifier(provider, model string, dimensions int, request EmbeddingRequest) string {
	payload := map[string]any{
		"provider":   provider,
		"model":      model,
		"dimensions": dimensions,
		"request":    request.Clone(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(fmt.Sprintf("%#v", payload))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
