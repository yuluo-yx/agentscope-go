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
	"math"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestMemoryVectorStoreSearchListAndDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := rag.NewMemoryVectorStore()
	hasCollection, err := store.HasCollection(ctx, "docs")
	if err != nil {
		t.Fatalf("HasCollection returned error: %v", err)
	}
	if hasCollection {
		t.Fatal("collection should not exist before CreateCollection")
	}

	if err := store.CreateCollection(ctx, "docs", 3); err != nil {
		t.Fatalf("CreateCollection returned error: %v", err)
	}
	if err := store.CreateCollection(ctx, "docs", 3); err != nil {
		t.Fatalf("CreateCollection should be idempotent for matching dimensions: %v", err)
	}

	records := []rag.VectorRecord{
		memoryRecord("doc-1", 0, types.Embedding{1, 0, 0}, "PTO policy", map[string]any{
			"tenant_id": "tenant-1",
			"kind":      "policy",
		}),
		memoryRecord("doc-1", 1, types.Embedding{0, 1, 0}, "Travel policy", map[string]any{
			"tenant_id": "tenant-1",
			"kind":      "policy",
		}),
		memoryRecord("doc-2", 0, types.Embedding{0.6, 0.8, 0}, "PTO calendar", map[string]any{
			"tenant_id": "tenant-1",
			"kind":      "calendar",
		}),
		memoryRecord("doc-3", 0, types.Embedding{0.99, 0, 0}, "Other tenant", map[string]any{
			"tenant_id": "tenant-2",
			"kind":      "policy",
		}),
	}
	if err := store.Insert(ctx, "docs", records); err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}

	results, err := store.Search(ctx, "docs", types.Embedding{1, 0, 0}, 2, map[string]any{"tenant_id": "tenant-1"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected two search results, got %#v", results)
	}
	if results[0].DocumentID != "doc-1" || results[0].Chunk.ChunkIndex != 0 || math.Abs(results[0].Score-1) > 1e-9 {
		t.Fatalf("first result mismatch: %#v", results[0])
	}
	if results[1].DocumentID != "doc-2" || math.Abs(results[1].Score-0.6) > 1e-9 {
		t.Fatalf("second result mismatch: %#v", results[1])
	}

	documents, err := store.ListDocuments(ctx, "docs", map[string]any{"tenant_id": "tenant-1"})
	if err != nil {
		t.Fatalf("ListDocuments returned error: %v", err)
	}
	if len(documents) != 2 {
		t.Fatalf("expected two documents, got %#v", documents)
	}
	if documents[0].DocumentID != "doc-1" || documents[0].ChunkCount != 2 || documents[0].Metadata["kind"] != "policy" {
		t.Fatalf("first document summary mismatch: %#v", documents[0])
	}
	if documents[1].DocumentID != "doc-2" || documents[1].ChunkCount != 1 {
		t.Fatalf("second document summary mismatch: %#v", documents[1])
	}

	if err := store.Delete(ctx, "docs", "doc-1"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	documents, err = store.ListDocuments(ctx, "docs", map[string]any{"tenant_id": "tenant-1"})
	if err != nil {
		t.Fatalf("ListDocuments after delete returned error: %v", err)
	}
	if len(documents) != 1 || documents[0].DocumentID != "doc-2" {
		t.Fatalf("deleted document should not be listed: %#v", documents)
	}
}

func TestMemoryVectorStoreValidatesDimensions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := rag.NewMemoryVectorStore()
	if err := store.CreateCollection(ctx, "docs", 2); err != nil {
		t.Fatalf("CreateCollection returned error: %v", err)
	}
	if err := store.CreateCollection(ctx, "docs", 3); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for conflicting dimensions, got %v", err)
	}
	if err := store.Insert(ctx, "docs", []rag.VectorRecord{
		memoryRecord("doc-1", 0, types.Embedding{1}, "bad vector", nil),
	}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for insert dimension mismatch, got %v", err)
	}
	if _, err := store.Search(ctx, "docs", types.Embedding{1}, 1, nil); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for search dimension mismatch, got %v", err)
	}
}

func TestMemoryVectorStoreClonesStoredRecords(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := rag.NewMemoryVectorStore()
	if err := store.CreateCollection(ctx, "docs", 2); err != nil {
		t.Fatalf("CreateCollection returned error: %v", err)
	}

	record := memoryRecord("doc-1", 0, types.Embedding{1, 0}, "original", map[string]any{"kind": "policy"})
	if err := store.Insert(ctx, "docs", []rag.VectorRecord{record}); err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	record.Vector[0] = 0
	record.Chunk.Content.(*message.TextBlock).Text = "mutated"
	record.Chunk.Metadata["kind"] = "mutated"

	results, err := store.Search(ctx, "docs", types.Embedding{1, 0}, 1, nil)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got := results[0].Chunk.Content.(*message.TextBlock).Text; got != "original" {
		t.Fatalf("stored content was not cloned: %q", got)
	}
	results[0].Chunk.Content.(*message.TextBlock).Text = "changed result"
	results[0].Chunk.Metadata["kind"] = "changed"

	results, err = store.Search(ctx, "docs", types.Embedding{1, 0}, 1, nil)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got := results[0].Chunk.Content.(*message.TextBlock).Text; got != "original" {
		t.Fatalf("search results should be cloned: %q", got)
	}
	if got := results[0].Chunk.Metadata["kind"]; got != "policy" {
		t.Fatalf("search metadata should be cloned: %#v", results[0].Chunk.Metadata)
	}
}

func memoryRecord(
	documentID string,
	chunkIndex int,
	vector types.Embedding,
	text string,
	metadata map[string]any,
) rag.VectorRecord {
	return rag.VectorRecord{
		Vector:     vector,
		DocumentID: documentID,
		Chunk: rag.Chunk{
			Content:     message.NewTextBlock(text),
			Source:      documentID + ".md",
			ChunkIndex:  chunkIndex,
			TotalChunks: 1,
			Metadata:    metadata,
		},
	}
}
