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

package qdrant

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestRecordToPointPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	record := qdrantRecord("doc-1", 2, types.Embedding{1, 0}, "hello", map[string]any{
		"tenant_id": "tenant-1",
		"version":   3,
		"active":    true,
		"nested":    map[string]any{"ignored_for_filter": true},
	})
	point, err := recordToPoint(record)
	if err != nil {
		t.Fatalf("recordToPoint returned error: %v", err)
	}
	if point.GetId() == nil || point.GetVectors() == nil {
		t.Fatalf("point missing id or vector: %#v", point)
	}
	if got := payloadString(point.Payload, payloadDocumentID); got != "doc-1" {
		t.Fatalf("document id payload = %q", got)
	}
	if got := payloadString(point.Payload, metadataKeyPrefix+"tenant_id"); got != "tenant-1" {
		t.Fatalf("metadata scalar payload = %q", got)
	}
	if _, exists := point.Payload[metadataKeyPrefix+"nested"]; exists {
		t.Fatalf("non-scalar metadata should not be expanded for filtering: %#v", point.Payload)
	}

	result, err := pointPayloadToResult(point.Payload, 0.9)
	if err != nil {
		t.Fatalf("pointPayloadToResult returned error: %v", err)
	}
	if result.Score != 0.9 || result.DocumentID != "doc-1" || result.Chunk.ChunkIndex != 2 {
		t.Fatalf("result mismatch: %#v", result)
	}
	if got := result.Chunk.Content.(*message.TextBlock).Text; got != "hello" {
		t.Fatalf("content mismatch: %q", got)
	}
	if got := result.Chunk.Metadata["tenant_id"]; got != "tenant-1" {
		t.Fatalf("metadata mismatch: %#v", result.Chunk.Metadata)
	}
}

func TestBuildMetadataFilterRejectsUnsupportedValues(t *testing.T) {
	t.Parallel()

	filter, err := buildMetadataFilter(map[string]any{
		"tenant_id": "tenant-1",
		"active":    true,
		"version":   uint64(3),
	})
	if err != nil {
		t.Fatalf("buildMetadataFilter returned error: %v", err)
	}
	if filter == nil || len(filter.Must) != 3 {
		t.Fatalf("filter mismatch: %#v", filter)
	}

	_, err = buildMetadataFilter(map[string]any{"nested": map[string]any{"x": 1}})
	if !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nested filter, got %v", err)
	}
}

func TestStoreIntegration(t *testing.T) {
	addr := os.Getenv("AGENTSCOPE_QDRANT_ADDR")
	if addr == "" {
		addr = os.Getenv("QDRANT_ADDR")
	}
	if addr == "" {
		t.Skip("set AGENTSCOPE_QDRANT_ADDR=127.0.0.1:6334 to run Qdrant integration test")
	}
	host, port, err := splitHostPort(addr)
	if err != nil {
		t.Fatalf("invalid qdrant address: %v", err)
	}

	store, err := Connect(Config{Host: host, Port: port, SkipCompatibilityCheck: true})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	collection := "agentscope_go_qdrant_test"
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = store.DeleteCollection(cleanupCtx, collection)
	cleanupCancel()
	if err := store.CreateCollection(ctx, collection, 2); err != nil {
		t.Fatalf("CreateCollection returned error: %v", err)
	}
	defer func() { _ = store.DeleteCollection(context.Background(), collection) }()

	if err := store.Insert(ctx, collection, []rag.VectorRecord{
		qdrantRecord("doc-1", 0, types.Embedding{1, 0}, "alpha", map[string]any{"tenant_id": "tenant-1"}),
		qdrantRecord("doc-2", 0, types.Embedding{0, 1}, "beta", map[string]any{"tenant_id": "tenant-2"}),
	}); err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	results, err := store.Search(ctx, collection, types.Embedding{1, 0}, 1, map[string]any{"tenant_id": "tenant-1"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].DocumentID != "doc-1" {
		t.Fatalf("unexpected search results: %#v", results)
	}
}

func qdrantRecord(
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

func splitHostPort(addr string) (string, int, error) {
	host, portText, ok := strings.Cut(addr, ":")
	if !ok || host == "" || portText == "" {
		return "", 0, errors.New("want host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}
