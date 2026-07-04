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
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	qdrantapi "github.com/qdrant/go-client/qdrant"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

const (
	payloadDocumentID  = "document_id"
	payloadSource      = "source"
	payloadChunkIndex  = "chunk_index"
	payloadTotalChunks = "total_chunks"
	payloadContentJSON = "content_json"
	payloadMetadata    = "metadata_json"
	metadataKeyPrefix  = "metadata__"

	defaultScrollLimit = 256
)

// Distance is the similarity metric used when creating Qdrant collections.
type Distance string

const (
	DistanceCosine    Distance = "Cosine"
	DistanceDot       Distance = "Dot"
	DistanceEuclid    Distance = "Euclid"
	DistanceManhattan Distance = "Manhattan"
)

// Config configures a Qdrant client created by Connect.
type Config struct {
	Host                   string
	Port                   int
	APIKey                 string
	UseTLS                 bool
	SkipCompatibilityCheck bool
	Distance               Distance
}

// StoreOption configures Store.
type StoreOption func(*Store) error

// Store implements rag.VectorStore on top of Qdrant.
type Store struct {
	client   *qdrantapi.Client
	distance qdrantapi.Distance
}

var _ rag.VectorStore = (*Store)(nil)

// Connect creates a Qdrant client and wraps it as a vector store.
func Connect(config Config) (*Store, error) {
	host := strings.TrimSpace(config.Host)
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	port := config.Port
	if port == 0 {
		port = 6334
	}
	client, err := qdrantapi.NewClient(&qdrantapi.Config{
		Host:                   host,
		Port:                   port,
		APIKey:                 config.APIKey,
		UseTLS:                 config.UseTLS,
		SkipCompatibilityCheck: config.SkipCompatibilityCheck,
	})
	if err != nil {
		return nil, err
	}
	store, err := NewStore(client, WithDistance(config.Distance))
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return store, nil
}

// NewStore wraps an existing Qdrant client.
func NewStore(client *qdrantapi.Client, opts ...StoreOption) (*Store, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: qdrant client is nil", rag.ErrInvalidInput)
	}
	store := &Store{
		client:   client,
		distance: qdrantapi.Distance_Cosine,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(store); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// WithDistance sets the metric used when creating new collections.
func WithDistance(distance Distance) StoreOption {
	return func(store *Store) error {
		parsed, err := parseDistance(distance)
		if err != nil {
			return err
		}
		store.distance = parsed
		return nil
	}
}

// Close closes the underlying Qdrant client.
func (s *Store) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

// CreateCollection creates a Qdrant collection if it does not already exist.
func (s *Store) CreateCollection(ctx context.Context, name string, dimensions int) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: collection name is required", rag.ErrInvalidInput)
	}
	if dimensions <= 0 {
		return fmt.Errorf("%w: collection dimensions must be positive", rag.ErrInvalidInput)
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("%w: qdrant store is nil", rag.ErrInvalidInput)
	}

	exists, err := s.client.CollectionExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.client.CreateCollection(ctx, &qdrantapi.CreateCollection{
		CollectionName: name,
		VectorsConfig: qdrantapi.NewVectorsConfig(&qdrantapi.VectorParams{
			Size:     uint64(dimensions),
			Distance: s.distance,
		}),
	})
}

// DeleteCollection deletes a Qdrant collection.
func (s *Store) DeleteCollection(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: collection name is required", rag.ErrInvalidInput)
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("%w: qdrant store is nil", rag.ErrInvalidInput)
	}
	exists, err := s.client.CollectionExists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return s.client.DeleteCollection(ctx, name)
}

// HasCollection reports whether a Qdrant collection exists.
func (s *Store) HasCollection(ctx context.Context, name string) (bool, error) {
	if s == nil || s.client == nil {
		return false, fmt.Errorf("%w: qdrant store is nil", rag.ErrInvalidInput)
	}
	return s.client.CollectionExists(ctx, strings.TrimSpace(name))
}

// Insert upserts vector records into Qdrant.
func (s *Store) Insert(ctx context.Context, collection string, records []rag.VectorRecord) error {
	if len(records) == 0 {
		return nil
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("%w: qdrant store is nil", rag.ErrInvalidInput)
	}

	points := make([]*qdrantapi.PointStruct, 0, len(records))
	for _, record := range records {
		point, err := recordToPoint(record)
		if err != nil {
			return err
		}
		points = append(points, point)
	}

	wait := true
	_, err := s.client.Upsert(ctx, &qdrantapi.UpsertPoints{
		CollectionName: strings.TrimSpace(collection),
		Wait:           &wait,
		Points:         points,
	})
	return err
}

// Delete removes all points for a document id.
func (s *Store) Delete(ctx context.Context, collection string, documentID string) error {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return fmt.Errorf("%w: document id is required", rag.ErrInvalidInput)
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("%w: qdrant store is nil", rag.ErrInvalidInput)
	}

	wait := true
	_, err := s.client.Delete(ctx, &qdrantapi.DeletePoints{
		CollectionName: strings.TrimSpace(collection),
		Wait:           &wait,
		Points: qdrantapi.NewPointsSelectorFilter(&qdrantapi.Filter{
			Must: []*qdrantapi.Condition{qdrantapi.NewMatch(payloadDocumentID, documentID)},
		}),
	})
	return err
}

// Search returns topK records ranked by Qdrant's similarity score.
func (s *Store) Search(
	ctx context.Context,
	collection string,
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
		return nil, fmt.Errorf("%w: qdrant store is nil", rag.ErrInvalidInput)
	}

	filter, err := buildMetadataFilter(metadataFilter)
	if err != nil {
		return nil, err
	}
	limit := uint64(topK)
	points, err := s.client.Query(ctx, &qdrantapi.QueryPoints{
		CollectionName: strings.TrimSpace(collection),
		Query:          qdrantapi.NewQuery(embeddingToFloat32(queryVector)...),
		Filter:         filter,
		Limit:          &limit,
		WithPayload:    qdrantapi.NewWithPayload(true),
	})
	if err != nil {
		return nil, err
	}

	results := make([]rag.VectorSearchResult, 0, len(points))
	for _, point := range points {
		result, err := pointPayloadToResult(point.GetPayload(), float64(point.GetScore()))
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return cloneAndSortResults(results), nil
}

// ListDocuments lists documents visible under the metadata filter.
func (s *Store) ListDocuments(
	ctx context.Context,
	collection string,
	metadataFilter map[string]any,
) ([]rag.DocumentSummary, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("%w: qdrant store is nil", rag.ErrInvalidInput)
	}

	filter, err := buildMetadataFilter(metadataFilter)
	if err != nil {
		return nil, err
	}
	limit := uint32(defaultScrollLimit)
	var offset *qdrantapi.PointId
	summaries := map[string]rag.DocumentSummary{}
	for {
		points, nextOffset, err := s.client.ScrollAndOffset(ctx, &qdrantapi.ScrollPoints{
			CollectionName: strings.TrimSpace(collection),
			Filter:         filter,
			Offset:         offset,
			Limit:          &limit,
			WithPayload:    qdrantapi.NewWithPayload(true),
		})
		if err != nil {
			return nil, err
		}
		for _, point := range points {
			result, err := pointPayloadToResult(point.GetPayload(), 0)
			if err != nil {
				return nil, err
			}
			summary := summaries[result.DocumentID]
			if summary.DocumentID == "" {
				summary = rag.DocumentSummary{
					DocumentID: result.DocumentID,
					Source:     result.Chunk.Source,
					Metadata:   utils.CloneAnyMap(result.Chunk.Metadata),
				}
			}
			summary.ChunkCount++
			summaries[result.DocumentID] = summary
		}
		if nextOffset == nil {
			break
		}
		offset = nextOffset
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

func recordToPoint(record rag.VectorRecord) (*qdrantapi.PointStruct, error) {
	if strings.TrimSpace(record.DocumentID) == "" {
		return nil, fmt.Errorf("%w: document id is required", rag.ErrInvalidInput)
	}
	if len(record.Vector) == 0 {
		return nil, fmt.Errorf("%w: record vector is required", rag.ErrInvalidInput)
	}
	if record.Chunk.Content == nil {
		return nil, fmt.Errorf("%w: chunk content is required", rag.ErrInvalidInput)
	}

	payload, err := recordPayload(record)
	if err != nil {
		return nil, err
	}
	return &qdrantapi.PointStruct{
		Id:      qdrantapi.NewIDNum(pointID(record.DocumentID, record.Chunk.ChunkIndex)),
		Payload: payload,
		Vectors: qdrantapi.NewVectors(embeddingToFloat32(record.Vector)...),
	}, nil
}

func recordPayload(record rag.VectorRecord) (map[string]*qdrantapi.Value, error) {
	contentJSON, err := json.Marshal(record.Chunk.Content)
	if err != nil {
		return nil, err
	}
	metadataJSON, err := json.Marshal(record.Chunk.Metadata)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		payloadDocumentID:  record.DocumentID,
		payloadSource:      record.Chunk.Source,
		payloadChunkIndex:  int64(record.Chunk.ChunkIndex),
		payloadTotalChunks: int64(record.Chunk.TotalChunks),
		payloadContentJSON: string(contentJSON),
		payloadMetadata:    string(metadataJSON),
	}
	for key, value := range record.Chunk.Metadata {
		scalar, ok := qdrantFilterScalar(value)
		if ok && strings.TrimSpace(key) != "" {
			payload[metadataKeyPrefix+key] = scalar
		}
	}
	return qdrantapi.TryValueMap(payload)
}

func pointPayloadToResult(payload map[string]*qdrantapi.Value, score float64) (rag.VectorSearchResult, error) {
	documentID := payloadString(payload, payloadDocumentID)
	if documentID == "" {
		return rag.VectorSearchResult{}, fmt.Errorf("%w: qdrant payload missing document id", rag.ErrInvalidInput)
	}
	contentJSON := payloadString(payload, payloadContentJSON)
	content, err := message.UnmarshalContentBlock([]byte(contentJSON))
	if err != nil {
		return rag.VectorSearchResult{}, err
	}

	var metadata map[string]any
	if metadataJSON := payloadString(payload, payloadMetadata); metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return rag.VectorSearchResult{}, err
		}
	}
	return rag.VectorSearchResult{
		Score:      score,
		DocumentID: documentID,
		Chunk: rag.Chunk{
			Content:     content,
			Source:      payloadString(payload, payloadSource),
			ChunkIndex:  payloadInt(payload, payloadChunkIndex),
			TotalChunks: payloadInt(payload, payloadTotalChunks),
			Metadata:    metadata,
		},
	}, nil
}

func buildMetadataFilter(metadata map[string]any) (*qdrantapi.Filter, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	conditions := make([]*qdrantapi.Condition, 0, len(metadata))
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%w: metadata filter key is required", rag.ErrInvalidInput)
		}
		condition, err := metadataCondition(metadataKeyPrefix+key, value)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	sort.SliceStable(conditions, func(i, j int) bool {
		return conditions[i].String() < conditions[j].String()
	})
	return &qdrantapi.Filter{Must: conditions}, nil
}

func metadataCondition(field string, value any) (*qdrantapi.Condition, error) {
	switch typed := value.(type) {
	case string:
		return qdrantapi.NewMatch(field, typed), nil
	case bool:
		return qdrantapi.NewMatchBool(field, typed), nil
	case int:
		return qdrantapi.NewMatchInt(field, int64(typed)), nil
	case int8:
		return qdrantapi.NewMatchInt(field, int64(typed)), nil
	case int16:
		return qdrantapi.NewMatchInt(field, int64(typed)), nil
	case int32:
		return qdrantapi.NewMatchInt(field, int64(typed)), nil
	case int64:
		return qdrantapi.NewMatchInt(field, typed), nil
	case uint:
		return qdrantUnsignedMatch(field, uint64(typed))
	case uint8:
		return qdrantUnsignedMatch(field, uint64(typed))
	case uint16:
		return qdrantUnsignedMatch(field, uint64(typed))
	case uint32:
		return qdrantUnsignedMatch(field, uint64(typed))
	case uint64:
		return qdrantUnsignedMatch(field, typed)
	case float32:
		value := float64(typed)
		return qdrantapi.NewRange(field, &qdrantapi.Range{Gte: &value, Lte: &value}), nil
	case float64:
		return qdrantapi.NewRange(field, &qdrantapi.Range{Gte: &typed, Lte: &typed}), nil
	default:
		return nil, fmt.Errorf("%w: unsupported metadata filter value for %q", rag.ErrInvalidInput, field)
	}
}

func qdrantUnsignedMatch(field string, value uint64) (*qdrantapi.Condition, error) {
	if value > math.MaxInt64 {
		return nil, fmt.Errorf("%w: metadata integer overflows int64 for %q", rag.ErrInvalidInput, field)
	}
	return qdrantapi.NewMatchInt(field, int64(value)), nil
}

func qdrantFilterScalar(value any) (any, bool) {
	switch typed := value.(type) {
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return typed, true
	default:
		return nil, false
	}
}

func parseDistance(distance Distance) (qdrantapi.Distance, error) {
	switch strings.ToLower(strings.TrimSpace(string(distance))) {
	case "", "cosine":
		return qdrantapi.Distance_Cosine, nil
	case "dot":
		return qdrantapi.Distance_Dot, nil
	case "euclid":
		return qdrantapi.Distance_Euclid, nil
	case "manhattan":
		return qdrantapi.Distance_Manhattan, nil
	default:
		return qdrantapi.Distance_UnknownDistance,
			fmt.Errorf("%w: unsupported qdrant distance %q", rag.ErrInvalidInput, distance)
	}
}

func embeddingToFloat32(vector types.Embedding) []float32 {
	out := make([]float32, 0, len(vector))
	for _, value := range vector {
		out = append(out, float32(value))
	}
	return out
}

func payloadString(payload map[string]*qdrantapi.Value, key string) string {
	if payload == nil || payload[key] == nil {
		return ""
	}
	return payload[key].GetStringValue()
}

func payloadInt(payload map[string]*qdrantapi.Value, key string) int {
	if payload == nil || payload[key] == nil {
		return 0
	}
	value := payload[key]
	if got := value.GetIntegerValue(); got != 0 {
		return int(got)
	}
	if got := value.GetDoubleValue(); got != 0 {
		return int(got)
	}
	if got := value.GetStringValue(); got != "" {
		parsed, _ := strconv.Atoi(got)
		return parsed
	}
	return 0
}

func pointID(documentID string, chunkIndex int) uint64 {
	sum := sha256.Sum256([]byte(documentID + "\x00" + strconv.Itoa(chunkIndex)))
	id := binary.BigEndian.Uint64(sum[:8])
	if id == 0 {
		return 1
	}
	return id
}

func cloneAndSortResults(results []rag.VectorSearchResult) []rag.VectorSearchResult {
	out := make([]rag.VectorSearchResult, 0, len(results))
	for _, result := range results {
		out = append(out, result.Clone())
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].DocumentID != out[j].DocumentID {
			return out[i].DocumentID < out[j].DocumentID
		}
		return out[i].Chunk.ChunkIndex < out[j].Chunk.ChunkIndex
	})
	return out
}
