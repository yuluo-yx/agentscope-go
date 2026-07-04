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
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	qdrantapi "github.com/qdrant/go-client/qdrant"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestParseDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		distance Distance
		want     qdrantapi.Distance
		wantErr  bool
	}{
		{name: "default", distance: "", want: qdrantapi.Distance_Cosine},
		{name: "cosine", distance: DistanceCosine, want: qdrantapi.Distance_Cosine},
		{name: "dot", distance: DistanceDot, want: qdrantapi.Distance_Dot},
		{name: "euclid", distance: DistanceEuclid, want: qdrantapi.Distance_Euclid},
		{name: "manhattan", distance: DistanceManhattan, want: qdrantapi.Distance_Manhattan},
		{name: "case insensitive", distance: Distance("dot"), want: qdrantapi.Distance_Dot},
		{name: "invalid", distance: Distance("Chebyshev"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDistance(tt.distance)
			if tt.wantErr {
				if !errors.Is(err, rag.ErrInvalidInput) {
					t.Fatalf("parseDistance error = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDistance returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseDistance = %v, want %v", got, tt.want)
			}
		})
	}
}

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

func TestStoreValidationBranches(t *testing.T) {
	t.Parallel()

	if _, err := NewStore(nil); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("NewStore(nil) error = %v, want ErrInvalidInput", err)
	}
	store := &Store{}
	if err := store.Close(); err != nil {
		t.Fatalf("Close without client should be nil, got %v", err)
	}
	if err := store.CreateCollection(context.Background(), " ", 2); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("blank CreateCollection should fail, got %v", err)
	}
	if err := store.CreateCollection(context.Background(), "docs", 0); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid dimensions should fail, got %v", err)
	}
	if err := store.CreateCollection(context.Background(), "docs", 2); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("nil-client CreateCollection should fail, got %v", err)
	}
	if err := store.DeleteCollection(context.Background(), " "); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("blank DeleteCollection should fail, got %v", err)
	}
	if err := store.DeleteCollection(context.Background(), "docs"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("nil-client DeleteCollection should fail, got %v", err)
	}
	if _, err := store.HasCollection(context.Background(), "docs"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("nil-client HasCollection should fail, got %v", err)
	}
	if err := store.Insert(context.Background(), "docs", nil); err != nil {
		t.Fatalf("empty Insert should be a no-op, got %v", err)
	}
	if err := store.Insert(context.Background(), "docs", []rag.VectorRecord{
		qdrantRecord("doc-1", 0, types.Embedding{1, 0}, "hello", nil),
	}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("nil-client Insert should fail, got %v", err)
	}
	if err := store.Delete(context.Background(), "docs", " "); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("blank Delete should fail, got %v", err)
	}
	if err := store.Delete(context.Background(), "docs", "doc-1"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("nil-client Delete should fail, got %v", err)
	}
	if results, err := store.Search(context.Background(), "docs", types.Embedding{1, 0}, 0, nil); err != nil || results != nil {
		t.Fatalf("topK <= 0 should return nil results, got %#v err=%v", results, err)
	}
	if _, err := store.Search(context.Background(), "docs", nil, 1, nil); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("empty query vector should fail, got %v", err)
	}
	if _, err := store.Search(context.Background(), "docs", types.Embedding{1, 0}, 1, nil); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("nil-client Search should fail, got %v", err)
	}
	if _, err := store.ListDocuments(context.Background(), "docs", nil); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("nil-client ListDocuments should fail, got %v", err)
	}

	if err := WithDistance(DistanceDot)(store); err != nil {
		t.Fatalf("WithDistance returned error: %v", err)
	}
	if store.distance != qdrantapi.Distance_Dot {
		t.Fatalf("distance = %v, want Dot", store.distance)
	}
	if err := WithDistance(Distance("bad"))(store); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid WithDistance should fail, got %v", err)
	}
}

func TestStoreClientMethodsRespectCanceledContext(t *testing.T) {
	t.Parallel()

	if _, err := Connect(Config{SkipCompatibilityCheck: true, Distance: Distance("bad")}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("Connect with invalid distance should fail before network use, got %v", err)
	}
	client, err := qdrantapi.NewClient(&qdrantapi.Config{
		Host:                   "127.0.0.1",
		Port:                   6334,
		SkipCompatibilityCheck: true,
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	store, err := NewStore(client, nil, WithDistance(DistanceCosine))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.CreateCollection(ctx, "docs", 2); err == nil {
		t.Fatal("CreateCollection with canceled context should fail")
	}
	if err := store.DeleteCollection(ctx, "docs"); err == nil {
		t.Fatal("DeleteCollection with canceled context should fail")
	}
	if _, err := store.HasCollection(ctx, "docs"); err == nil {
		t.Fatal("HasCollection with canceled context should fail")
	}
	if err := store.Insert(ctx, "docs", []rag.VectorRecord{
		qdrantRecord("doc-1", 0, types.Embedding{1, 0}, "hello", map[string]any{"tenant_id": "tenant-1"}),
	}); err == nil {
		t.Fatal("Insert with canceled context should fail")
	}
	if err := store.Delete(ctx, "docs", "doc-1"); err == nil {
		t.Fatal("Delete with canceled context should fail")
	}
	if _, err := store.Search(ctx, "docs", types.Embedding{1, 0}, 1, map[string]any{"tenant_id": "tenant-1"}); err == nil {
		t.Fatal("Search with canceled context should fail")
	}
	if _, err := store.ListDocuments(ctx, "docs", map[string]any{"tenant_id": "tenant-1"}); err == nil {
		t.Fatal("ListDocuments with canceled context should fail")
	}
}

func TestRecordToPointRejectsInvalidRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record rag.VectorRecord
	}{
		{name: "blank document", record: qdrantRecord(" ", 0, types.Embedding{1, 0}, "hello", nil)},
		{name: "empty vector", record: qdrantRecord("doc-1", 0, nil, "hello", nil)},
		{name: "nil content", record: rag.VectorRecord{DocumentID: "doc-1", Vector: types.Embedding{1, 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := recordToPoint(tt.record)
			if !errors.Is(err, rag.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
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

	if _, err := buildMetadataFilter(map[string]any{" ": "tenant-1"}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for blank metadata key, got %v", err)
	}
	_, err = buildMetadataFilter(map[string]any{"nested": map[string]any{"x": 1}})
	if !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nested filter, got %v", err)
	}
}

func TestMetadataConditionSupportsScalarTypes(t *testing.T) {
	t.Parallel()

	values := []any{
		"text",
		true,
		int(1),
		int8(1),
		int16(1),
		int32(1),
		int64(1),
		uint(1),
		uint8(1),
		uint16(1),
		uint32(1),
		uint64(1),
		float32(1.25),
		float64(2.5),
	}
	for _, value := range values {
		condition, err := metadataCondition("metadata__value", value)
		if err != nil {
			t.Fatalf("metadataCondition(%T) returned error: %v", value, err)
		}
		if condition == nil {
			t.Fatalf("metadataCondition(%T) returned nil condition", value)
		}
	}
	if _, err := metadataCondition("metadata__value", uint64(math.MaxInt64)+1); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("overflowing uint64 should fail, got %v", err)
	}
	if _, err := metadataCondition("metadata__value", []string{"x"}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("unsupported metadata value should fail, got %v", err)
	}
}

func TestPayloadHelpersAndResultErrors(t *testing.T) {
	t.Parallel()

	if payloadString(nil, "missing") != "" || payloadInt(nil, "missing") != 0 {
		t.Fatal("nil payload helpers should return zero values")
	}
	payload, err := qdrantapi.TryValueMap(map[string]any{
		"integer": int64(3),
		"double":  4.0,
		"string":  "5",
		"bad":     "bad",
	})
	if err != nil {
		t.Fatalf("TryValueMap returned error: %v", err)
	}
	if payloadInt(payload, "integer") != 3 || payloadInt(payload, "double") != 4 ||
		payloadInt(payload, "string") != 5 || payloadInt(payload, "bad") != 0 {
		t.Fatalf("payloadInt mismatch: %#v", payload)
	}
	if _, err := pointPayloadToResult(nil, 0); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("missing document id should fail, got %v", err)
	}
	invalidContent, err := qdrantapi.TryValueMap(map[string]any{
		payloadDocumentID:  "doc-1",
		payloadContentJSON: `{"type":"unknown"}`,
	})
	if err != nil {
		t.Fatalf("TryValueMap invalid content returned error: %v", err)
	}
	if _, err := pointPayloadToResult(invalidContent, 0); err == nil {
		t.Fatal("invalid content JSON should fail")
	}
	invalidMetadata, err := qdrantapi.TryValueMap(map[string]any{
		payloadDocumentID:  "doc-1",
		payloadContentJSON: `{"type":"text","text":"hello"}`,
		payloadMetadata:    `{`,
	})
	if err != nil {
		t.Fatalf("TryValueMap invalid metadata returned error: %v", err)
	}
	if _, err := pointPayloadToResult(invalidMetadata, 0); err == nil {
		t.Fatal("invalid metadata JSON should fail")
	}
}

func TestCloneAndSortResults(t *testing.T) {
	t.Parallel()

	results := []rag.VectorSearchResult{
		{Score: 0.7, DocumentID: "doc-b", Chunk: rag.Chunk{ChunkIndex: 0, Content: message.NewTextBlock("b")}},
		{Score: 0.9, DocumentID: "doc-a", Chunk: rag.Chunk{ChunkIndex: 1, Content: message.NewTextBlock("a1")}},
		{Score: 0.9, DocumentID: "doc-a", Chunk: rag.Chunk{ChunkIndex: 0, Content: message.NewTextBlock("a0")}},
	}
	sorted := cloneAndSortResults(results)
	order := []string{
		sorted[0].DocumentID + ":0",
		sorted[1].DocumentID + ":1",
		sorted[2].DocumentID + ":0",
	}
	if strings.Join(order, ",") != "doc-a:0,doc-a:1,doc-b:0" {
		t.Fatalf("sorted order mismatch: %#v", order)
	}
	sorted[0].Chunk.Content.(*message.TextBlock).Text = "changed"
	if results[2].Chunk.Content.(*message.TextBlock).Text != "a0" {
		t.Fatalf("cloneAndSortResults should clone content, got %#v", results[2])
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
