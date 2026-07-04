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
	"math"
	"os"
	"reflect"
	"strings"
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

func TestStoreValidationBranches(t *testing.T) {
	t.Parallel()

	if _, err := NewStore(nil); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("NewStore(nil) error = %v, want ErrInvalidInput", err)
	}
	store := &Store{}
	if err := store.Close(context.Background()); err != nil {
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
		milvusRecord("doc-1", 0, types.Embedding{1, 0}, "hello", nil),
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
}

func TestStoreUsesMilvusClient(t *testing.T) {
	t.Parallel()

	client := &fakeMilvusClient{
		searchResults: []milvusclient.ResultSet{
			milvusResultSet([]string{"doc-b", "doc-a", "doc-a"}, []int64{0, 1, 0}, []float32{0.7, 0.9, 0.9}),
		},
		queryResults: []milvusclient.ResultSet{
			milvusResultSet([]string{"doc-b", "doc-a", "doc-a"}, []int64{0, 1, 0}, []float32{0.7, 0.9, 0.9}),
		},
	}
	store := &Store{client: client}
	ctx := context.Background()

	if err := store.CreateCollection(ctx, "docs", 2); err != nil {
		t.Fatalf("CreateCollection returned error: %v", err)
	}
	if client.hasCalls != 1 || client.createCalls != 1 || client.loadCalls != 1 || client.awaitCalls != 1 {
		t.Fatalf("create/load calls mismatch: %#v", client)
	}

	client.collectionExists = true
	if err := store.CreateCollection(ctx, "docs", 2); err != nil {
		t.Fatalf("CreateCollection for existing collection returned error: %v", err)
	}
	if client.createCalls != 1 || client.loadCalls != 2 {
		t.Fatalf("existing collection should only load, got %#v", client)
	}

	if err := store.Insert(ctx, "docs", []rag.VectorRecord{
		milvusRecord("doc-a", 0, types.Embedding{1, 0}, "alpha", map[string]any{"tenant_id": "tenant-1"}),
	}); err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}
	if err := store.Delete(ctx, "docs", "doc-a"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if err := store.DeleteCollection(ctx, "docs"); err != nil {
		t.Fatalf("DeleteCollection returned error: %v", err)
	}
	if exists, err := store.HasCollection(ctx, "docs"); err != nil || !exists {
		t.Fatalf("HasCollection = %v, %v; want true, nil", exists, err)
	}
	if err := store.Close(ctx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	results, err := store.Search(ctx, "docs", types.Embedding{1, 0}, 2, map[string]any{"tenant_id": "tenant-1"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got := []string{results[0].DocumentID, results[1].DocumentID}; !reflect.DeepEqual(got, []string{"doc-a", "doc-a"}) {
		t.Fatalf("Search sort/topK mismatch: %#v", results)
	}

	summaries, err := store.ListDocuments(ctx, "docs", nil)
	if err != nil {
		t.Fatalf("ListDocuments returned error: %v", err)
	}
	if got := []string{summaries[0].DocumentID, summaries[1].DocumentID}; !reflect.DeepEqual(got, []string{"doc-a", "doc-b"}) {
		t.Fatalf("document summary order mismatch: %#v", summaries)
	}
	if summaries[0].ChunkCount != 2 || summaries[1].ChunkCount != 1 {
		t.Fatalf("document summary chunk counts mismatch: %#v", summaries)
	}
	if client.insertCalls != 1 || client.deleteCalls != 1 || client.dropCalls != 1 ||
		client.closeCalls != 1 || client.searchCalls != 1 || client.queryCalls != 1 {
		t.Fatalf("client calls mismatch: %#v", client)
	}
}

func TestStoreMilvusClientErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	expected := errors.New("milvus failed")

	store := &Store{client: &fakeMilvusClient{hasErr: expected}}
	if err := store.CreateCollection(ctx, "docs", 2); !errors.Is(err, expected) {
		t.Fatalf("HasCollection error should propagate, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{createErr: expected}}
	if err := store.CreateCollection(ctx, "docs", 2); !errors.Is(err, expected) {
		t.Fatalf("CreateCollection error should propagate, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{loadErr: expected, collectionExists: true}}
	if err := store.CreateCollection(ctx, "docs", 2); !errors.Is(err, expected) {
		t.Fatalf("LoadCollection error should propagate, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{awaitErr: expected, collectionExists: true}}
	if err := store.CreateCollection(ctx, "docs", 2); !errors.Is(err, expected) {
		t.Fatalf("load Await error should propagate, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{searchErr: expected}}
	if _, err := store.Search(ctx, "docs", types.Embedding{1, 0}, 1, nil); !errors.Is(err, expected) {
		t.Fatalf("Search error should propagate, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{searchResults: []milvusclient.ResultSet{{Err: expected}}}}
	if _, err := store.Search(ctx, "docs", types.Embedding{1, 0}, 1, nil); !errors.Is(err, expected) {
		t.Fatalf("ResultSet error should propagate, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{queryErr: expected}}
	if _, err := store.ListDocuments(ctx, "docs", nil); !errors.Is(err, expected) {
		t.Fatalf("Query error should propagate, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{}}
	if err := store.Insert(ctx, "docs", []rag.VectorRecord{{DocumentID: " ", Vector: types.Embedding{1}}}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid record should fail before insert, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{searchResults: []milvusclient.ResultSet{{
		ResultCount: 1,
		Fields:      milvusclient.DataSet{},
	}}}}
	if _, err := store.Search(ctx, "docs", types.Embedding{1, 0}, 1, nil); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid search result set should fail, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{queryResults: []milvusclient.ResultSet{{
		ResultCount: 1,
		Fields:      milvusclient.DataSet{},
	}}}}
	if _, err := store.ListDocuments(ctx, "docs", nil); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid query result set should fail, got %v", err)
	}

	store = &Store{client: &fakeMilvusClient{}}
	if _, err := store.ListDocuments(ctx, "docs", map[string]any{" ": "bad"}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid metadata filter should fail before query, got %v", err)
	}
}

func TestRecordsToColumnsRejectsInvalidRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		records []rag.VectorRecord
	}{
		{name: "blank document", records: []rag.VectorRecord{milvusRecord(" ", 0, types.Embedding{1, 0}, "hello", nil)}},
		{name: "empty vector", records: []rag.VectorRecord{milvusRecord("doc-1", 0, nil, "hello", nil)}},
		{name: "mixed dimensions", records: []rag.VectorRecord{
			milvusRecord("doc-1", 0, types.Embedding{1, 0}, "hello", nil),
			milvusRecord("doc-1", 1, types.Embedding{1, 0, 0}, "hello", nil),
		}},
		{name: "nil content", records: []rag.VectorRecord{{DocumentID: "doc-1", Vector: types.Embedding{1, 0}}}},
		{name: "content too large", records: []rag.VectorRecord{
			milvusRecord("doc-1", 0, types.Embedding{1, 0}, strings.Repeat("x", maxContentLength), nil),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := recordsToColumns(tt.records)
			if !errors.Is(err, rag.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
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

	if _, err := metadataExpr(map[string]any{" ": "tenant-1"}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for blank metadata key, got %v", err)
	}
	_, err = metadataExpr(map[string]any{"nested": map[string]any{"x": 1}})
	if !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nested filter, got %v", err)
	}
}

func TestMilvusLiteralSupportsScalarTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value any
		want  string
	}{
		{value: `a"b`, want: `"a\"b"`},
		{value: true, want: "true"},
		{value: int(1), want: "1"},
		{value: int8(2), want: "2"},
		{value: int16(3), want: "3"},
		{value: int32(4), want: "4"},
		{value: int64(5), want: "5"},
		{value: uint(6), want: "6"},
		{value: uint8(7), want: "7"},
		{value: uint16(8), want: "8"},
		{value: uint32(9), want: "9"},
		{value: uint64(10), want: "10"},
		{value: float32(1.25), want: "1.25"},
		{value: float64(2.5), want: "2.5"},
	}
	for _, tt := range tests {
		got, err := milvusLiteral(tt.value)
		if err != nil {
			t.Fatalf("milvusLiteral(%T) returned error: %v", tt.value, err)
		}
		if got != tt.want {
			t.Fatalf("milvusLiteral(%#v) = %q, want %q", tt.value, got, tt.want)
		}
	}
	if _, err := milvusLiteral(uint64(math.MaxInt64) + 1); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("overflowing uint64 should fail, got %v", err)
	}
	if _, err := milvusLiteral([]string{"x"}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("unsupported literal should fail, got %v", err)
	}
}

func TestResultSetErrorBranchesAndSorting(t *testing.T) {
	t.Parallel()

	if _, err := resultSetToResults(milvusclient.ResultSet{
		ResultCount: 1,
		Fields:      milvusclient.DataSet{},
	}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("missing field should fail, got %v", err)
	}
	stringMetadata := milvusclient.ResultSet{
		ResultCount: 1,
		Fields: milvusclient.DataSet{
			column.NewColumnVarChar(fieldDocumentID, []string{"doc-1"}),
			column.NewColumnVarChar(fieldSource, []string{"doc-1.md"}),
			column.NewColumnInt64(fieldChunkIndex, []int64{0}),
			column.NewColumnInt64(fieldTotalChunks, []int64{1}),
			column.NewColumnVarChar(fieldContentJSON, []string{`{"type":"text","text":"hello"}`}),
			column.NewColumnVarChar(fieldMetadata, []string{`{"tenant_id":"tenant-1"}`}),
		},
	}
	results, err := resultSetToResults(stringMetadata)
	if err != nil {
		t.Fatalf("string metadata resultSetToResults returned error: %v", err)
	}
	if results[0].Chunk.Metadata["tenant_id"] != "tenant-1" {
		t.Fatalf("metadata mismatch: %#v", results[0].Chunk.Metadata)
	}
	invalidMetadata := stringMetadata
	invalidMetadata.Fields[5] = column.NewColumnInt64(fieldMetadata, []int64{1})
	if _, err := resultSetToResults(invalidMetadata); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("unexpected metadata column type should fail, got %v", err)
	}

	toSort := []rag.VectorSearchResult{
		{Score: 0.7, DocumentID: "doc-b", Chunk: rag.Chunk{ChunkIndex: 0, Content: message.NewTextBlock("b")}},
		{Score: 0.9, DocumentID: "doc-a", Chunk: rag.Chunk{ChunkIndex: 1, Content: message.NewTextBlock("a1")}},
		{Score: 0.9, DocumentID: "doc-a", Chunk: rag.Chunk{ChunkIndex: 0, Content: message.NewTextBlock("a0")}},
	}
	sortResults(toSort)
	order := []string{
		toSort[0].DocumentID + ":0",
		toSort[1].DocumentID + ":1",
		toSort[2].DocumentID + ":0",
	}
	if !reflect.DeepEqual(order, []string{"doc-a:0", "doc-a:1", "doc-b:0"}) {
		t.Fatalf("sortResults order mismatch: %#v", order)
	}
	cloned := cloneResults(toSort)
	cloned[0].Chunk.Content.(*message.TextBlock).Text = "changed"
	if toSort[0].Chunk.Content.(*message.TextBlock).Text != "a0" {
		t.Fatalf("cloneResults should clone content, got %#v", toSort[0])
	}
}

func TestResultSetRowErrorBranches(t *testing.T) {
	t.Parallel()

	valid := milvusResultSet([]string{"doc-1"}, []int64{0}, []float32{0.9})
	for _, tt := range []struct {
		name  string
		field string
	}{
		{name: "source", field: fieldSource},
		{name: "chunk index", field: fieldChunkIndex},
		{name: "total chunks", field: fieldTotalChunks},
		{name: "content", field: fieldContentJSON},
		{name: "metadata", field: fieldMetadata},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resultSet := valid
			resultSet.Fields = withoutMilvusField(valid.Fields, tt.field)
			if _, err := resultSetToResults(resultSet); !errors.Is(err, rag.ErrInvalidInput) {
				t.Fatalf("missing %s should fail, got %v", tt.field, err)
			}
		})
	}

	invalidContent := valid
	invalidContent.Fields = replaceMilvusField(
		valid.Fields,
		fieldContentJSON,
		column.NewColumnVarChar(fieldContentJSON, []string{`{"type":"unknown"}`}),
	)
	if _, err := resultSetToResults(invalidContent); err == nil {
		t.Fatal("invalid content JSON should fail")
	}

	if _, err := columnString(valid, fieldDocumentID, 5); err == nil {
		t.Fatal("out-of-range string column lookup should fail")
	}
	if _, err := columnInt(valid, fieldChunkIndex, 5); err == nil {
		t.Fatal("out-of-range int column lookup should fail")
	}
	if _, err := columnJSONMap(valid, fieldMetadata, 5); err == nil {
		t.Fatal("out-of-range JSON column lookup should fail")
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

type fakeMilvusClient struct {
	collectionExists bool
	hasErr           error
	createErr        error
	dropErr          error
	insertErr        error
	deleteErr        error
	searchErr        error
	queryErr         error
	loadErr          error
	awaitErr         error
	closeErr         error
	searchResults    []milvusclient.ResultSet
	queryResults     []milvusclient.ResultSet
	hasCalls         int
	createCalls      int
	dropCalls        int
	insertCalls      int
	deleteCalls      int
	searchCalls      int
	queryCalls       int
	loadCalls        int
	awaitCalls       int
	closeCalls       int
}

func (f *fakeMilvusClient) Close(context.Context) error {
	f.closeCalls++
	return f.closeErr
}

func (f *fakeMilvusClient) HasCollection(context.Context, milvusclient.HasCollectionOption) (bool, error) {
	f.hasCalls++
	return f.collectionExists, f.hasErr
}

func (f *fakeMilvusClient) CreateCollection(context.Context, milvusclient.CreateCollectionOption) error {
	f.createCalls++
	if f.createErr == nil {
		f.collectionExists = true
	}
	return f.createErr
}

func (f *fakeMilvusClient) DropCollection(context.Context, milvusclient.DropCollectionOption) error {
	f.dropCalls++
	return f.dropErr
}

func (f *fakeMilvusClient) Insert(context.Context, milvusclient.InsertOption) (milvusclient.InsertResult, error) {
	f.insertCalls++
	return milvusclient.InsertResult{}, f.insertErr
}

func (f *fakeMilvusClient) Delete(context.Context, milvusclient.DeleteOption) (milvusclient.DeleteResult, error) {
	f.deleteCalls++
	return milvusclient.DeleteResult{}, f.deleteErr
}

func (f *fakeMilvusClient) Search(context.Context, milvusclient.SearchOption) ([]milvusclient.ResultSet, error) {
	f.searchCalls++
	return f.searchResults, f.searchErr
}

func (f *fakeMilvusClient) Query(context.Context, milvusclient.QueryOption) (milvusclient.ResultSet, error) {
	f.queryCalls++
	if len(f.queryResults) == 0 {
		return milvusclient.ResultSet{}, f.queryErr
	}
	result := f.queryResults[0]
	f.queryResults = f.queryResults[1:]
	return result, f.queryErr
}

func (f *fakeMilvusClient) LoadCollection(context.Context, milvusclient.LoadCollectionOption) (milvusLoadTask, error) {
	f.loadCalls++
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return fakeMilvusLoadTask{client: f}, nil
}

type fakeMilvusLoadTask struct {
	client *fakeMilvusClient
}

func (t fakeMilvusLoadTask) Await(context.Context) error {
	t.client.awaitCalls++
	return t.client.awaitErr
}

func milvusResultSet(documentIDs []string, chunkIndexes []int64, scores []float32) milvusclient.ResultSet {
	sources := make([]string, 0, len(documentIDs))
	totalChunks := make([]int64, 0, len(documentIDs))
	contents := make([]string, 0, len(documentIDs))
	metadata := make([][]byte, 0, len(documentIDs))
	for index, documentID := range documentIDs {
		sources = append(sources, documentID+".md")
		totalChunks = append(totalChunks, int64(len(documentIDs)))
		contents = append(contents, `{"type":"text","text":"`+documentID+`"}`)
		metadata = append(metadata, []byte(`{"tenant_id":"tenant-1"}`))
		if index >= len(chunkIndexes) {
			chunkIndexes = append(chunkIndexes, int64(index))
		}
	}
	return milvusclient.ResultSet{
		ResultCount: len(documentIDs),
		Fields: milvusclient.DataSet{
			column.NewColumnVarChar(fieldDocumentID, documentIDs),
			column.NewColumnVarChar(fieldSource, sources),
			column.NewColumnInt64(fieldChunkIndex, chunkIndexes),
			column.NewColumnInt64(fieldTotalChunks, totalChunks),
			column.NewColumnVarChar(fieldContentJSON, contents),
			column.NewColumnJSONBytes(fieldMetadata, metadata),
		},
		Scores: scores,
	}
}

func withoutMilvusField(fields milvusclient.DataSet, field string) milvusclient.DataSet {
	out := make(milvusclient.DataSet, 0, len(fields))
	for _, column := range fields {
		if column.Name() != field {
			out = append(out, column)
		}
	}
	return out
}

func replaceMilvusField(fields milvusclient.DataSet, field string, replacement column.Column) milvusclient.DataSet {
	out := make(milvusclient.DataSet, 0, len(fields))
	for _, column := range fields {
		if column.Name() == field {
			out = append(out, replacement)
			continue
		}
		out = append(out, column)
	}
	return out
}
