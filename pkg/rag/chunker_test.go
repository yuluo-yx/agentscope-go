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
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

func TestApproxTokenChunkerPreservesSectionBoundariesAndIndexes(t *testing.T) {
	chunker, err := rag.NewApproxTokenChunker(
		rag.WithChunkSize(1),
		rag.WithChunkOverlap(0),
	)
	if err != nil {
		t.Fatalf("NewApproxTokenChunker returned error: %v", err)
	}
	data := message.NewDataBlock(message.NewBase64Source("AA==", "image/png"))
	sections := []rag.Section{
		{
			Content:  message.NewTextBlock("abcdefgh"),
			Source:   "guide.txt",
			Metadata: map[string]any{"page": 1},
		},
		{
			Content:  data,
			Source:   "guide.txt",
			Metadata: map[string]any{"page": 2},
		},
	}

	chunks, err := chunker.Chunk(context.Background(), sections)
	if err != nil {
		t.Fatalf("Chunk returned error: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %#v", len(chunks), chunks)
	}
	expectedText := []string{"abcd", "efgh"}
	for i, expected := range expectedText {
		text, ok := chunks[i].Content.(*message.TextBlock)
		if !ok {
			t.Fatalf("chunk %d should be text, got %T", i, chunks[i].Content)
		}
		if text.Text != expected {
			t.Fatalf("chunk %d text = %q, want %q", i, text.Text, expected)
		}
		if chunks[i].ChunkIndex != i || chunks[i].TotalChunks != len(chunks) {
			t.Fatalf("chunk %d indexes mismatch: %#v", i, chunks[i])
		}
		if chunks[i].Source != "guide.txt" || chunks[i].Metadata["page"] != 1 {
			t.Fatalf("chunk %d source/metadata mismatch: %#v", i, chunks[i])
		}
	}
	if _, ok := chunks[2].Content.(*message.DataBlock); !ok {
		t.Fatalf("data section should pass through as one chunk, got %T", chunks[2].Content)
	}
	if chunks[2].ChunkIndex != 2 || chunks[2].TotalChunks != 3 || chunks[2].Metadata["page"] != 2 {
		t.Fatalf("data chunk metadata/index mismatch: %#v", chunks[2])
	}
}

func TestNewApproxTokenChunkerRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		opts []rag.ChunkerOption
	}{
		{name: "zero chunk size", opts: []rag.ChunkerOption{rag.WithChunkSize(0)}},
		{name: "negative overlap", opts: []rag.ChunkerOption{rag.WithChunkOverlap(-1)}},
		{name: "overlap equals chunk size", opts: []rag.ChunkerOption{rag.WithChunkSize(4), rag.WithChunkOverlap(4)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := rag.NewApproxTokenChunker(tt.opts...)
			if !errors.Is(err, rag.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}
