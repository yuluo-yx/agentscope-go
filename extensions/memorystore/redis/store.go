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

// Package redis provides a Redis-backed long-term memory store.
package redis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/yuluo-yx/agentscope-go/pkg/middleware"
)

const (
	defaultAddr      = "127.0.0.1:6379"
	defaultKeyPrefix = "agentscope:memory"
	defaultTopK      = 5
)

// Config configures a Redis memory store created by Connect.
type Config struct {
	Addr      string
	Username  string
	Password  string
	DB        int
	KeyPrefix string
}

// StoreOption configures Store.
type StoreOption func(*Store) error

// Store implements middleware.MemoryStore on top of Redis.
type Store struct {
	client    *goredis.Client
	keyPrefix string
	now       func() time.Time
}

var _ middleware.MemoryStore = (*Store)(nil)

// Connect creates a Redis client and wraps it as a memory store.
func Connect(config Config) (*Store, error) {
	addr := strings.TrimSpace(config.Addr)
	if addr == "" {
		addr = defaultAddr
	}
	client := goredis.NewClient(&goredis.Options{
		Addr:     addr,
		Username: config.Username,
		Password: config.Password,
		DB:       config.DB,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	store, _ := NewStore(client, WithKeyPrefix(config.KeyPrefix))
	return store, nil
}

// NewStore wraps an existing Redis client.
func NewStore(client *goredis.Client, opts ...StoreOption) (*Store, error) {
	if client == nil {
		return nil, fmt.Errorf("memorystore/redis: client is nil")
	}
	store := &Store{
		client:    client,
		keyPrefix: defaultKeyPrefix,
		now:       time.Now,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(store); err != nil {
			return nil, err
		}
	}
	store.keyPrefix = normalizeKeyPrefix(store.keyPrefix)
	return store, nil
}

// WithKeyPrefix sets the Redis key prefix used by Store.
func WithKeyPrefix(prefix string) StoreOption {
	return func(store *Store) error {
		store.keyPrefix = prefix
		return nil
	}
}

// Close closes the underlying Redis client.
func (s *Store) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

// Add stores one long-term memory entry.
func (s *Store) Add(ctx context.Context, entry middleware.MemoryEntry) error {
	ctx = storeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.client == nil {
		return fmt.Errorf("memorystore/redis: store is nil")
	}

	userID := strings.TrimSpace(entry.UserID)
	if userID == "" {
		return fmt.Errorf("memorystore/redis: user id is required")
	}
	text := memoryEntryText(entry)
	if text == "" {
		return fmt.Errorf("memorystore/redis: memory text is required")
	}

	id, err := s.client.Incr(ctx, s.key("next_id")).Result()
	if err != nil {
		return err
	}
	record := redisMemoryRecord{
		ID:        strconv.FormatInt(id, 10),
		UserID:    userID,
		AgentID:   strings.TrimSpace(entry.AgentID),
		Text:      text,
		Metadata:  cloneMap(entry.Metadata),
		SessionID: strings.TrimSpace(entry.SessionID),
		ReplyID:   strings.TrimSpace(entry.ReplyID),
		CreatedAt: s.now().UTC(),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}

	pipe := s.client.TxPipeline()
	pipe.Set(ctx, s.recordKey(record.ID), data, 0)
	pipe.ZAdd(ctx, s.scopeKey(record.UserID, record.AgentID), goredis.Z{
		Score:  float64(record.CreatedAt.UnixMicro()),
		Member: record.ID,
	})
	_, err = pipe.Exec(ctx)
	return err
}

// Search returns memories in the requested user/agent scope ranked by simple text relevance.
func (s *Store) Search(ctx context.Context, query middleware.MemoryQuery) ([]middleware.MemoryRecord, error) {
	ctx = storeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("memorystore/redis: store is nil")
	}

	userID := strings.TrimSpace(query.UserID)
	if userID == "" {
		return nil, fmt.Errorf("memorystore/redis: user id is required")
	}
	topK := query.TopK
	if topK <= 0 {
		topK = defaultTopK
	}

	// ponytail: scans one user's memories; add RediSearch or vector indexing when scope size matters.
	ids, err := s.client.ZRevRange(ctx, s.scopeKey(userID, strings.TrimSpace(query.AgentID)), 0, -1).Result()
	if err != nil {
		return nil, err
	}

	terms := queryTerms(query.Query)
	scored := make([]scoredRedisMemory, 0, len(ids))
	for _, id := range ids {
		record, ok, err := s.getRecord(ctx, id)
		if err != nil {
			return nil, err
		}
		if !ok || !metadataMatches(record.Metadata, query.Metadata) {
			continue
		}
		score := textScore(record.Text, terms)
		if len(terms) > 0 && score == 0 {
			continue
		}
		scored = append(scored, scoredRedisMemory{record: record, score: score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].record.CreatedAt.After(scored[j].record.CreatedAt)
	})
	if len(scored) > topK {
		scored = scored[:topK]
	}

	out := make([]middleware.MemoryRecord, 0, len(scored))
	for _, item := range scored {
		out = append(out, middleware.MemoryRecord{
			ID:       item.record.ID,
			Text:     item.record.Text,
			Score:    item.score,
			Metadata: cloneMap(item.record.Metadata),
		})
	}
	return out, nil
}

func (s *Store) getRecord(ctx context.Context, id string) (redisMemoryRecord, bool, error) {
	data, err := s.client.Get(ctx, s.recordKey(id)).Bytes()
	if err == goredis.Nil {
		return redisMemoryRecord{}, false, nil
	}
	if err != nil {
		return redisMemoryRecord{}, false, err
	}
	record := redisMemoryRecord{}
	if err := json.Unmarshal(data, &record); err != nil {
		return redisMemoryRecord{}, false, err
	}
	return record, true, nil
}

func (s *Store) key(parts ...string) string {
	return s.keyPrefix + ":" + strings.Join(parts, ":")
}

func (s *Store) recordKey(id string) string {
	return s.key("record", id)
}

func (s *Store) scopeKey(userID string, agentID string) string {
	scope := base64.RawURLEncoding.EncodeToString([]byte(userID + "\x00" + agentID))
	return s.key("scope", scope)
}

type redisMemoryRecord struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	AgentID   string         `json:"agent_id,omitempty"`
	Text      string         `json:"text"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	ReplyID   string         `json:"reply_id,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type scoredRedisMemory struct {
	record redisMemoryRecord
	score  float64
}

func normalizeKeyPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = defaultKeyPrefix
	}
	return strings.TrimRight(prefix, ":")
}

func storeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func memoryEntryText(entry middleware.MemoryEntry) string {
	input := strings.TrimSpace(entry.Input)
	output := strings.TrimSpace(entry.Output)
	switch {
	case input == "":
		return output
	case output == "":
		return input
	default:
		return "User: " + input + "\nAssistant: " + output
	}
}

func queryTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " \t\r\n.,;:!?()[]{}\"'`")
		if field != "" {
			terms = append(terms, field)
		}
	}
	return terms
}

func textScore(text string, terms []string) float64 {
	if len(terms) == 0 {
		return 1
	}
	lower := strings.ToLower(text)
	score := 0.0
	phrase := strings.Join(terms, " ")
	if phrase != "" && strings.Contains(lower, phrase) {
		score += float64(len(terms) + 1)
	}
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score++
		}
	}
	return score
}

func metadataMatches(metadata map[string]any, filter map[string]any) bool {
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

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
