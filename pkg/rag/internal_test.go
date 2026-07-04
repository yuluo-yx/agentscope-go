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
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/embedding"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestInternalMemoryVectorStoreBranches(t *testing.T) {
	ctx := context.Background()
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	store := NewMemoryVectorStore()
	if err := store.CreateCollection(canceled, "docs", 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
	if err := store.CreateCollection(ctx, " ", 2); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid collection name, got %v", err)
	}
	if err := store.CreateCollection(ctx, "docs", 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid dimensions, got %v", err)
	}

	var nilStore *MemoryVectorStore
	if err := nilStore.CreateCollection(ctx, "docs", 2); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil CreateCollection should fail with ErrInvalidInput, got %v", err)
	}
	if err := nilStore.DeleteCollection(ctx, "docs"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil DeleteCollection should fail with ErrInvalidInput, got %v", err)
	}
	if _, err := nilStore.HasCollection(ctx, "docs"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil HasCollection should fail with ErrInvalidInput, got %v", err)
	}
	if err := nilStore.Insert(ctx, "docs", []VectorRecord{internalMemoryRecord("doc", 0, types.Embedding{1, 0})}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil Insert should fail with ErrInvalidInput, got %v", err)
	}
	if err := nilStore.Delete(ctx, "docs", "doc"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil Delete should fail with ErrInvalidInput, got %v", err)
	}
	if _, err := nilStore.Search(ctx, "docs", types.Embedding{1, 0}, 1, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil Search should fail with ErrInvalidInput, got %v", err)
	}
	if _, err := nilStore.ListDocuments(ctx, "docs", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil ListDocuments should fail with ErrInvalidInput, got %v", err)
	}

	bare := &MemoryVectorStore{}
	if err := bare.CreateCollection(ctx, "docs", 2); err != nil {
		t.Fatalf("CreateCollection with nil map returned error: %v", err)
	}
	if err := bare.Insert(ctx, "docs", nil); err != nil {
		t.Fatalf("empty Insert should be a no-op: %v", err)
	}
	if err := bare.Insert(ctx, "missing", []VectorRecord{internalMemoryRecord("doc", 0, types.Embedding{1, 0})}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing collection should fail, got %v", err)
	}
	if err := bare.Insert(ctx, "docs", []VectorRecord{internalMemoryRecord("", 0, types.Embedding{1, 0})}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty document id should fail, got %v", err)
	}
	if err := bare.Insert(ctx, "docs", []VectorRecord{{
		DocumentID: "doc",
		Vector:     types.Embedding{1, 0},
		Chunk:      Chunk{Source: "nil.md"},
	}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil chunk content should fail, got %v", err)
	}

	records := []VectorRecord{
		internalMemoryRecord("doc-b", 0, types.Embedding{1, 0}),
		internalMemoryRecord("doc-a", 1, types.Embedding{1, 0}),
		internalMemoryRecord("doc-a", 0, types.Embedding{1, 0}),
		internalMemoryRecord("doc-zero", 0, types.Embedding{0, 0}),
	}
	if err := bare.Insert(ctx, "docs", records); err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	results, err := bare.Search(ctx, "docs", types.Embedding{1, 0}, 10, nil)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	gotOrder := []string{
		results[0].DocumentID + ":0",
		results[1].DocumentID + ":1",
		results[2].DocumentID + ":0",
		results[3].DocumentID + ":0",
	}
	if !reflect.DeepEqual(gotOrder, []string{"doc-a:0", "doc-a:1", "doc-b:0", "doc-zero:0"}) {
		t.Fatalf("search tie ordering mismatch: %#v", gotOrder)
	}
	if results[3].Score != 0 {
		t.Fatalf("zero vector similarity should be 0, got %v", results[3].Score)
	}
	if results, err := bare.Search(ctx, "docs", types.Embedding{1, 0}, 0, nil); err != nil || results != nil {
		t.Fatalf("topK <= 0 should return nil results without error, got %#v err=%v", results, err)
	}
	if results, err := bare.Search(ctx, "docs", types.Embedding{1, 0}, 10, map[string]any{"missing": true}); err != nil || len(results) != 0 {
		t.Fatalf("metadata miss should return no results, got %#v err=%v", results, err)
	}
	if _, err := bare.Search(ctx, " ", types.Embedding{1, 0}, 1, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank collection search should fail, got %v", err)
	}
	if _, err := bare.ListDocuments(ctx, "missing", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing collection list should fail, got %v", err)
	}
	if documents, err := bare.ListDocuments(ctx, "docs", map[string]any{"missing": true}); err != nil || len(documents) != 0 {
		t.Fatalf("metadata miss should list no documents, got %#v err=%v", documents, err)
	}
	if err := bare.Delete(ctx, "docs", " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank document delete should fail, got %v", err)
	}
	if err := bare.Delete(ctx, " ", "doc-a"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank collection delete should fail, got %v", err)
	}
	if err := bare.DeleteCollection(ctx, "docs"); err != nil {
		t.Fatalf("DeleteCollection returned error: %v", err)
	}
	if exists, err := bare.HasCollection(ctx, "docs"); err != nil || exists {
		t.Fatalf("collection should be deleted, exists=%t err=%v", exists, err)
	}
}

func TestInternalParserChunkerAndCloneBranches(t *testing.T) {
	if got := splitText("", 4, 0); got != nil {
		t.Fatalf("empty splitText should return nil, got %#v", got)
	}
	if got := splitText("abcdef", 4, 2); !reflect.DeepEqual(got, []string{"abcd", "cdef"}) {
		t.Fatalf("splitText overlap mismatch: %#v", got)
	}

	var nilChunker *ApproxTokenChunker
	if _, err := nilChunker.Chunk(context.Background(), nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil chunker should fail, got %v", err)
	}
	chunker, err := NewApproxTokenChunker(WithChunkSize(2), WithChunkOverlap(1))
	if err != nil {
		t.Fatalf("NewApproxTokenChunker returned error: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := chunker.Chunk(canceled, []Section{{Content: message.NewTextBlock("hello")}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled chunk should fail, got %v", err)
	}
	if _, err := chunker.Chunk(context.Background(), []Section{{}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil section content should fail, got %v", err)
	}
	if _, err := chunker.Chunk(context.Background(), []Section{{Content: message.NewThinkingBlock("hidden")}}); !errors.Is(err, ErrUnsupportedContent) {
		t.Fatalf("unsupported content should fail, got %v", err)
	}

	if _, err := ParseFileAs(context.Background(), nil, "guide.txt", "guide.txt"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil parser should fail, got %v", err)
	}
	if _, err := ParseFileAs(canceled, NewTextParser(), "guide.txt", "guide.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ParseFileAs should fail, got %v", err)
	}
	if _, err := ParseFileAs(context.Background(), NewTextParser(), " ", "guide.txt"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank path should fail, got %v", err)
	}
	path := filepath.Join(t.TempDir(), "guide.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if sections, err := ParseFileAs(context.Background(), NewTextParser(), path, "source.txt"); err != nil || sections[0].Source != "source.txt" {
		t.Fatalf("ParseFileAs source mismatch: sections=%#v err=%v", sections, err)
	}

	var textParser *TextParser
	if sections, err := textParser.Parse(context.Background(), []byte("hello"), "nil.txt"); err != nil || sections[0].Source != "nil.txt" {
		t.Fatalf("nil text parser receiver should parse UTF-8: sections=%#v err=%v", sections, err)
	}
	imageParser := NewImageParser()
	if _, err := imageParser.Parse(context.Background(), []byte("data"), " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank image filename should fail, got %v", err)
	}
	if _, err := imageParser.Parse(context.Background(), nil, "empty.jpg"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty image should fail, got %v", err)
	}
	for _, tt := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "jpeg", data: []byte("\xff\xd8jpeg"), want: "image/jpeg"},
		{name: "gif", data: []byte("GIF89aimage"), want: "image/gif"},
		{name: "bmp", data: []byte("BMimage"), want: "image/bmp"},
		{name: "webp", data: []byte("RIFFxxxxWEBPimage"), want: "image/webp"},
		{name: "fallback", data: []byte("unknown"), want: "image/jpeg"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sections, err := imageParser.Parse(context.Background(), tt.data, tt.name)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if sections[0].Metadata["media_type"] != tt.want {
				t.Fatalf("media type = %q, want %q", sections[0].Metadata["media_type"], tt.want)
			}
		})
	}

	if (Section{}).Clone().Content != nil {
		t.Fatal("cloning empty section should keep nil content")
	}
	if cloneEmbedding(nil) != nil || cloneResults(nil) != nil || cloneDocumentSummaries(nil) != nil {
		t.Fatal("nil clone helpers should return nil")
	}
}

func TestInternalKnowledgeBaseValidationAndEmbeddingInputs(t *testing.T) {
	model := internalKnowledgeEmbeddingModel{
		dimensions: 4,
		modalities: []embedding.Modality{
			embedding.ModalityText,
			embedding.ModalityImage,
			embedding.ModalityVideo,
		},
	}
	store := &internalKnowledgeVectorStore{hasCollection: true}
	kb, err := NewKnowledgeBase(" kb ", " desc ", model, store, " collection ", WithMetadataFilter(nil))
	if err != nil {
		t.Fatalf("NewKnowledgeBase returned error: %v", err)
	}
	if kb.Name() != "kb" || kb.Description() != "desc" || kb.Collection() != "collection" {
		t.Fatalf("knowledge base accessors mismatch: name=%q desc=%q collection=%q", kb.Name(), kb.Description(), kb.Collection())
	}
	var nilKB *KnowledgeBase
	if nilKB.Name() != "" || nilKB.Description() != "" || nilKB.Collection() != "" {
		t.Fatal("nil knowledge base accessors should return empty strings")
	}

	for _, tt := range []struct {
		name       string
		kbName     string
		collection string
		model      embedding.EmbeddingModel
		store      VectorStore
	}{
		{name: "blank name", kbName: " ", collection: "docs", model: model, store: store},
		{name: "blank collection", kbName: "kb", collection: " ", model: model, store: store},
		{name: "nil model", kbName: "kb", collection: "docs", store: store},
		{name: "zero dimensions", kbName: "kb", collection: "docs", model: internalKnowledgeEmbeddingModel{}, store: store},
		{name: "nil store", kbName: "kb", collection: "docs", model: model},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewKnowledgeBase(tt.kbName, "desc", tt.model, tt.store, tt.collection)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}

	imageInput, err := kb.embeddingInput(message.NewDataBlock(message.NewBase64Source("AA==", "image/png")))
	if err != nil {
		t.Fatalf("image base64 embeddingInput returned error: %v", err)
	}
	if imageInput.Type != embedding.ModalityImage || imageInput.Source.Type != embedding.SourceBase64 {
		t.Fatalf("image base64 input mismatch: %#v", imageInput)
	}
	urlImageInput, err := kb.embeddingInput(message.NewDataBlock(message.NewURLSource("https://example.test/a.png", "image/png")))
	if err != nil {
		t.Fatalf("image URL embeddingInput returned error: %v", err)
	}
	if urlImageInput.Type != embedding.ModalityImage || urlImageInput.Source.Type != embedding.SourceURL {
		t.Fatalf("image URL input mismatch: %#v", urlImageInput)
	}
	videoInput, err := kb.embeddingInput(message.NewDataBlock(message.NewURLSource("https://example.test/a.mp4", "video/mp4")))
	if err != nil {
		t.Fatalf("video URL embeddingInput returned error: %v", err)
	}
	if videoInput.Type != embedding.ModalityVideo || videoInput.Source.URL == "" {
		t.Fatalf("video URL input mismatch: %#v", videoInput)
	}
	if _, err := kb.embeddingInput(message.NewDataBlock(nil)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil data source should fail, got %v", err)
	}
	if _, err := kb.embeddingInput(message.NewDataBlock(message.NewBase64Source("AA==", "application/octet-stream"))); !errors.Is(err, ErrUnsupportedContent) {
		t.Fatalf("unsupported data source should fail, got %v", err)
	}

	textOnly, err := NewKnowledgeBase(
		"text",
		"desc",
		internalKnowledgeEmbeddingModel{dimensions: 4, modalities: []embedding.Modality{embedding.ModalityText}},
		store,
		"text",
	)
	if err != nil {
		t.Fatalf("NewKnowledgeBase text-only returned error: %v", err)
	}
	inputs, err := textOnly.queryInputs(message.ContentBlockList{
		message.NewDataBlock(message.NewBase64Source("AA==", "image/png")),
		message.NewTextBlock("hello"),
	})
	if err != nil {
		t.Fatalf("queryInputs should skip unsupported blocks, got %v", err)
	}
	if len(inputs) != 1 || inputs[0].Text != "hello" {
		t.Fatalf("queryInputs mismatch: %#v", inputs)
	}
	results, err := textOnly.Search(context.Background(), message.ContentBlockList{
		message.NewDataBlock(message.NewBase64Source("AA==", "image/png")),
	})
	if err != nil || results != nil {
		t.Fatalf("Search with only unsupported queries should return nil results, got %#v err=%v", results, err)
	}
	if _, err := textOnly.Search(context.Background(), message.ContentBlockList{message.NewTextBlock(" ")}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank text query should fail, got %v", err)
	}
	if count := embeddingCount(nil); count != 0 {
		t.Fatalf("nil embedding response count = %d", count)
	}
}

func internalMemoryRecord(documentID string, chunkIndex int, vector types.Embedding) VectorRecord {
	return VectorRecord{
		Vector:     vector,
		DocumentID: documentID,
		Chunk: Chunk{
			Content:    message.NewTextBlock(documentID),
			Source:     documentID + ".md",
			ChunkIndex: chunkIndex,
			Metadata:   map[string]any{"tenant": "t1"},
		},
	}
}

type internalKnowledgeEmbeddingModel struct {
	dimensions int
	modalities []embedding.Modality
	embeddings []types.Embedding
}

func (m internalKnowledgeEmbeddingModel) Name() string {
	return "internal-knowledge-embedding"
}

func (m internalKnowledgeEmbeddingModel) Dimensions() int {
	return m.dimensions
}

func (m internalKnowledgeEmbeddingModel) SupportedModalities() []embedding.Modality {
	if len(m.modalities) == 0 {
		return []embedding.Modality{embedding.ModalityText}
	}
	return append([]embedding.Modality(nil), m.modalities...)
}

func (m internalKnowledgeEmbeddingModel) Embed(context.Context, embedding.EmbeddingRequest) (*embedding.EmbeddingResponse, error) {
	if len(m.embeddings) == 0 {
		return embedding.NewEmbeddingResponse([]types.Embedding{{1, 0, 0, 0}}), nil
	}
	return embedding.NewEmbeddingResponse(m.embeddings), nil
}

type internalKnowledgeVectorStore struct {
	hasCollection bool
}

func (s *internalKnowledgeVectorStore) CreateCollection(context.Context, string, int) error {
	s.hasCollection = true
	return nil
}

func (s *internalKnowledgeVectorStore) DeleteCollection(context.Context, string) error {
	s.hasCollection = false
	return nil
}

func (s *internalKnowledgeVectorStore) HasCollection(context.Context, string) (bool, error) {
	return s.hasCollection, nil
}

func (s *internalKnowledgeVectorStore) Insert(context.Context, string, []VectorRecord) error {
	return nil
}

func (s *internalKnowledgeVectorStore) Delete(context.Context, string, string) error {
	return nil
}

func (s *internalKnowledgeVectorStore) Search(
	context.Context,
	string,
	types.Embedding,
	int,
	map[string]any,
) ([]VectorSearchResult, error) {
	return nil, nil
}

func (s *internalKnowledgeVectorStore) ListDocuments(context.Context, string, map[string]any) ([]DocumentSummary, error) {
	return nil, nil
}
