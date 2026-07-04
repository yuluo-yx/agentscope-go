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
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

const (
	fieldID          = "id"
	fieldVector      = "vector"
	fieldDocumentID  = "document_id"
	fieldSource      = "source"
	fieldChunkIndex  = "chunk_index"
	fieldTotalChunks = "total_chunks"
	fieldContentJSON = "content_json"
	fieldMetadata    = "metadata"

	maxDocumentIDLength = 1024
	maxSourceLength     = 8192
	maxContentLength    = 65535
	defaultQueryLimit   = 1024
)

var outputFields = []string{
	fieldDocumentID,
	fieldSource,
	fieldChunkIndex,
	fieldTotalChunks,
	fieldContentJSON,
	fieldMetadata,
}

// Config configures a Milvus client created by Connect.
type Config struct {
	Address  string
	Username string
	Password string
	DBName   string
	APIKey   string
}

// Store implements rag.VectorStore on top of Milvus.
type Store struct {
	client milvusAPI
}

var _ rag.VectorStore = (*Store)(nil)

type milvusAPI interface {
	Close(context.Context) error
	HasCollection(context.Context, milvusclient.HasCollectionOption) (bool, error)
	CreateCollection(context.Context, milvusclient.CreateCollectionOption) error
	DropCollection(context.Context, milvusclient.DropCollectionOption) error
	Insert(context.Context, milvusclient.InsertOption) (milvusclient.InsertResult, error)
	Delete(context.Context, milvusclient.DeleteOption) (milvusclient.DeleteResult, error)
	Search(context.Context, milvusclient.SearchOption) ([]milvusclient.ResultSet, error)
	Query(context.Context, milvusclient.QueryOption) (milvusclient.ResultSet, error)
	LoadCollection(context.Context, milvusclient.LoadCollectionOption) (milvusLoadTask, error)
}

type milvusLoadTask interface {
	Await(context.Context) error
}

type sdkMilvusClient struct {
	client *milvusclient.Client
}

func (c sdkMilvusClient) Close(ctx context.Context) error {
	return c.client.Close(ctx)
}

func (c sdkMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption) (bool, error) {
	return c.client.HasCollection(ctx, option)
}

func (c sdkMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption) error {
	return c.client.CreateCollection(ctx, option)
}

func (c sdkMilvusClient) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption) error {
	return c.client.DropCollection(ctx, option)
}

func (c sdkMilvusClient) Insert(ctx context.Context, option milvusclient.InsertOption) (milvusclient.InsertResult, error) {
	return c.client.Insert(ctx, option)
}

func (c sdkMilvusClient) Delete(ctx context.Context, option milvusclient.DeleteOption) (milvusclient.DeleteResult, error) {
	return c.client.Delete(ctx, option)
}

func (c sdkMilvusClient) Search(ctx context.Context, option milvusclient.SearchOption) ([]milvusclient.ResultSet, error) {
	return c.client.Search(ctx, option)
}

func (c sdkMilvusClient) Query(ctx context.Context, option milvusclient.QueryOption) (milvusclient.ResultSet, error) {
	return c.client.Query(ctx, option)
}

func (c sdkMilvusClient) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption) (milvusLoadTask, error) {
	task, err := c.client.LoadCollection(ctx, option)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// Connect creates a Milvus client and wraps it as a vector store.
func Connect(ctx context.Context, config Config) (*Store, error) {
	client, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address:  config.Address,
		Username: config.Username,
		Password: config.Password,
		DBName:   config.DBName,
		APIKey:   config.APIKey,
	})
	if err != nil {
		return nil, err
	}
	return NewStore(client)
}

// NewStore wraps an existing Milvus client.
func NewStore(client *milvusclient.Client) (*Store, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: milvus client is nil", rag.ErrInvalidInput)
	}
	return &Store{client: sdkMilvusClient{client: client}}, nil
}

// Close closes the underlying Milvus client.
func (s *Store) Close(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close(ctx)
}

// CreateCollection creates a Milvus collection and loads it for search.
func (s *Store) CreateCollection(ctx context.Context, name string, dimensions int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: collection name is required", rag.ErrInvalidInput)
	}
	if dimensions <= 0 {
		return fmt.Errorf("%w: collection dimensions must be positive", rag.ErrInvalidInput)
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("%w: milvus store is nil", rag.ErrInvalidInput)
	}

	exists, err := s.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(name))
	if err != nil {
		return err
	}
	if !exists {
		schema := newSchema(name, dimensions)
		indexOption := milvusclient.NewCreateIndexOption(name, fieldVector, index.NewAutoIndex(entity.COSINE))
		if err := s.client.CreateCollection(
			ctx,
			milvusclient.NewCreateCollectionOption(name, schema).WithIndexOptions(indexOption),
		); err != nil {
			return err
		}
	}
	return s.loadCollection(ctx, name)
}

// DeleteCollection drops a Milvus collection.
func (s *Store) DeleteCollection(ctx context.Context, name string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("%w: milvus store is nil", rag.ErrInvalidInput)
	}
	return s.client.DropCollection(ctx, milvusclient.NewDropCollectionOption(strings.TrimSpace(name)))
}

// HasCollection reports whether a Milvus collection exists.
func (s *Store) HasCollection(ctx context.Context, name string) (bool, error) {
	if s == nil || s.client == nil {
		return false, fmt.Errorf("%w: milvus store is nil", rag.ErrInvalidInput)
	}
	return s.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(strings.TrimSpace(name)))
}

// Insert inserts vector records into Milvus.
func (s *Store) Insert(ctx context.Context, collectionName string, records []rag.VectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("%w: milvus store is nil", rag.ErrInvalidInput)
	}

	columns, err := recordsToColumns(records)
	if err != nil {
		return err
	}
	option := milvusclient.NewColumnBasedInsertOption(strings.TrimSpace(collectionName)).WithColumns(columns...)
	_, err = s.client.Insert(ctx, option)
	return err
}

// Delete removes all rows for a document id.
func (s *Store) Delete(ctx context.Context, collectionName string, documentID string) error {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return fmt.Errorf("%w: document id is required", rag.ErrInvalidInput)
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("%w: milvus store is nil", rag.ErrInvalidInput)
	}

	_, err := s.client.Delete(
		ctx,
		milvusclient.NewDeleteOption(strings.TrimSpace(collectionName)).
			WithExpr(fieldDocumentID+" == "+milvusStringLiteral(documentID)),
	)
	return err
}

// Search returns topK records ranked by Milvus score.
func (s *Store) Search(
	ctx context.Context,
	collectionName string,
	queryVector types.Embedding,
	topK int,
	metadataFilter map[string]any,
) ([]rag.VectorSearchResult, error) {
	if topK <= 0 {
		return nil, nil
	}
	if len(queryVector) == 0 {
		return nil, fmt.Errorf("%w: query vector is required", rag.ErrInvalidInput)
	}
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("%w: milvus store is nil", rag.ErrInvalidInput)
	}

	filter, err := metadataExpr(metadataFilter)
	if err != nil {
		return nil, err
	}
	option := milvusclient.NewSearchOption(
		strings.TrimSpace(collectionName),
		topK,
		[]entity.Vector{entity.FloatVector(embeddingToFloat32(queryVector))},
	).WithANNSField(fieldVector).
		WithOutputFields(outputFields...).
		WithConsistencyLevel(entity.ClStrong)
	if filter != "" {
		option = option.WithFilter(filter)
	}

	resultSets, err := s.client.Search(ctx, option)
	if err != nil {
		return nil, err
	}
	results := make([]rag.VectorSearchResult, 0, topK)
	for _, resultSet := range resultSets {
		if resultSet.Err != nil {
			return nil, resultSet.Err
		}
		rows, err := resultSetToResults(resultSet)
		if err != nil {
			return nil, err
		}
		results = append(results, rows...)
	}
	sortResults(results)
	if len(results) > topK {
		results = results[:topK]
	}
	return cloneResults(results), nil
}

// ListDocuments lists documents visible under the metadata filter.
func (s *Store) ListDocuments(
	ctx context.Context,
	collectionName string,
	metadataFilter map[string]any,
) ([]rag.DocumentSummary, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("%w: milvus store is nil", rag.ErrInvalidInput)
	}

	filter, err := metadataExpr(metadataFilter)
	if err != nil {
		return nil, err
	}
	if filter == "" {
		filter = fieldID + " >= 0"
	}

	summaries := map[string]rag.DocumentSummary{}
	for offset := 0; ; offset += defaultQueryLimit {
		resultSet, err := s.client.Query(ctx, milvusclient.NewQueryOption(strings.TrimSpace(collectionName)).
			WithFilter(filter).
			WithOutputFields(outputFields...).
			WithLimit(defaultQueryLimit).
			WithOffset(offset).
			WithConsistencyLevel(entity.ClStrong))
		if err != nil {
			return nil, err
		}
		rows, err := resultSetToResults(resultSet)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			summary := summaries[row.DocumentID]
			if summary.DocumentID == "" {
				summary = rag.DocumentSummary{
					DocumentID: row.DocumentID,
					Source:     row.Chunk.Source,
					Metadata:   utils.CloneAnyMap(row.Chunk.Metadata),
				}
			}
			summary.ChunkCount++
			summaries[row.DocumentID] = summary
		}
		if len(rows) < defaultQueryLimit {
			break
		}
	}

	out := make([]rag.DocumentSummary, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, summary.Clone())
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].DocumentID < out[j].DocumentID
	})
	return out, nil
}

func (s *Store) loadCollection(ctx context.Context, name string) error {
	loadTask, err := s.client.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(name))
	if err != nil {
		return err
	}
	return loadTask.Await(ctx)
}

func newSchema(collectionName string, dimensions int) *entity.Schema {
	return entity.NewSchema().
		WithName(collectionName).
		WithField(entity.NewField().WithName(fieldID).WithDataType(entity.FieldTypeInt64).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName(fieldVector).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(dimensions))).
		WithField(entity.NewField().WithName(fieldDocumentID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(maxDocumentIDLength)).
		WithField(entity.NewField().WithName(fieldSource).WithDataType(entity.FieldTypeVarChar).WithMaxLength(maxSourceLength)).
		WithField(entity.NewField().WithName(fieldChunkIndex).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldTotalChunks).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldContentJSON).WithDataType(entity.FieldTypeVarChar).WithMaxLength(maxContentLength)).
		WithField(entity.NewField().WithName(fieldMetadata).WithDataType(entity.FieldTypeJSON))
}

func recordsToColumns(records []rag.VectorRecord) ([]column.Column, error) {
	ids := make([]int64, 0, len(records))
	vectors := make([][]float32, 0, len(records))
	documentIDs := make([]string, 0, len(records))
	sources := make([]string, 0, len(records))
	chunkIndexes := make([]int64, 0, len(records))
	totalChunks := make([]int64, 0, len(records))
	contentJSON := make([]string, 0, len(records))
	metadataJSON := make([][]byte, 0, len(records))

	dimensions := 0
	for _, record := range records {
		if strings.TrimSpace(record.DocumentID) == "" {
			return nil, fmt.Errorf("%w: document id is required", rag.ErrInvalidInput)
		}
		if len(record.Vector) == 0 {
			return nil, fmt.Errorf("%w: record vector is required", rag.ErrInvalidInput)
		}
		if dimensions == 0 {
			dimensions = len(record.Vector)
		}
		if len(record.Vector) != dimensions {
			return nil, fmt.Errorf("%w: records have mixed vector dimensions", rag.ErrInvalidInput)
		}
		if record.Chunk.Content == nil {
			return nil, fmt.Errorf("%w: chunk content is required", rag.ErrInvalidInput)
		}

		content, err := json.Marshal(record.Chunk.Content)
		if err != nil {
			return nil, err
		}
		if len(content) > maxContentLength {
			return nil, fmt.Errorf("%w: chunk content json exceeds %d bytes", rag.ErrInvalidInput, maxContentLength)
		}
		metadata, err := json.Marshal(record.Chunk.Metadata)
		if err != nil {
			return nil, err
		}

		ids = append(ids, rowID(record.DocumentID, record.Chunk.ChunkIndex))
		vectors = append(vectors, embeddingToFloat32(record.Vector))
		documentIDs = append(documentIDs, record.DocumentID)
		sources = append(sources, record.Chunk.Source)
		chunkIndexes = append(chunkIndexes, int64(record.Chunk.ChunkIndex))
		totalChunks = append(totalChunks, int64(record.Chunk.TotalChunks))
		contentJSON = append(contentJSON, string(content))
		metadataJSON = append(metadataJSON, metadata)
	}

	return []column.Column{
		column.NewColumnInt64(fieldID, ids),
		column.NewColumnFloatVector(fieldVector, dimensions, vectors),
		column.NewColumnVarChar(fieldDocumentID, documentIDs),
		column.NewColumnVarChar(fieldSource, sources),
		column.NewColumnInt64(fieldChunkIndex, chunkIndexes),
		column.NewColumnInt64(fieldTotalChunks, totalChunks),
		column.NewColumnVarChar(fieldContentJSON, contentJSON),
		column.NewColumnJSONBytes(fieldMetadata, metadataJSON),
	}, nil
}

func resultSetToResults(resultSet milvusclient.ResultSet) ([]rag.VectorSearchResult, error) {
	out := make([]rag.VectorSearchResult, 0, resultSet.Len())
	for index := 0; index < resultSet.Len(); index++ {
		result, err := resultSetRowToResult(resultSet, index)
		if err != nil {
			return nil, err
		}
		if index < len(resultSet.Scores) {
			result.Score = float64(resultSet.Scores[index])
		}
		out = append(out, result)
	}
	return out, nil
}

func resultSetRowToResult(resultSet milvusclient.ResultSet, row int) (rag.VectorSearchResult, error) {
	documentID, err := columnString(resultSet, fieldDocumentID, row)
	if err != nil {
		return rag.VectorSearchResult{}, err
	}
	source, err := columnString(resultSet, fieldSource, row)
	if err != nil {
		return rag.VectorSearchResult{}, err
	}
	chunkIndex, err := columnInt(resultSet, fieldChunkIndex, row)
	if err != nil {
		return rag.VectorSearchResult{}, err
	}
	totalChunks, err := columnInt(resultSet, fieldTotalChunks, row)
	if err != nil {
		return rag.VectorSearchResult{}, err
	}
	contentJSON, err := columnString(resultSet, fieldContentJSON, row)
	if err != nil {
		return rag.VectorSearchResult{}, err
	}
	content, err := message.UnmarshalContentBlock([]byte(contentJSON))
	if err != nil {
		return rag.VectorSearchResult{}, err
	}
	metadata, err := columnJSONMap(resultSet, fieldMetadata, row)
	if err != nil {
		return rag.VectorSearchResult{}, err
	}

	return rag.VectorSearchResult{
		DocumentID: documentID,
		Chunk: rag.Chunk{
			Content:     content,
			Source:      source,
			ChunkIndex:  chunkIndex,
			TotalChunks: totalChunks,
			Metadata:    metadata,
		},
	}, nil
}

func columnString(resultSet milvusclient.ResultSet, field string, row int) (string, error) {
	col := resultSet.GetColumn(field)
	if col == nil {
		return "", fmt.Errorf("%w: missing milvus field %q", rag.ErrInvalidInput, field)
	}
	value, err := col.GetAsString(row)
	if err != nil {
		return "", err
	}
	return value, nil
}

func columnInt(resultSet milvusclient.ResultSet, field string, row int) (int, error) {
	col := resultSet.GetColumn(field)
	if col == nil {
		return 0, fmt.Errorf("%w: missing milvus field %q", rag.ErrInvalidInput, field)
	}
	value, err := col.GetAsInt64(row)
	if err != nil {
		return 0, err
	}
	return int(value), nil
}

func columnJSONMap(resultSet milvusclient.ResultSet, field string, row int) (map[string]any, error) {
	col := resultSet.GetColumn(field)
	if col == nil {
		return nil, fmt.Errorf("%w: missing milvus field %q", rag.ErrInvalidInput, field)
	}
	raw, err := col.Get(row)
	if err != nil {
		return nil, err
	}
	var data []byte
	switch typed := raw.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return nil, fmt.Errorf("%w: unexpected milvus json field %q type %T", rag.ErrInvalidInput, field, raw)
	}
	if len(data) == 0 {
		return nil, nil
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func metadataExpr(metadata map[string]any) (string, error) {
	if len(metadata) == 0 {
		return "", nil
	}
	conditions := make([]string, 0, len(metadata))
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		if key == "" {
			return "", fmt.Errorf("%w: metadata filter key is required", rag.ErrInvalidInput)
		}
		literal, err := milvusLiteral(value)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, fmt.Sprintf("%s[%s] == %s", fieldMetadata, milvusStringLiteral(key), literal))
	}
	sort.Strings(conditions)
	return strings.Join(conditions, " && "), nil
}

func milvusLiteral(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return milvusStringLiteral(typed), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case int:
		return strconv.FormatInt(int64(typed), 10), nil
	case int8:
		return strconv.FormatInt(int64(typed), 10), nil
	case int16:
		return strconv.FormatInt(int64(typed), 10), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case uint:
		return milvusUnsignedLiteral(uint64(typed))
	case uint8:
		return milvusUnsignedLiteral(uint64(typed))
	case uint16:
		return milvusUnsignedLiteral(uint64(typed))
	case uint32:
		return milvusUnsignedLiteral(uint64(typed))
	case uint64:
		return milvusUnsignedLiteral(typed)
	case float32:
		return strconv.FormatFloat(float64(typed), 'g', -1, 32), nil
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("%w: unsupported metadata filter value %T", rag.ErrInvalidInput, value)
	}
}

func milvusUnsignedLiteral(value uint64) (string, error) {
	if value > math.MaxInt64 {
		return "", fmt.Errorf("%w: metadata integer overflows int64", rag.ErrInvalidInput)
	}
	return strconv.FormatUint(value, 10), nil
}

func milvusStringLiteral(value string) string {
	return strconv.Quote(value)
}

func embeddingToFloat32(vector types.Embedding) []float32 {
	out := make([]float32, 0, len(vector))
	for _, value := range vector {
		out = append(out, float32(value))
	}
	return out
}

func rowID(documentID string, chunkIndex int) int64 {
	sum := sha256.Sum256([]byte(documentID + "\x00" + strconv.Itoa(chunkIndex)))
	id := int64(binary.BigEndian.Uint64(sum[:8]) & math.MaxInt64)
	if id == 0 {
		return 1
	}
	return id
}

func sortResults(results []rag.VectorSearchResult) {
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

func cloneResults(results []rag.VectorSearchResult) []rag.VectorSearchResult {
	out := make([]rag.VectorSearchResult, 0, len(results))
	for _, result := range results {
		out = append(out, result.Clone())
	}
	return out
}
