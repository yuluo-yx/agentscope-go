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

package redis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/yuluo-yx/agentscope-go/pkg/middleware"
)

func TestStoreImplementsMemoryStore(t *testing.T) {
	var _ middleware.MemoryStore = (*Store)(nil)
}

func TestConnect(t *testing.T) {
	store, err := Connect(Config{Addr: " "})
	if err == nil {
		if closeErr := store.Close(); closeErr != nil {
			t.Fatalf("Close returned error: %v", closeErr)
		}
	}

	if _, err := Connect(Config{Addr: "127.0.0.1:0"}); err == nil {
		t.Fatal("Connect should fail when Redis is unreachable")
	}

	server := miniredis.RunT(t)
	store, err = Connect(Config{Addr: server.Addr(), KeyPrefix: "custom:"})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if store.keyPrefix != "custom" {
		t.Fatalf("key prefix = %q, want custom", store.keyPrefix)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestNewStoreAndClose(t *testing.T) {
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	store, err := NewStore(client, nil, WithKeyPrefix(" custom::"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	if store.keyPrefix != "custom" {
		t.Fatalf("key prefix = %q, want custom", store.keyPrefix)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if _, err := NewStore(nil); err == nil {
		t.Fatal("NewStore should reject a nil client")
	}

	errBoom := errors.New("boom")
	client = goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	defer client.Close()
	if _, err := NewStore(client, func(*Store) error { return errBoom }); !errors.Is(err, errBoom) {
		t.Fatalf("NewStore error = %v, want %v", err, errBoom)
	}

	var nilStore *Store
	if err := nilStore.Close(); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if err := (&Store{}).Close(); err != nil {
		t.Fatalf("empty Close returned error: %v", err)
	}
}

func TestAddValidationAndErrors(t *testing.T) {
	store := newTestStore(t)

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
		t.Fatal("Add should reject a store without client")
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

	badStore := newTestStore(t)
	if err := badStore.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := badStore.Add(context.Background(), middleware.MemoryEntry{UserID: "u", Input: "memory"}); err == nil {
		t.Fatal("Add should return Redis write error")
	}
}

func TestAddAndSearch(t *testing.T) {
	store := newTestStore(t)
	metadata := map[string]any{"kind": "preference"}

	entries := []middleware.MemoryEntry{
		{UserID: "alice", AgentID: "friday", Input: "Ada prefers jasmine tea.", Metadata: metadata},
		{UserID: "alice", AgentID: "friday", Input: "Ada likes jasmine tea every morning.", Metadata: metadata},
		{UserID: "alice", AgentID: "friday", Input: "Ada mentions jasmine only.", Metadata: metadata},
		{UserID: "alice", AgentID: "saturday", Input: "Ada prefers coffee."},
		{UserID: "bob", AgentID: "friday", Input: "Bob prefers jasmine tea."},
		{UserID: "alice", AgentID: "friday", Output: "Ada likes green tea."},
		{UserID: "alice", AgentID: "friday", Input: "What tea?", Output: "Jasmine tea."},
	}
	for _, entry := range entries {
		if err := store.Add(nil, entry); err != nil {
			t.Fatalf("Add returned error: %v", err)
		}
	}
	metadata["kind"] = "changed"

	results, err := store.Search(nil, middleware.MemoryQuery{
		UserID:   "alice",
		AgentID:  "friday",
		Query:    "jasmine tea!",
		TopK:     2,
		Metadata: map[string]any{"kind": "preference"},
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got, want := len(results), 2; got != want {
		t.Fatalf("Search returned %d results, want %d: %#v", got, want, results)
	}
	if results[0].ID != "2" || results[1].ID != "1" {
		t.Fatalf("Search order mismatch: %#v", results)
	}
	if results[0].Metadata["kind"] != "preference" {
		t.Fatalf("Search metadata mismatch: %#v", results[0].Metadata)
	}
	results[0].Metadata["kind"] = "mutated"

	results, err = store.Search(context.Background(), middleware.MemoryQuery{
		UserID:  "alice",
		AgentID: "friday",
		TopK:    0,
	})
	if err != nil {
		t.Fatalf("Search without query returned error: %v", err)
	}
	if got, want := len(results), 5; got != want {
		t.Fatalf("Search without query returned %d results, want %d: %#v", got, want, results)
	}

	results, err = store.Search(context.Background(), middleware.MemoryQuery{
		UserID:   "alice",
		AgentID:  "friday",
		Query:    "banana",
		Metadata: map[string]any{"kind": "preference"},
	})
	if err != nil {
		t.Fatalf("Search no-match query returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search no-match query returned %#v, want empty", results)
	}

	results, err = store.Search(context.Background(), middleware.MemoryQuery{
		UserID:   "alice",
		AgentID:  "friday",
		Metadata: map[string]any{"kind": "missing"},
	})
	if err != nil {
		t.Fatalf("Search metadata mismatch returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search metadata mismatch returned %#v, want empty", results)
	}

	if err := store.client.ZAdd(context.Background(), store.scopeKey("alice", "friday"), goredis.Z{
		Score:  1,
		Member: "missing",
	}).Err(); err != nil {
		t.Fatalf("ZAdd missing returned error: %v", err)
	}
	results, err = store.Search(context.Background(), middleware.MemoryQuery{UserID: "alice", AgentID: "friday"})
	if err != nil {
		t.Fatalf("Search with missing record returned error: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("Search with missing record returned %d results, want 5", len(results))
	}
}

func TestSearchValidationAndErrors(t *testing.T) {
	store := newTestStore(t)

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
		t.Fatal("Search should reject a store without client")
	}
	if _, err := store.Search(context.Background(), middleware.MemoryQuery{}); err == nil {
		t.Fatal("Search should require user id")
	}

	badStore := newTestStore(t)
	if err := badStore.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if _, err := badStore.Search(context.Background(), middleware.MemoryQuery{UserID: "u"}); err == nil {
		t.Fatal("Search should return Redis query error")
	}
	if _, _, err := badStore.getRecord(context.Background(), "1"); err == nil {
		t.Fatal("getRecord should return Redis read error")
	}

	if _, ok, err := store.getRecord(context.Background(), "missing"); err != nil || ok {
		t.Fatalf("getRecord missing = ok %v err %v, want false nil", ok, err)
	}
	if err := store.client.Set(context.Background(), store.recordKey("bad"), "{", 0).Err(); err != nil {
		t.Fatalf("Set bad record returned error: %v", err)
	}
	if _, _, err := store.getRecord(context.Background(), "bad"); err == nil {
		t.Fatal("getRecord should return JSON error")
	}

	if err := store.client.LPush(context.Background(), store.recordKey("wrongtype"), "memory").Err(); err != nil {
		t.Fatalf("LPush wrongtype returned error: %v", err)
	}
	if err := store.client.ZAdd(context.Background(), store.scopeKey("u", ""), goredis.Z{
		Score:  1,
		Member: "wrongtype",
	}).Err(); err != nil {
		t.Fatalf("ZAdd wrongtype returned error: %v", err)
	}
	if _, err := store.Search(context.Background(), middleware.MemoryQuery{UserID: "u"}); err == nil {
		t.Fatal("Search should return getRecord error")
	}
}

func TestStoreIntegration(t *testing.T) {
	addr := os.Getenv("AGENTSCOPE_REDIS_ADDR")
	if addr == "" {
		t.Skip("set AGENTSCOPE_REDIS_ADDR to run Redis integration test")
	}

	ctx := context.Background()
	prefix := fmt.Sprintf("agentscope:test:memory:%d", time.Now().UnixNano())
	db, _ := strconv.Atoi(os.Getenv("AGENTSCOPE_REDIS_DB"))
	store, err := Connect(Config{
		Addr:      addr,
		Username:  os.Getenv("AGENTSCOPE_REDIS_USERNAME"),
		Password:  os.Getenv("AGENTSCOPE_REDIS_PASSWORD"),
		DB:        db,
		KeyPrefix: prefix,
	})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer store.Close()
	defer cleanPrefix(ctx, t, store, prefix)

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
	if got := normalizeKeyPrefix(" "); got != defaultKeyPrefix {
		t.Fatalf("normalizeKeyPrefix empty = %q, want %q", got, defaultKeyPrefix)
	}
	if got := normalizeKeyPrefix(" custom::"); got != "custom" {
		t.Fatalf("normalizeKeyPrefix custom = %q, want custom", got)
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

	if cloneMap(nil) != nil {
		t.Fatal("cloneMap(nil) should return nil")
	}
	cloned := cloneMap(metadata)
	cloned["kind"] = "changed"
	if metadata["kind"] != "preference" {
		t.Fatal("cloneMap should not alias the input map")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	store, err := NewStore(client, WithKeyPrefix(fmt.Sprintf("agentscope:test:%d", time.Now().UnixNano())))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	n := 0
	store.now = func() time.Time {
		current := base.Add(time.Duration(n) * time.Second)
		n++
		return current
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func cleanPrefix(ctx context.Context, t *testing.T, store *Store, prefix string) {
	t.Helper()

	keys, err := store.client.Keys(ctx, prefix+"*").Result()
	if err != nil {
		t.Fatalf("cleanup keys returned error: %v", err)
	}
	if len(keys) > 0 {
		if err := store.client.Del(ctx, keys...).Err(); err != nil {
			t.Fatalf("cleanup del returned error: %v", err)
		}
	}
}
