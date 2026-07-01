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
	"unicode/utf8"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

const (
	defaultChunkSizeTokens    = 512
	defaultChunkOverlapTokens = 64
	approxBytesPerToken       = 4
)

// Chunker turns parsed sections into embedding-ready chunks.
type Chunker interface {
	Chunk(ctx context.Context, sections []Section) ([]Chunk, error)
}

// ChunkerOption configures ApproxTokenChunker.
type ChunkerOption func(*ApproxTokenChunker)

// ApproxTokenChunker chunks text by an approximate token budget.
type ApproxTokenChunker struct {
	chunkSize    int
	chunkOverlap int
}

// NewApproxTokenChunker creates an approximate token chunker.
func NewApproxTokenChunker(opts ...ChunkerOption) (*ApproxTokenChunker, error) {
	chunker := &ApproxTokenChunker{
		chunkSize:    defaultChunkSizeTokens,
		chunkOverlap: defaultChunkOverlapTokens,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(chunker)
		}
	}
	if chunker.chunkSize <= 0 {
		return nil, fmt.Errorf("%w: chunk size must be positive", ErrInvalidInput)
	}
	if chunker.chunkOverlap < 0 {
		return nil, fmt.Errorf("%w: chunk overlap must not be negative", ErrInvalidInput)
	}
	if chunker.chunkOverlap >= chunker.chunkSize {
		return nil, fmt.Errorf("%w: chunk overlap must be smaller than chunk size", ErrInvalidInput)
	}
	return chunker, nil
}

// WithChunkSize sets the chunk size in approximate tokens.
func WithChunkSize(tokens int) ChunkerOption {
	return func(c *ApproxTokenChunker) {
		c.chunkSize = tokens
	}
}

// WithChunkOverlap sets the overlap in approximate tokens.
func WithChunkOverlap(tokens int) ChunkerOption {
	return func(c *ApproxTokenChunker) {
		c.chunkOverlap = tokens
	}
}

// Chunk splits text sections and passes data sections through as single chunks.
func (c *ApproxTokenChunker) Chunk(ctx context.Context, sections []Section) ([]Chunk, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: chunker is nil", ErrInvalidInput)
	}

	chunks := make([]Chunk, 0, len(sections))
	for _, section := range sections {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if section.Content == nil {
			return nil, fmt.Errorf("%w: section content is nil", ErrInvalidInput)
		}

		switch block := section.Content.(type) {
		case *message.TextBlock:
			parts := splitText(block.Text, c.chunkSize*approxBytesPerToken, c.chunkOverlap*approxBytesPerToken)
			for _, part := range parts {
				chunks = append(chunks, Chunk{
					Content:  message.NewTextBlock(part),
					Source:   section.Source,
					Metadata: utils.CloneAnyMap(section.Metadata),
				})
			}
		case *message.DataBlock:
			chunks = append(chunks, Chunk{
				Content:  block.Clone(),
				Source:   section.Source,
				Metadata: utils.CloneAnyMap(section.Metadata),
			})
		default:
			return nil, fmt.Errorf("%w: %s blocks cannot be chunked", ErrUnsupportedContent, section.Content.BlockType())
		}
	}

	total := len(chunks)
	for index := range chunks {
		chunks[index].ChunkIndex = index
		chunks[index].TotalChunks = total
	}
	return chunks, nil
}

func splitText(text string, budgetBytes int, overlapBytes int) []string {
	if text == "" {
		return nil
	}
	if len(text) <= budgetBytes {
		return []string{text}
	}

	runes := []rune(text)
	parts := []string{}
	for start := 0; start < len(runes); {
		end := start
		used := 0
		for end < len(runes) {
			size := utf8.RuneLen(runes[end])
			if used > 0 && used+size > budgetBytes {
				break
			}
			used += size
			end++
		}
		if end == start {
			end++
		}
		parts = append(parts, string(runes[start:end]))
		if end >= len(runes) {
			break
		}
		if overlapBytes <= 0 {
			start = end
			continue
		}

		overlapStart := end
		used = 0
		for overlapStart > start {
			size := utf8.RuneLen(runes[overlapStart-1])
			if used+size > overlapBytes {
				break
			}
			used += size
			overlapStart--
		}
		if overlapStart <= start || overlapStart == end {
			start = end
			continue
		}
		start = overlapStart
	}
	return parts
}
