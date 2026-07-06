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

// Package mysql provides a MySQL-backed long-term memory store.
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"

	_ "github.com/go-sql-driver/mysql"
	"github.com/yuluo-yx/agentscope-go/pkg/middleware"
)

const (
	defaultTable = "agentscope_memories"
	defaultTopK  = 5
)

// Config configures a MySQL memory store created by Connect.
type Config struct {
	DSN   string
	Table string
}

// StoreOption configures Store.
type StoreOption func(*Store) error

// Store implements middleware.MemoryStore on top of MySQL.
type Store struct {
	db          *sql.DB
	table       string
	schemaMu    sync.Mutex
	schemaReady bool
}

var _ middleware.MemoryStore = (*Store)(nil)

var openDB = sql.Open

// Connect opens a MySQL database handle and wraps it as a memory store.
func Connect(config Config) (*Store, error) {
	dsn := strings.TrimSpace(config.DSN)
	if dsn == "" {
		return nil, fmt.Errorf("memorystore/mysql: dsn is required")
	}
	db, err := openDB("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	store, err := NewStore(db, WithTable(config.Table))
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// NewStore wraps an existing MySQL database handle.
func NewStore(db *sql.DB, opts ...StoreOption) (*Store, error) {
	store := &Store{
		db:    db,
		table: defaultTable,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(store); err != nil {
			return nil, err
		}
	}
	if store.db == nil {
		return nil, fmt.Errorf("memorystore/mysql: db is nil")
	}
	return store, nil
}

// WithTable sets the table used by Store.
func WithTable(table string) StoreOption {
	return func(store *Store) error {
		table = strings.TrimSpace(table)
		if table == "" {
			store.table = defaultTable
			return nil
		}
		if !validTableName(table) {
			return fmt.Errorf("memorystore/mysql: unsafe table name %q", table)
		}
		store.table = table
		return nil
	}
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Add stores one long-term memory entry.
func (s *Store) Add(ctx context.Context, entry middleware.MemoryEntry) error {
	ctx = storeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("memorystore/mysql: store is nil")
	}
	if err := s.ensureSchema(ctx); err != nil {
		return err
	}

	userID := strings.TrimSpace(entry.UserID)
	if userID == "" {
		return fmt.Errorf("memorystore/mysql: user id is required")
	}
	text := memoryEntryText(entry)
	if text == "" {
		return fmt.Errorf("memorystore/mysql: memory text is required")
	}

	metadata, err := metadataJSON(entry.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(
		ctx,
		"INSERT INTO "+s.quotedTable()+" (user_id, agent_id, text, input_text, output_text, session_id, reply_id, metadata_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		userID,
		strings.TrimSpace(entry.AgentID),
		text,
		nullableString(entry.Input),
		nullableString(entry.Output),
		nullableString(entry.SessionID),
		nullableString(entry.ReplyID),
		metadata,
	)
	return err
}

// Search returns memories in the requested user/agent scope ranked by simple text relevance.
func (s *Store) Search(ctx context.Context, query middleware.MemoryQuery) ([]middleware.MemoryRecord, error) {
	ctx = storeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("memorystore/mysql: store is nil")
	}
	if err := s.ensureSchema(ctx); err != nil {
		return nil, err
	}

	userID := strings.TrimSpace(query.UserID)
	if userID == "" {
		return nil, fmt.Errorf("memorystore/mysql: user id is required")
	}
	topK := query.TopK
	if topK <= 0 {
		topK = defaultTopK
	}

	// ponytail: scans one user's memories; add FULLTEXT or vector indexing when scope size matters.
	rows, err := s.db.QueryContext(
		ctx,
		"SELECT id, text, metadata_json FROM "+s.quotedTable()+" WHERE user_id = ? AND agent_id = ? ORDER BY id DESC",
		userID,
		strings.TrimSpace(query.AgentID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	terms := queryTerms(query.Query)
	scored := []scoredMySQLMemory{}
	for rows.Next() {
		record, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		if !metadataMatches(record.Metadata, query.Metadata) {
			continue
		}
		score := textScore(record.Text, terms)
		if len(terms) > 0 && score == 0 {
			continue
		}
		scored = append(scored, scoredMySQLMemory{record: record, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].record.ID > scored[j].record.ID
	})
	if len(scored) > topK {
		scored = scored[:topK]
	}

	out := make([]middleware.MemoryRecord, 0, len(scored))
	for _, item := range scored {
		out = append(out, middleware.MemoryRecord{
			ID:       fmt.Sprint(item.record.ID),
			Text:     item.record.Text,
			Score:    item.score,
			Metadata: cloneMap(item.record.Metadata),
		})
	}
	return out, nil
}

func (s *Store) ensureSchema(ctx context.Context) error {
	ctx = storeContext(ctx)
	s.schemaMu.Lock()
	defer s.schemaMu.Unlock()
	if s.schemaReady {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+s.quotedTable()+` (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id VARCHAR(191) NOT NULL,
  agent_id VARCHAR(191) NOT NULL DEFAULT '',
  text LONGTEXT NOT NULL,
  input_text LONGTEXT NULL,
  output_text LONGTEXT NULL,
  session_id VARCHAR(191) NULL,
  reply_id VARCHAR(191) NULL,
  metadata_json JSON NULL,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  KEY idx_scope_created (user_id, agent_id, created_at),
  KEY idx_session (session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		return err
	}
	s.schemaReady = true
	return nil
}

func (s *Store) quotedTable() string {
	return "`" + s.table + "`"
}

type mysqlMemoryRecord struct {
	ID       uint64
	Text     string
	Metadata map[string]any
}

type scoredMySQLMemory struct {
	record mysqlMemoryRecord
	score  float64
}

func scanMemory(rows *sql.Rows) (mysqlMemoryRecord, error) {
	record := mysqlMemoryRecord{}
	metadata := sql.NullString{}
	if err := rows.Scan(&record.ID, &record.Text, &metadata); err != nil {
		return mysqlMemoryRecord{}, err
	}
	if metadata.Valid && strings.TrimSpace(metadata.String) != "" {
		if err := json.Unmarshal([]byte(metadata.String), &record.Metadata); err != nil {
			return mysqlMemoryRecord{}, err
		}
	}
	return record, nil
}

func validTableName(table string) bool {
	if table == "" {
		return false
	}
	for _, r := range table {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
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

func metadataJSON(metadata map[string]any) (any, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
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
