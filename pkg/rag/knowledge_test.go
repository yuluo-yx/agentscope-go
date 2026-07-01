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

package rag_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/embedding"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestKnowledgeBaseInsertDocumentEmbedsChunksAndStoresScopedRecords(t *testing.T) {
	model := &recordingEmbeddingModel{
		dimensions: 3,
		embeddings: []types.Embedding{
			{1, 0, 0},
			{0, 1, 0},
		},
	}
	store := &recordingVectorStore{}
	kb, err := rag.NewKnowledgeBase(
		"handbook",
		"Company handbook.",
		model,
		store,
		"kb-handbook",
		rag.WithMetadataFilter(map[string]any{"tenant_id": "tenant-1"}),
	)
	if err != nil {
		t.Fatalf("NewKnowledgeBase returned error: %v", err)
	}

	documentID, err := kb.InsertDocument(
		context.Background(),
		[]rag.Chunk{
			{
				Content:     message.NewTextBlock("first chunk"),
				Source:      "handbook.md",
				ChunkIndex:  0,
				TotalChunks: 2,
				Metadata:    map[string]any{"category": "policy", "tenant_id": "wrong"},
			},
			{
				Content:     message.NewTextBlock("second chunk"),
				Source:      "handbook.md",
				ChunkIndex:  1,
				TotalChunks: 2,
				Metadata:    map[string]any{"section": "benefits"},
			},
		},
		rag.WithDocumentID("doc-1"),
		rag.WithDocumentMetadata(map[string]any{"category": "document", "filename": "handbook.md"}),
	)
	if err != nil {
		t.Fatalf("InsertDocument returned error: %v", err)
	}

	if documentID != "doc-1" {
		t.Fatalf("document id = %q, want doc-1", documentID)
	}
	if len(store.createCalls) != 1 || store.createCalls[0].name != "kb-handbook" || store.createCalls[0].dimensions != 3 {
		t.Fatalf("collection create calls mismatch: %#v", store.createCalls)
	}
	if got := model.inputTexts(); !reflect.DeepEqual(got, []string{"first chunk", "second chunk"}) {
		t.Fatalf("embedding inputs = %#v", got)
	}
	if len(store.inserted) != 2 {
		t.Fatalf("expected two inserted records, got %#v", store.inserted)
	}
	first := store.inserted[0]
	if first.DocumentID != "doc-1" || !reflect.DeepEqual(first.Vector, types.Embedding{1, 0, 0}) {
		t.Fatalf("first vector record mismatch: %#v", first)
	}
	if first.Chunk.Metadata["filename"] != "handbook.md" {
		t.Fatalf("document metadata was not propagated: %#v", first.Chunk.Metadata)
	}
	if first.Chunk.Metadata["category"] != "policy" {
		t.Fatalf("chunk metadata should override document metadata: %#v", first.Chunk.Metadata)
	}
	if first.Chunk.Metadata["tenant_id"] != "tenant-1" {
		t.Fatalf("metadata filter should override caller metadata: %#v", first.Chunk.Metadata)
	}
}

func TestKnowledgeBaseInsertDocumentRejectsEmbeddingCountMismatch(t *testing.T) {
	model := &recordingEmbeddingModel{
		dimensions: 3,
		embeddings: []types.Embedding{{1, 0, 0}},
	}
	store := &recordingVectorStore{}
	kb, err := rag.NewKnowledgeBase("kb", "desc", model, store, "collection")
	if err != nil {
		t.Fatalf("NewKnowledgeBase returned error: %v", err)
	}

	_, err = kb.InsertDocument(context.Background(), []rag.Chunk{
		{Content: message.NewTextBlock("one"), Source: "a.txt"},
		{Content: message.NewTextBlock("two"), Source: "a.txt"},
	})
	if !errors.Is(err, rag.ErrEmbeddingMismatch) {
		t.Fatalf("expected ErrEmbeddingMismatch, got %v", err)
	}
	if len(store.inserted) != 0 {
		t.Fatalf("records should not be inserted on mismatch: %#v", store.inserted)
	}
}

func TestKnowledgeBaseSearchDeduplicatesScoresAndAppliesThreshold(t *testing.T) {
	model := &recordingEmbeddingModel{
		dimensions: 3,
		embeddings: []types.Embedding{
			{1, 0, 0},
			{0, 1, 0},
		},
	}
	store := &recordingVectorStore{
		searchResults: [][]rag.VectorSearchResult{
			{
				searchResult("doc-1", 0, 0.70, "old"),
				searchResult("doc-2", 0, 0.90, "best"),
			},
			{
				searchResult("doc-1", 0, 0.95, "new"),
				searchResult("doc-3", 0, 0.40, "below threshold"),
			},
		},
	}
	kb, err := rag.NewKnowledgeBase(
		"kb",
		"desc",
		model,
		store,
		"collection",
		rag.WithMetadataFilter(map[string]any{"tenant_id": "tenant-1"}),
	)
	if err != nil {
		t.Fatalf("NewKnowledgeBase returned error: %v", err)
	}

	results, err := kb.Search(
		context.Background(),
		message.ContentBlockList{
			message.NewTextBlock("benefits"),
			message.NewTextBlock("pto"),
		},
		rag.WithSearchTopK(2),
		rag.WithScoreThreshold(0.5),
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if got := model.inputTexts(); !reflect.DeepEqual(got, []string{"benefits", "pto"}) {
		t.Fatalf("embedding inputs = %#v", got)
	}
	if len(store.searchCalls) != 2 {
		t.Fatalf("expected two vector searches, got %#v", store.searchCalls)
	}
	for _, call := range store.searchCalls {
		if call.collection != "collection" || call.topK != 2 || call.metadataFilter["tenant_id"] != "tenant-1" {
			t.Fatalf("search call mismatch: %#v", call)
		}
	}
	if len(results) != 2 {
		t.Fatalf("expected two merged results, got %#v", results)
	}
	if results[0].DocumentID != "doc-1" || results[0].Score != 0.95 || results[0].Chunk.Content.(*message.TextBlock).Text != "new" {
		t.Fatalf("first result mismatch: %#v", results[0])
	}
	if results[1].DocumentID != "doc-2" || results[1].Score != 0.90 {
		t.Fatalf("second result mismatch: %#v", results[1])
	}
}

func TestKnowledgeBaseDeleteAndListForwardToVectorStore(t *testing.T) {
	model := &recordingEmbeddingModel{dimensions: 3}
	store := &recordingVectorStore{
		documents: []rag.DocumentSummary{{DocumentID: "doc-1", Source: "a.txt", ChunkCount: 2}},
	}
	kb, err := rag.NewKnowledgeBase("kb", "desc", model, store, "collection")
	if err != nil {
		t.Fatalf("NewKnowledgeBase returned error: %v", err)
	}

	if err := kb.DeleteDocument(context.Background(), "doc-1"); err != nil {
		t.Fatalf("DeleteDocument returned error: %v", err)
	}
	docs, err := kb.ListDocuments(context.Background())
	if err != nil {
		t.Fatalf("ListDocuments returned error: %v", err)
	}

	if !reflect.DeepEqual(store.deleted, []string{"doc-1"}) {
		t.Fatalf("deleted documents mismatch: %#v", store.deleted)
	}
	if !reflect.DeepEqual(docs, store.documents) {
		t.Fatalf("documents mismatch: %#v", docs)
	}
}

func searchResult(documentID string, chunkIndex int, score float64, text string) rag.VectorSearchResult {
	return rag.VectorSearchResult{
		Score:      score,
		DocumentID: documentID,
		Chunk: rag.Chunk{
			Content:     message.NewTextBlock(text),
			Source:      documentID + ".txt",
			ChunkIndex:  chunkIndex,
			TotalChunks: 1,
		},
	}
}

type recordingEmbeddingModel struct {
	name                string
	dimensions          int
	supportedModalities []embedding.Modality
	embeddings          []types.Embedding
	requests            []embedding.EmbeddingRequest
	err                 error
}

func (m *recordingEmbeddingModel) Name() string {
	if m.name != "" {
		return m.name
	}
	return "recording-embedding"
}

func (m *recordingEmbeddingModel) Dimensions() int {
	return m.dimensions
}

func (m *recordingEmbeddingModel) SupportedModalities() []embedding.Modality {
	if len(m.supportedModalities) > 0 {
		return append([]embedding.Modality(nil), m.supportedModalities...)
	}
	return []embedding.Modality{embedding.ModalityText}
}

func (m *recordingEmbeddingModel) Embed(_ context.Context, request embedding.EmbeddingRequest) (*embedding.EmbeddingResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if m.err != nil {
		return nil, m.err
	}
	return embedding.NewEmbeddingResponse(m.embeddings), nil
}

func (m *recordingEmbeddingModel) inputTexts() []string {
	if len(m.requests) == 0 {
		return nil
	}
	texts := make([]string, 0, len(m.requests[len(m.requests)-1].Inputs))
	for _, input := range m.requests[len(m.requests)-1].Inputs {
		texts = append(texts, input.Text)
	}
	return texts
}

type recordingVectorStore struct {
	hasCollection bool
	createCalls   []createCollectionCall
	inserted      []rag.VectorRecord
	searchResults [][]rag.VectorSearchResult
	searchCalls   []searchCall
	deleted       []string
	documents     []rag.DocumentSummary
}

type createCollectionCall struct {
	name       string
	dimensions int
}

type searchCall struct {
	collection     string
	topK           int
	metadataFilter map[string]any
}

func (s *recordingVectorStore) CreateCollection(_ context.Context, name string, dimensions int) error {
	s.hasCollection = true
	s.createCalls = append(s.createCalls, createCollectionCall{name: name, dimensions: dimensions})
	return nil
}

func (s *recordingVectorStore) DeleteCollection(_ context.Context, name string) error {
	s.hasCollection = false
	return nil
}

func (s *recordingVectorStore) HasCollection(context.Context, string) (bool, error) {
	return s.hasCollection, nil
}

func (s *recordingVectorStore) Insert(_ context.Context, _ string, records []rag.VectorRecord) error {
	s.inserted = append([]rag.VectorRecord(nil), records...)
	return nil
}

func (s *recordingVectorStore) Delete(_ context.Context, _ string, documentID string) error {
	s.deleted = append(s.deleted, documentID)
	return nil
}

func (s *recordingVectorStore) Search(
	_ context.Context,
	collection string,
	_ types.Embedding,
	topK int,
	metadataFilter map[string]any,
) ([]rag.VectorSearchResult, error) {
	s.searchCalls = append(s.searchCalls, searchCall{
		collection:     collection,
		topK:           topK,
		metadataFilter: metadataFilter,
	})
	index := len(s.searchCalls) - 1
	if index >= len(s.searchResults) {
		return nil, nil
	}
	return s.searchResults[index], nil
}

func (s *recordingVectorStore) ListDocuments(_ context.Context, _ string, _ map[string]any) ([]rag.DocumentSummary, error) {
	return append([]rag.DocumentSummary(nil), s.documents...), nil
}
