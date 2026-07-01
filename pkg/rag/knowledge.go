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

package rag

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/yuluo-yx/agentscope-go/pkg/embedding"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

const defaultSearchTopK = 5

// KnowledgeBase embeds chunks and delegates persistence/search to a VectorStore.
type KnowledgeBase struct {
	name        string
	description string
	model       embedding.EmbeddingModel
	store       VectorStore
	collection  string

	metadataFilter map[string]any

	collectionMu    sync.Mutex
	collectionReady bool
}

// KnowledgeBaseOption configures KnowledgeBase.
type KnowledgeBaseOption func(*KnowledgeBase)

// InsertOption configures InsertDocument.
type InsertOption func(*insertOptions)

// SearchOption configures Search.
type SearchOption func(*searchOptions)

type insertOptions struct {
	documentID string
	metadata   map[string]any
}

type searchOptions struct {
	topK              int
	scoreThreshold    float64
	hasScoreThreshold bool
}

// NewKnowledgeBase creates a knowledge base over an embedding model and vector store.
func NewKnowledgeBase(
	name string,
	description string,
	model embedding.EmbeddingModel,
	store VectorStore,
	collection string,
	opts ...KnowledgeBaseOption,
) (*KnowledgeBase, error) {
	kb := &KnowledgeBase{
		name:            strings.TrimSpace(name),
		description:     strings.TrimSpace(description),
		model:           model,
		store:           store,
		collection:      strings.TrimSpace(collection),
		metadataFilter:  map[string]any{},
		collectionReady: false,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(kb)
		}
	}
	if kb.name == "" {
		return nil, fmt.Errorf("%w: knowledge base name is required", ErrInvalidInput)
	}
	if kb.collection == "" {
		return nil, fmt.Errorf("%w: collection name is required", ErrInvalidInput)
	}
	if kb.model == nil {
		return nil, fmt.Errorf("%w: embedding model is required", ErrInvalidInput)
	}
	if kb.model.Dimensions() <= 0 {
		return nil, fmt.Errorf("%w: embedding dimensions must be positive", ErrInvalidInput)
	}
	if kb.store == nil {
		return nil, fmt.Errorf("%w: vector store is required", ErrInvalidInput)
	}
	if kb.metadataFilter == nil {
		kb.metadataFilter = map[string]any{}
	}
	return kb, nil
}

// WithMetadataFilter scopes all vector-store operations to fixed metadata.
func WithMetadataFilter(metadata map[string]any) KnowledgeBaseOption {
	return func(kb *KnowledgeBase) {
		kb.metadataFilter = utils.CloneAnyMap(metadata)
	}
}

// Name returns the knowledge base name.
func (kb *KnowledgeBase) Name() string {
	if kb == nil {
		return ""
	}
	return kb.name
}

// Description returns the knowledge base description.
func (kb *KnowledgeBase) Description() string {
	if kb == nil {
		return ""
	}
	return kb.description
}

// Collection returns the vector-store collection name.
func (kb *KnowledgeBase) Collection() string {
	if kb == nil {
		return ""
	}
	return kb.collection
}

// WithDocumentID sets the document id used for inserted chunks.
func WithDocumentID(documentID string) InsertOption {
	return func(opts *insertOptions) {
		opts.documentID = strings.TrimSpace(documentID)
	}
}

// WithDocumentMetadata sets metadata propagated to every inserted chunk.
func WithDocumentMetadata(metadata map[string]any) InsertOption {
	return func(opts *insertOptions) {
		opts.metadata = utils.CloneAnyMap(metadata)
	}
}

// WithSearchTopK sets the maximum number of merged search results.
func WithSearchTopK(topK int) SearchOption {
	return func(opts *searchOptions) {
		opts.topK = topK
	}
}

// WithScoreThreshold filters search results below the provided score.
func WithScoreThreshold(score float64) SearchOption {
	return func(opts *searchOptions) {
		opts.scoreThreshold = score
		opts.hasScoreThreshold = true
	}
}

// InsertDocument embeds and stores chunks as one logical document.
func (kb *KnowledgeBase) InsertDocument(ctx context.Context, chunks []Chunk, opts ...InsertOption) (string, error) {
	if kb == nil {
		return "", fmt.Errorf("%w: knowledge base is nil", ErrInvalidInput)
	}
	if len(chunks) == 0 {
		return "", fmt.Errorf("%w: at least one chunk is required", ErrInvalidInput)
	}
	if err := kb.ensureCollection(ctx); err != nil {
		return "", err
	}

	cfg := insertOptions{documentID: utils.NewID()}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.documentID == "" {
		cfg.documentID = utils.NewID()
	}

	inputs := make([]embedding.EmbeddingInput, 0, len(chunks))
	prepared := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		input, err := kb.embeddingInput(chunk.Content)
		if err != nil {
			return "", err
		}
		inputs = append(inputs, input)
		prepared = append(prepared, kb.chunkWithMetadata(chunk, cfg.metadata))
	}

	response, err := kb.model.Embed(ctx, embedding.EmbeddingRequest{Inputs: inputs})
	if err != nil {
		return "", err
	}
	if response == nil || len(response.Embeddings) != len(prepared) {
		return "", fmt.Errorf("%w: got %d embeddings for %d chunks", ErrEmbeddingMismatch, embeddingCount(response), len(prepared))
	}

	records := make([]VectorRecord, 0, len(prepared))
	for index, chunk := range prepared {
		records = append(records, VectorRecord{
			Vector:     cloneEmbedding(response.Embeddings[index]),
			DocumentID: cfg.documentID,
			Chunk:      chunk,
		})
	}
	if err := kb.store.Insert(ctx, kb.collection, records); err != nil {
		return "", err
	}
	return cfg.documentID, nil
}

// Search retrieves chunks relevant to the provided query blocks.
func (kb *KnowledgeBase) Search(ctx context.Context, queries message.ContentBlockList, opts ...SearchOption) ([]VectorSearchResult, error) {
	if kb == nil {
		return nil, fmt.Errorf("%w: knowledge base is nil", ErrInvalidInput)
	}
	cfg := newSearchOptions(opts)

	inputs, err := kb.queryInputs(queries)
	if err != nil {
		return nil, err
	}
	if len(inputs) == 0 {
		return nil, nil
	}
	if err := kb.ensureCollection(ctx); err != nil {
		return nil, err
	}

	response, err := kb.model.Embed(ctx, embedding.EmbeddingRequest{Inputs: inputs})
	if err != nil {
		return nil, err
	}
	if response == nil || len(response.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("%w: got %d embeddings for %d queries", ErrEmbeddingMismatch, embeddingCount(response), len(inputs))
	}

	return kb.searchEmbeddings(ctx, response.Embeddings, cfg)
}

func newSearchOptions(opts []SearchOption) searchOptions {
	cfg := searchOptions{topK: defaultSearchTopK}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.topK <= 0 {
		cfg.topK = defaultSearchTopK
	}
	return cfg
}

func (kb *KnowledgeBase) searchEmbeddings(
	ctx context.Context,
	embeddings []types.Embedding,
	cfg searchOptions,
) ([]VectorSearchResult, error) {
	merged := map[string]VectorSearchResult{}
	for _, vector := range embeddings {
		results, err := kb.store.Search(ctx, kb.collection, cloneEmbedding(vector), cfg.topK, kb.metadataScope())
		if err != nil {
			return nil, err
		}
		mergeSearchResults(merged, results, cfg)
	}

	return sortedSearchResults(merged, cfg.topK), nil
}

func mergeSearchResults(
	merged map[string]VectorSearchResult,
	results []VectorSearchResult,
	cfg searchOptions,
) {
	for _, result := range results {
		if cfg.hasScoreThreshold && result.Score < cfg.scoreThreshold {
			continue
		}
		key := resultKey(result)
		current, exists := merged[key]
		if !exists || result.Score > current.Score {
			merged[key] = result.Clone()
		}
	}
}

func sortedSearchResults(merged map[string]VectorSearchResult, topK int) []VectorSearchResult {
	out := make([]VectorSearchResult, 0, len(merged))
	for _, result := range merged {
		out = append(out, result)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	if len(out) > topK {
		out = out[:topK]
	}
	return cloneResults(out)
}

// DeleteDocument removes one logical document from the vector store.
func (kb *KnowledgeBase) DeleteDocument(ctx context.Context, documentID string) error {
	if kb == nil {
		return fmt.Errorf("%w: knowledge base is nil", ErrInvalidInput)
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return fmt.Errorf("%w: document id is required", ErrInvalidInput)
	}
	return kb.store.Delete(ctx, kb.collection, documentID)
}

// ListDocuments lists documents visible in the knowledge base metadata scope.
func (kb *KnowledgeBase) ListDocuments(ctx context.Context) ([]DocumentSummary, error) {
	if kb == nil {
		return nil, fmt.Errorf("%w: knowledge base is nil", ErrInvalidInput)
	}
	documents, err := kb.store.ListDocuments(ctx, kb.collection, kb.metadataScope())
	if err != nil {
		return nil, err
	}
	return cloneDocumentSummaries(documents), nil
}

func (kb *KnowledgeBase) ensureCollection(ctx context.Context) error {
	kb.collectionMu.Lock()
	defer kb.collectionMu.Unlock()

	if kb.collectionReady {
		return nil
	}
	exists, err := kb.store.HasCollection(ctx, kb.collection)
	if err != nil {
		return err
	}
	if !exists {
		if err := kb.store.CreateCollection(ctx, kb.collection, kb.model.Dimensions()); err != nil {
			return err
		}
	}
	kb.collectionReady = true
	return nil
}

func (kb *KnowledgeBase) chunkWithMetadata(chunk Chunk, documentMetadata map[string]any) Chunk {
	out := chunk.Clone()
	out.Metadata = mergeMetadata(documentMetadata, out.Metadata, kb.metadataFilter)
	return out
}

func (kb *KnowledgeBase) metadataScope() map[string]any {
	return utils.CloneAnyMap(kb.metadataFilter)
}

func mergeMetadata(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, current := range maps {
		for key, value := range current {
			out[key] = utils.CloneAny(value)
		}
	}
	return out
}

func (kb *KnowledgeBase) queryInputs(queries message.ContentBlockList) ([]embedding.EmbeddingInput, error) {
	inputs := make([]embedding.EmbeddingInput, 0, len(queries))
	for _, block := range queries {
		input, err := kb.embeddingInput(block)
		if err != nil {
			if errors.Is(err, ErrUnsupportedContent) {
				continue
			}
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func (kb *KnowledgeBase) embeddingInput(block message.ContentBlock) (embedding.EmbeddingInput, error) {
	if block == nil {
		return embedding.EmbeddingInput{}, fmt.Errorf("%w: content block is nil", ErrInvalidInput)
	}
	switch typed := block.(type) {
	case *message.TextBlock:
		if strings.TrimSpace(typed.Text) == "" {
			return embedding.EmbeddingInput{}, fmt.Errorf("%w: text content is empty", ErrInvalidInput)
		}
		if !kb.supports(embedding.ModalityText) {
			return embedding.EmbeddingInput{}, fmt.Errorf("%w: embedding model does not support text", ErrUnsupportedContent)
		}
		return embedding.NewTextInput(typed.Text), nil
	case *message.DataBlock:
		return kb.dataEmbeddingInput(typed)
	default:
		return embedding.EmbeddingInput{}, fmt.Errorf("%w: %s blocks cannot be embedded", ErrUnsupportedContent, block.BlockType())
	}
}

func (kb *KnowledgeBase) dataEmbeddingInput(block *message.DataBlock) (embedding.EmbeddingInput, error) {
	if block == nil || block.Source == nil {
		return embedding.EmbeddingInput{}, fmt.Errorf("%w: data source is required", ErrInvalidInput)
	}
	switch source := block.Source.(type) {
	case *message.Base64Source:
		if strings.HasPrefix(source.MediaType, "image/") && kb.supports(embedding.ModalityImage) {
			return embedding.NewImageBase64Input(source.Data, source.MediaType), nil
		}
	case *message.URLSource:
		if strings.HasPrefix(source.MediaType, "image/") && kb.supports(embedding.ModalityImage) {
			return embedding.NewImageURLInput(source.URL, source.MediaType), nil
		}
		if strings.HasPrefix(source.MediaType, "video/") && kb.supports(embedding.ModalityVideo) {
			return embedding.NewVideoURLInput(source.URL, source.MediaType), nil
		}
	}
	return embedding.EmbeddingInput{}, fmt.Errorf("%w: data block media type is not supported by embedding model", ErrUnsupportedContent)
}

func (kb *KnowledgeBase) supports(modality embedding.Modality) bool {
	for _, supported := range kb.model.SupportedModalities() {
		if supported == modality {
			return true
		}
	}
	return false
}

func embeddingCount(response *embedding.EmbeddingResponse) int {
	if response == nil {
		return 0
	}
	return len(response.Embeddings)
}

func resultKey(result VectorSearchResult) string {
	return fmt.Sprintf("%s:%d", result.DocumentID, result.Chunk.ChunkIndex)
}
