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
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// Section is parsed source content before chunking.
type Section struct {
	Content  message.ContentBlock `json:"content"`
	Source   string               `json:"source,omitempty"`
	Metadata map[string]any       `json:"metadata,omitempty"`
}

// Clone returns a deep copy of the section.
func (s Section) Clone() Section {
	return Section{
		Content:  cloneBlock(s.Content),
		Source:   s.Source,
		Metadata: utils.CloneAnyMap(s.Metadata),
	}
}

// Chunk is the unit embedded into and retrieved from a knowledge base.
type Chunk struct {
	Content     message.ContentBlock `json:"content"`
	Source      string               `json:"source,omitempty"`
	ChunkIndex  int                  `json:"chunk_index"`
	TotalChunks int                  `json:"total_chunks"`
	Metadata    map[string]any       `json:"metadata,omitempty"`
}

// Clone returns a deep copy of the chunk.
func (c Chunk) Clone() Chunk {
	return Chunk{
		Content:     cloneBlock(c.Content),
		Source:      c.Source,
		ChunkIndex:  c.ChunkIndex,
		TotalChunks: c.TotalChunks,
		Metadata:    utils.CloneAnyMap(c.Metadata),
	}
}

// VectorRecord is one vector-store row belonging to a source document chunk.
type VectorRecord struct {
	Vector     types.Embedding `json:"vector"`
	DocumentID string          `json:"document_id"`
	Chunk      Chunk           `json:"chunk"`
}

// Clone returns a deep copy of the vector record.
func (r VectorRecord) Clone() VectorRecord {
	return VectorRecord{
		Vector:     cloneEmbedding(r.Vector),
		DocumentID: r.DocumentID,
		Chunk:      r.Chunk.Clone(),
	}
}

// VectorSearchResult is one ranked retrieval result returned by a vector store.
type VectorSearchResult struct {
	Score      float64 `json:"score"`
	DocumentID string  `json:"document_id"`
	Chunk      Chunk   `json:"chunk"`
}

// Clone returns a deep copy of the search result.
func (r VectorSearchResult) Clone() VectorSearchResult {
	return VectorSearchResult{
		Score:      r.Score,
		DocumentID: r.DocumentID,
		Chunk:      r.Chunk.Clone(),
	}
}

// DocumentSummary describes one indexed source document.
type DocumentSummary struct {
	DocumentID string         `json:"document_id"`
	Source     string         `json:"source,omitempty"`
	ChunkCount int            `json:"chunk_count"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Clone returns a deep copy of the document summary.
func (s DocumentSummary) Clone() DocumentSummary {
	return DocumentSummary{
		DocumentID: s.DocumentID,
		Source:     s.Source,
		ChunkCount: s.ChunkCount,
		Metadata:   utils.CloneAnyMap(s.Metadata),
	}
}

func cloneBlock(block message.ContentBlock) message.ContentBlock {
	if block == nil {
		return nil
	}
	return block.Clone()
}

func cloneEmbedding(in types.Embedding) types.Embedding {
	if in == nil {
		return nil
	}
	return append(types.Embedding(nil), in...)
}

func cloneResults(in []VectorSearchResult) []VectorSearchResult {
	if in == nil {
		return nil
	}
	out := make([]VectorSearchResult, 0, len(in))
	for _, result := range in {
		out = append(out, result.Clone())
	}
	return out
}

func cloneDocumentSummaries(in []DocumentSummary) []DocumentSummary {
	if in == nil {
		return nil
	}
	out := make([]DocumentSummary, 0, len(in))
	for _, summary := range in {
		out = append(out, summary.Clone())
	}
	return out
}
