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

package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/yuluo-yx/agentscope-go/pkg/middleware"
)

func TestStoreImplementsMemoryStore(t *testing.T) {
	var _ middleware.MemoryStore = (*Store)(nil)
}

func TestConnect(t *testing.T) {
	if _, err := Connect(Config{}); err == nil {
		t.Fatal("Connect should require DSN")
	}

	oldOpenDB := openDB
	t.Cleanup(func() { openDB = oldOpenDB })

	errBoom := errors.New("boom")
	openDB = func(string, string) (*sql.DB, error) {
		return nil, errBoom
	}
	if _, err := Connect(Config{DSN: "dsn"}); !errors.Is(err, errBoom) {
		t.Fatalf("Connect open error = %v, want %v", err, errBoom)
	}

	db, mock := newMockDB(t)
	mock.ExpectPing().WillReturnError(errBoom)
	mock.ExpectClose()
	openDB = func(string, string) (*sql.DB, error) {
		return db, nil
	}
	if _, err := Connect(Config{DSN: "dsn"}); !errors.Is(err, errBoom) {
		t.Fatalf("Connect ping error = %v, want %v", err, errBoom)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	db, mock = newMockDB(t)
	mock.ExpectPing()
	mock.ExpectClose()
	openDB = func(string, string) (*sql.DB, error) {
		return db, nil
	}
	if _, err := Connect(Config{DSN: "dsn", Table: "bad-table"}); err == nil {
		t.Fatal("Connect should reject unsafe table names")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	db, mock = newMockDB(t)
	mock.ExpectPing()
	expectSchema(mock, "memories").WillReturnError(errBoom)
	mock.ExpectClose()
	openDB = func(string, string) (*sql.DB, error) {
		return db, nil
	}
	if _, err := Connect(Config{DSN: "dsn", Table: "memories"}); !errors.Is(err, errBoom) {
		t.Fatalf("Connect schema error = %v, want %v", err, errBoom)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	db, mock = newMockDB(t)
	mock.ExpectPing()
	expectSchema(mock, "memories").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectClose()
	openDB = func(string, string) (*sql.DB, error) {
		return db, nil
	}
	store, err := Connect(Config{DSN: "dsn", Table: "memories"})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestNewStoreWithTableAndClose(t *testing.T) {
	db, mock := newMockDB(t)
	store, err := NewStore(db, nil, WithTable(""))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	if store.table != defaultTable {
		t.Fatalf("table = %q, want %q", store.table, defaultTable)
	}
	if _, err := NewStore(nil); err == nil {
		t.Fatal("NewStore should reject nil db")
	}
	if _, err := NewStore(db, WithTable("bad-table")); err == nil {
		t.Fatal("NewStore should reject unsafe table names")
	}

	var nilStore *Store
	if err := nilStore.Close(); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if err := (&Store{}).Close(); err != nil {
		t.Fatalf("empty Close returned error: %v", err)
	}
	mock.ExpectClose()
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestAddValidationAndErrors(t *testing.T) {
	store, mock := newReadyMockStore(t, "memories")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Add(ctx, middleware.MemoryEntry{UserID: "u", Input: "memory"}); err == nil {
		t.Fatal("Add should return context error")
	}

	var nilStore *Store
	if err := nilStore.Add(context.Background(), middleware.MemoryEntry{UserID: "u", Input: "memory"}); err == nil {
		t.Fatal("Add should reject a nil store")
	}
	if err := (&Store{}).Add(context.Background(), middleware.MemoryEntry{UserID: "u", Input: "memory"}); err == nil {
		t.Fatal("Add should reject a store without db")
	}

	errBoom := errors.New("boom")
	db, schemaMock := newMockDB(t)
	schemaStore, err := NewStore(db, WithTable("memories"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	expectSchema(schemaMock, "memories").WillReturnError(errBoom)
	if err := schemaStore.Add(context.Background(), middleware.MemoryEntry{UserID: "u", Input: "memory"}); !errors.Is(err, errBoom) {
		t.Fatalf("Add schema error = %v, want %v", err, errBoom)
	}
	if err := schemaMock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	if err := store.Add(context.Background(), middleware.MemoryEntry{Input: "memory"}); err == nil {
		t.Fatal("Add should require user id")
	}
	if err := store.Add(context.Background(), middleware.MemoryEntry{UserID: "u"}); err == nil {
		t.Fatal("Add should require memory text")
	}
	if err := store.Add(context.Background(), middleware.MemoryEntry{
		UserID:   "u",
		Input:    "memory",
		Metadata: map[string]any{"bad": func() {}},
	}); err == nil {
		t.Fatal("Add should return metadata marshal error")
	}

	expectInsert(mock, "memories").WithArgs(
		"alice",
		"friday",
		"User: hi\nAssistant: there",
		"hi",
		"there",
		"s1",
		"r1",
		`{"kind":"preference"}`,
	).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.Add(nil, middleware.MemoryEntry{
		UserID:    " alice ",
		AgentID:   " friday ",
		Input:     " hi ",
		Output:    " there ",
		SessionID: " s1 ",
		ReplyID:   " r1 ",
		Metadata:  map[string]any{"kind": "preference"},
	}); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	store, mock = newReadyMockStore(t, "memories")
	expectInsert(mock, "memories").WillReturnError(errBoom)
	if err := store.Add(context.Background(), middleware.MemoryEntry{UserID: "u", Input: "memory"}); !errors.Is(err, errBoom) {
		t.Fatalf("Add insert error = %v, want %v", err, errBoom)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSearchValidationAndErrors(t *testing.T) {
	store, mock := newReadyMockStore(t, "memories")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Search(ctx, middleware.MemoryQuery{UserID: "u"}); err == nil {
		t.Fatal("Search should return context error")
	}

	var nilStore *Store
	if _, err := nilStore.Search(context.Background(), middleware.MemoryQuery{UserID: "u"}); err == nil {
		t.Fatal("Search should reject a nil store")
	}
	if _, err := (&Store{}).Search(context.Background(), middleware.MemoryQuery{UserID: "u"}); err == nil {
		t.Fatal("Search should reject a store without db")
	}

	errBoom := errors.New("boom")
	db, schemaMock := newMockDB(t)
	schemaStore, err := NewStore(db, WithTable("memories"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	expectSchema(schemaMock, "memories").WillReturnError(errBoom)
	if _, err := schemaStore.Search(context.Background(), middleware.MemoryQuery{UserID: "u"}); !errors.Is(err, errBoom) {
		t.Fatalf("Search schema error = %v, want %v", err, errBoom)
	}
	if err := schemaMock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	if _, err := store.Search(context.Background(), middleware.MemoryQuery{}); err == nil {
		t.Fatal("Search should require user id")
	}

	expectSelect(mock, "memories").WillReturnError(errBoom)
	if _, err := store.Search(context.Background(), middleware.MemoryQuery{UserID: "u"}); !errors.Is(err, errBoom) {
		t.Fatalf("Search query error = %v, want %v", err, errBoom)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSearchResults(t *testing.T) {
	store, mock := newReadyMockStore(t, "memories")
	rows := sqlmock.NewRows([]string{"id", "text", "metadata_json"}).
		AddRow(uint64(3), "Ada likes jasmine tea.", `{"kind":"preference"}`).
		AddRow(uint64(2), "Ada mentions jasmine only.", `{"kind":"preference"}`).
		AddRow(uint64(1), "Ada likes coffee.", `{"kind":"preference"}`).
		AddRow(uint64(4), "Ada likes jasmine tea.", `{"kind":"note"}`)
	expectSelect(mock, "memories").WithArgs("alice", "friday").WillReturnRows(rows)
	results, err := store.Search(nil, middleware.MemoryQuery{
		UserID:   " alice ",
		AgentID:  " friday ",
		Query:    "jasmine tea",
		TopK:     1,
		Metadata: map[string]any{"kind": "preference"},
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("Search returned %d results, want %d: %#v", got, want, results)
	}
	if results[0].ID != "3" || results[0].Metadata["kind"] != "preference" {
		t.Fatalf("Search result mismatch: %#v", results[0])
	}
	results[0].Metadata["kind"] = "changed"
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	store, mock = newReadyMockStore(t, "memories")
	rows = sqlmock.NewRows([]string{"id", "text", "metadata_json"}).
		AddRow(uint64(6), "memory 6", nil).
		AddRow(uint64(5), "memory 5", " ").
		AddRow(uint64(4), "memory 4", nil).
		AddRow(uint64(3), "memory 3", nil).
		AddRow(uint64(2), "memory 2", nil).
		AddRow(uint64(1), "memory 1", nil)
	expectSelect(mock, "memories").WillReturnRows(rows)
	results, err = store.Search(context.Background(), middleware.MemoryQuery{UserID: "alice"})
	if err != nil {
		t.Fatalf("Search without query returned error: %v", err)
	}
	if got, want := len(results), 5; got != want {
		t.Fatalf("Search without query returned %d results, want %d", got, want)
	}
	if results[0].ID != "6" || results[4].ID != "2" {
		t.Fatalf("Search default order mismatch: %#v", results)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestSearchRowErrors(t *testing.T) {
	errBoom := errors.New("boom")

	store, mock := newReadyMockStore(t, "memories")
	rows := sqlmock.NewRows([]string{"id", "text", "metadata_json"}).
		AddRow(uint64(1), "memory", "{")
	expectSelect(mock, "memories").WillReturnRows(rows)
	if _, err := store.Search(context.Background(), middleware.MemoryQuery{UserID: "u"}); err == nil {
		t.Fatal("Search should return metadata JSON error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	store, mock = newReadyMockStore(t, "memories")
	rows = sqlmock.NewRows([]string{"id", "text", "metadata_json"}).
		AddRow("bad", "memory", nil)
	expectSelect(mock, "memories").WillReturnRows(rows)
	if _, err := store.Search(context.Background(), middleware.MemoryQuery{UserID: "u"}); err == nil {
		t.Fatal("Search should return scan error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}

	store, mock = newReadyMockStore(t, "memories")
	rows = sqlmock.NewRows([]string{"id", "text", "metadata_json"}).
		AddRow(uint64(1), "memory", nil).
		RowError(0, errBoom)
	expectSelect(mock, "memories").WillReturnRows(rows)
	if _, err := store.Search(context.Background(), middleware.MemoryQuery{UserID: "u"}); !errors.Is(err, errBoom) {
		t.Fatalf("Search rows error = %v, want %v", err, errBoom)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestStoreIntegration(t *testing.T) {
	dsn := os.Getenv("AGENTSCOPE_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set AGENTSCOPE_MYSQL_DSN to run MySQL integration test")
	}

	ctx := context.Background()
	table := fmt.Sprintf("agentscope_memory_test_%d", time.Now().UnixNano())
	store, err := Connect(Config{DSN: dsn, Table: table})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer store.Close()
	defer dropTable(ctx, t, store.db, table)

	if err := store.Add(ctx, middleware.MemoryEntry{
		UserID:  "alice",
		AgentID: "friday",
		Input:   "Ada prefers jasmine tea.",
		Metadata: map[string]any{
			"kind": "preference",
		},
	}); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if err := store.Add(ctx, middleware.MemoryEntry{
		UserID:  "bob",
		AgentID: "friday",
		Input:   "Bob prefers coffee.",
	}); err != nil {
		t.Fatalf("Add other user returned error: %v", err)
	}

	results, err := store.Search(ctx, middleware.MemoryQuery{
		UserID:  "alice",
		AgentID: "friday",
		Query:   "jasmine",
		TopK:    2,
		Metadata: map[string]any{
			"kind": "preference",
		},
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search returned %d results, want 1: %#v", len(results), results)
	}
	if !strings.Contains(results[0].Text, "jasmine tea") {
		t.Fatalf("Search text mismatch: %#v", results[0])
	}
	if results[0].Score <= 0 {
		t.Fatalf("Search score should be positive: %#v", results[0])
	}
}

func TestHelpers(t *testing.T) {
	validNames := []string{"memories", "memories_2026", "Memory"}
	for _, name := range validNames {
		if !validTableName(name) {
			t.Fatalf("validTableName(%q) = false, want true", name)
		}
	}
	invalidNames := []string{"", "bad-table"}
	for _, name := range invalidNames {
		if validTableName(name) {
			t.Fatalf("validTableName(%q) = true, want false", name)
		}
	}

	ctx := context.Background()
	if storeContext(ctx) != ctx {
		t.Fatal("storeContext should return non-nil context unchanged")
	}
	if storeContext(nil) == nil {
		t.Fatal("storeContext(nil) should return background context")
	}

	textCases := []struct {
		name  string
		entry middleware.MemoryEntry
		want  string
	}{
		{name: "input only", entry: middleware.MemoryEntry{Input: " hello "}, want: "hello"},
		{name: "output only", entry: middleware.MemoryEntry{Output: " world "}, want: "world"},
		{name: "input and output", entry: middleware.MemoryEntry{Input: "hi", Output: "there"}, want: "User: hi\nAssistant: there"},
	}
	for _, tc := range textCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := memoryEntryText(tc.entry); got != tc.want {
				t.Fatalf("memoryEntryText = %q, want %q", got, tc.want)
			}
		})
	}

	if got, want := queryTerms(" Hello, tea! "), []string{"hello", "tea"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("queryTerms = %#v, want %#v", got, want)
	}
	if got := textScore("hello tea", nil); got != 1 {
		t.Fatalf("textScore without terms = %v, want 1", got)
	}
	if got := textScore("hello jasmine tea", []string{"jasmine", "tea"}); got != 5 {
		t.Fatalf("textScore phrase = %v, want 5", got)
	}
	if got := textScore("hello", []string{"missing"}); got != 0 {
		t.Fatalf("textScore missing = %v, want 0", got)
	}

	metadata := map[string]any{"kind": "preference"}
	if !metadataMatches(metadata, map[string]any{"kind": "preference"}) {
		t.Fatal("metadataMatches should match equal metadata")
	}
	if metadataMatches(metadata, map[string]any{"missing": "preference"}) {
		t.Fatal("metadataMatches should reject missing keys")
	}
	if metadataMatches(metadata, map[string]any{"kind": "note"}) {
		t.Fatal("metadataMatches should reject unequal values")
	}
	if !metadataMatches(metadata, nil) {
		t.Fatal("metadataMatches should accept empty filters")
	}

	data, err := metadataJSON(metadata)
	if err != nil {
		t.Fatalf("metadataJSON returned error: %v", err)
	}
	if data != `{"kind":"preference"}` {
		t.Fatalf("metadataJSON = %v, want preference JSON", data)
	}
	if data, err := metadataJSON(nil); err != nil || data != nil {
		t.Fatalf("metadataJSON(nil) = %v, %v; want nil, nil", data, err)
	}
	if _, err := metadataJSON(map[string]any{"bad": func() {}}); err == nil {
		t.Fatal("metadataJSON should return marshal error")
	}

	if nullableString(" ") != nil {
		t.Fatal("nullableString should turn blank strings into nil")
	}
	if got := nullableString(" value "); got != "value" {
		t.Fatalf("nullableString = %v, want value", got)
	}
	if cloneMap(nil) != nil {
		t.Fatal("cloneMap(nil) should return nil")
	}
	cloned := cloneMap(metadata)
	cloned["kind"] = "changed"
	if metadata["kind"] != "preference" {
		t.Fatal("cloneMap should not alias the input map")
	}
}

func dropTable(ctx context.Context, t *testing.T, db *sql.DB, table string) {
	t.Helper()

	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS `"+table+"`"); err != nil {
		t.Fatalf("drop table returned error: %v", err)
	}
}

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New returned error: %v", err)
	}
	return db, mock
}

func newReadyMockStore(t *testing.T, table string) (*Store, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New returned error: %v", err)
	}
	store, err := NewStore(db, WithTable(table))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	store.schemaReady = true
	return store, mock
}

func expectSchema(mock sqlmock.Sqlmock, table string) *sqlmock.ExpectedExec {
	return mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS `" + table + "`"))
}

func expectInsert(mock sqlmock.Sqlmock, table string) *sqlmock.ExpectedExec {
	return mock.ExpectExec(regexp.QuoteMeta(
		"INSERT INTO `" + table + "` (user_id, agent_id, text, input_text, output_text, session_id, reply_id, metadata_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
	))
}

func expectSelect(mock sqlmock.Sqlmock, table string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT id, text, metadata_json FROM `" + table + "` WHERE user_id = ? AND agent_id = ? ORDER BY id DESC",
	))
}
