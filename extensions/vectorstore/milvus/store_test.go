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

package milvus

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestNewSchemaDefinesRequiredFields(t *testing.T) {
	t.Parallel()

	schema := newSchema("docs", 3)
	if schema.CollectionName != "docs" || len(schema.Fields) != 8 {
		t.Fatalf("schema mismatch: %#v", schema)
	}
	if schema.PKFieldName() != fieldID {
		t.Fatalf("primary key = %q", schema.PKFieldName())
	}
	var vectorField *entity.Field
	for _, field := range schema.Fields {
		if field.Name == fieldVector {
			vectorField = field
			break
		}
	}
	if vectorField == nil || vectorField.DataType != entity.FieldTypeFloatVector {
		t.Fatalf("vector field mismatch: %#v", vectorField)
	}
	dim, err := vectorField.GetDim()
	if err != nil {
		t.Fatalf("GetDim returned error: %v", err)
	}
	if dim != 3 {
		t.Fatalf("vector dimension = %d, want 3", dim)
	}
}

func TestRecordsToColumnsAndResultSetRoundTrip(t *testing.T) {
	t.Parallel()

	records := []rag.VectorRecord{
		milvusRecord("doc-1", 0, types.Embedding{1, 0}, "hello", map[string]any{"tenant_id": "tenant-1"}),
		milvusRecord("doc-1", 1, types.Embedding{0, 1}, "world", map[string]any{"tenant_id": "tenant-1"}),
	}
	columns, err := recordsToColumns(records)
	if err != nil {
		t.Fatalf("recordsToColumns returned error: %v", err)
	}
	if len(columns) != 8 {
		t.Fatalf("expected 8 columns, got %#v", columns)
	}
	content, err := columns[6].Get(0)
	if err != nil {
		t.Fatalf("content column Get returned error: %v", err)
	}
	metadata, err := columns[7].Get(0)
	if err != nil {
		t.Fatalf("metadata column Get returned error: %v", err)
	}

	resultSet := milvusclient.ResultSet{
		ResultCount: 1,
		Fields: milvusclient.DataSet{
			column.NewColumnVarChar(fieldDocumentID, []string{"doc-1"}),
			column.NewColumnVarChar(fieldSource, []string{"doc-1.md"}),
			column.NewColumnInt64(fieldChunkIndex, []int64{0}),
			column.NewColumnInt64(fieldTotalChunks, []int64{2}),
			column.NewColumnVarChar(fieldContentJSON, []string{content.(string)}),
			column.NewColumnJSONBytes(fieldMetadata, [][]byte{metadata.([]byte)}),
		},
		Scores: []float32{0.75},
	}
	results, err := resultSetToResults(resultSet)
	if err != nil {
		t.Fatalf("resultSetToResults returned error: %v", err)
	}
	if len(results) != 1 || results[0].Score != 0.75 || results[0].DocumentID != "doc-1" {
		t.Fatalf("result mismatch: %#v", results)
	}
	if got := results[0].Chunk.Content.(*message.TextBlock).Text; got != "hello" {
		t.Fatalf("content mismatch: %q", got)
	}
	if got := results[0].Chunk.Metadata["tenant_id"]; got != "tenant-1" {
		t.Fatalf("metadata mismatch: %#v", results[0].Chunk.Metadata)
	}
}

func TestMetadataExpr(t *testing.T) {
	t.Parallel()

	expr, err := metadataExpr(map[string]any{
		"active":    true,
		"tenant_id": "tenant-1",
		"version":   3,
	})
	if err != nil {
		t.Fatalf("metadataExpr returned error: %v", err)
	}
	want := `metadata["active"] == true && metadata["tenant_id"] == "tenant-1" && metadata["version"] == 3`
	if expr != want {
		t.Fatalf("expr = %q, want %q", expr, want)
	}

	_, err = metadataExpr(map[string]any{"nested": map[string]any{"x": 1}})
	if !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nested filter, got %v", err)
	}
}

func TestStoreIntegration(t *testing.T) {
	addr := os.Getenv("AGENTSCOPE_MILVUS_ADDR")
	if addr == "" {
		addr = os.Getenv("MILVUS_ADDR")
	}
	if addr == "" {
		t.Skip("set AGENTSCOPE_MILVUS_ADDR=localhost:19530 to run Milvus integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	store, err := Connect(ctx, Config{Address: addr})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer func() {
		if err := store.Close(context.Background()); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	}()

	collectionName := "agentscope_go_milvus_test"
	_ = store.DeleteCollection(ctx, collectionName)
	if err := store.CreateCollection(ctx, collectionName, 2); err != nil {
		t.Fatalf("CreateCollection returned error: %v", err)
	}
	defer func() { _ = store.DeleteCollection(context.Background(), collectionName) }()

	if err := store.Insert(ctx, collectionName, []rag.VectorRecord{
		milvusRecord("doc-1", 0, types.Embedding{1, 0}, "alpha", map[string]any{"tenant_id": "tenant-1"}),
		milvusRecord("doc-2", 0, types.Embedding{0, 1}, "beta", map[string]any{"tenant_id": "tenant-2"}),
	}); err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	results, err := store.Search(ctx, collectionName, types.Embedding{1, 0}, 1, map[string]any{"tenant_id": "tenant-1"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].DocumentID != "doc-1" {
		t.Fatalf("unexpected search results: %#v", results)
	}
}

func milvusRecord(
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
			TotalChunks: 2,
			Metadata:    metadata,
		},
	}
}
