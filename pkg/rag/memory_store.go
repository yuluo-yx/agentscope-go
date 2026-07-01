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
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/yuluo-yx/agentscope-go/pkg/types"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// MemoryVectorStore keeps vector records in process memory.
type MemoryVectorStore struct {
	mu          sync.RWMutex
	collections map[string]*memoryVectorCollection
}

type memoryVectorCollection struct {
	dimensions int
	records    []VectorRecord
}

// NewMemoryVectorStore creates an in-process VectorStore implementation.
func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{collections: map[string]*memoryVectorCollection{}}
}

// CreateCollection creates a collection or validates an existing collection's dimensions.
func (s *MemoryVectorStore) CreateCollection(ctx context.Context, name string, dimensions int) error {
	if err := checkMemoryStoreContext(ctx); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: collection name is required", ErrInvalidInput)
	}
	if dimensions <= 0 {
		return fmt.Errorf("%w: collection dimensions must be positive", ErrInvalidInput)
	}
	if s == nil {
		return fmt.Errorf("%w: memory vector store is nil", ErrInvalidInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCollectionsLocked()

	collection, exists := s.collections[name]
	if exists {
		if collection.dimensions != dimensions {
			return fmt.Errorf(
				"%w: collection %q already has %d dimensions, got %d",
				ErrInvalidInput,
				name,
				collection.dimensions,
				dimensions,
			)
		}
		return nil
	}
	s.collections[name] = &memoryVectorCollection{dimensions: dimensions}
	return nil
}

// DeleteCollection deletes an in-memory collection.
func (s *MemoryVectorStore) DeleteCollection(ctx context.Context, name string) error {
	if err := checkMemoryStoreContext(ctx); err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("%w: memory vector store is nil", ErrInvalidInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.collections, strings.TrimSpace(name))
	return nil
}

// HasCollection reports whether the named collection exists.
func (s *MemoryVectorStore) HasCollection(ctx context.Context, name string) (bool, error) {
	if err := checkMemoryStoreContext(ctx); err != nil {
		return false, err
	}
	if s == nil {
		return false, fmt.Errorf("%w: memory vector store is nil", ErrInvalidInput)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.collections[strings.TrimSpace(name)]
	return exists, nil
}

// Insert adds or replaces vector records in a collection.
func (s *MemoryVectorStore) Insert(ctx context.Context, collectionName string, records []VectorRecord) error {
	if err := checkMemoryStoreContext(ctx); err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	if s == nil {
		return fmt.Errorf("%w: memory vector store is nil", ErrInvalidInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	collection, err := s.collectionLocked(collectionName)
	if err != nil {
		return err
	}

	indexByKey := make(map[string]int, len(collection.records)+len(records))
	for index, record := range collection.records {
		indexByKey[memoryRecordKey(record)] = index
	}
	for _, record := range records {
		if err := validateMemoryRecord(record, collection.dimensions); err != nil {
			return err
		}

		record = record.Clone()
		key := memoryRecordKey(record)
		if index, exists := indexByKey[key]; exists {
			collection.records[index] = record
			continue
		}
		indexByKey[key] = len(collection.records)
		collection.records = append(collection.records, record)
	}
	return nil
}

// Delete removes one logical document from a collection.
func (s *MemoryVectorStore) Delete(ctx context.Context, collectionName string, documentID string) error {
	if err := checkMemoryStoreContext(ctx); err != nil {
		return err
	}
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return fmt.Errorf("%w: document id is required", ErrInvalidInput)
	}
	if s == nil {
		return fmt.Errorf("%w: memory vector store is nil", ErrInvalidInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	collection, err := s.collectionLocked(collectionName)
	if err != nil {
		return err
	}

	filtered := collection.records[:0]
	for _, record := range collection.records {
		if record.DocumentID != documentID {
			filtered = append(filtered, record)
		}
	}
	collection.records = filtered
	return nil
}

// Search returns topK records ranked by cosine similarity.
func (s *MemoryVectorStore) Search(
	ctx context.Context,
	collectionName string,
	queryVector types.Embedding,
	topK int,
	metadataFilter map[string]any,
) ([]VectorSearchResult, error) {
	if err := checkMemoryStoreContext(ctx); err != nil {
		return nil, err
	}
	if topK <= 0 {
		return nil, nil
	}
	if s == nil {
		return nil, fmt.Errorf("%w: memory vector store is nil", ErrInvalidInput)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	collection, err := s.collectionLocked(collectionName)
	if err != nil {
		return nil, err
	}
	if len(queryVector) != collection.dimensions {
		return nil, fmt.Errorf(
			"%w: query vector dimensions = %d, want %d",
			ErrInvalidInput,
			len(queryVector),
			collection.dimensions,
		)
	}

	results := make([]VectorSearchResult, 0, len(collection.records))
	for _, record := range collection.records {
		if !matchesMemoryMetadata(record.Chunk.Metadata, metadataFilter) {
			continue
		}
		results = append(results, VectorSearchResult{
			Score:      cosineSimilarity(queryVector, record.Vector),
			DocumentID: record.DocumentID,
			Chunk:      record.Chunk.Clone(),
		})
	}
	sortMemorySearchResults(results)
	if len(results) > topK {
		results = results[:topK]
	}
	return cloneResults(results), nil
}

// ListDocuments lists documents with at least one record matching the metadata filter.
func (s *MemoryVectorStore) ListDocuments(
	ctx context.Context,
	collectionName string,
	metadataFilter map[string]any,
) ([]DocumentSummary, error) {
	if err := checkMemoryStoreContext(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("%w: memory vector store is nil", ErrInvalidInput)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	collection, err := s.collectionLocked(collectionName)
	if err != nil {
		return nil, err
	}

	summaries := map[string]DocumentSummary{}
	for _, record := range collection.records {
		if !matchesMemoryMetadata(record.Chunk.Metadata, metadataFilter) {
			continue
		}
		summary := summaries[record.DocumentID]
		if summary.DocumentID == "" {
			summary = DocumentSummary{
				DocumentID: record.DocumentID,
				Source:     record.Chunk.Source,
				Metadata:   cloneMetadata(record.Chunk.Metadata),
			}
		}
		summary.ChunkCount++
		summaries[record.DocumentID] = summary
	}

	out := make([]DocumentSummary, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, summary)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].DocumentID < out[j].DocumentID
	})
	return cloneDocumentSummaries(out), nil
}

func (s *MemoryVectorStore) collectionLocked(name string) (*memoryVectorCollection, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: collection name is required", ErrInvalidInput)
	}
	collection, exists := s.collections[name]
	if !exists {
		return nil, fmt.Errorf("%w: collection %q does not exist", ErrInvalidInput, name)
	}
	return collection, nil
}

func (s *MemoryVectorStore) ensureCollectionsLocked() {
	if s.collections == nil {
		s.collections = map[string]*memoryVectorCollection{}
	}
}

func validateMemoryRecord(record VectorRecord, dimensions int) error {
	if strings.TrimSpace(record.DocumentID) == "" {
		return fmt.Errorf("%w: document id is required", ErrInvalidInput)
	}
	if len(record.Vector) != dimensions {
		return fmt.Errorf(
			"%w: record vector dimensions = %d, want %d",
			ErrInvalidInput,
			len(record.Vector),
			dimensions,
		)
	}
	if record.Chunk.Content == nil {
		return fmt.Errorf("%w: chunk content is required", ErrInvalidInput)
	}
	return nil
}

func checkMemoryStoreContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func memoryRecordKey(record VectorRecord) string {
	return record.DocumentID + "\x00" + fmt.Sprint(record.Chunk.ChunkIndex)
}

func matchesMemoryMetadata(metadata map[string]any, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}
	for key, expected := range filter {
		actual, exists := metadata[key]
		if !exists || !reflect.DeepEqual(actual, expected) {
			return false
		}
	}
	return true
}

func cosineSimilarity(left types.Embedding, right types.Embedding) float64 {
	var dot, leftNorm, rightNorm float64
	for index := range left {
		dot += left[index] * right[index]
		leftNorm += left[index] * left[index]
		rightNorm += right[index] * right[index]
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func sortMemorySearchResults(results []VectorSearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].DocumentID != results[j].DocumentID {
			return results[i].DocumentID < results[j].DocumentID
		}
		return results[i].Chunk.ChunkIndex < results[j].Chunk.ChunkIndex
	})
}

func cloneMetadata(metadata map[string]any) map[string]any {
	return utils.CloneAnyMap(metadata)
}
