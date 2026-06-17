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

package embedding_test

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestFileCacheStoreRetrieveOverwriteRemoveAndClear(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, newErr := asembedding.NewFileCache(t.TempDir())
	if newErr != nil {
		t.Fatalf("NewFileCache returned error: %v", newErr)
	}

	identifier := map[string]any{"provider": "mock", "input": []string{"hello"}}
	if _, ok, err := cache.Retrieve(ctx, identifier); err != nil || ok {
		t.Fatalf("empty cache should miss, ok=%v err=%v", ok, err)
	}

	first := []types.Embedding{{0.1, 0.2}}
	if err := cache.Store(ctx, identifier, first, asembedding.StoreOptions{}); err != nil {
		t.Fatalf("Store returned error: %v", err)
	}
	got, ok, retrieveErr := cache.Retrieve(ctx, identifier)
	if retrieveErr != nil || !ok {
		t.Fatalf("Retrieve should hit, ok=%v err=%v", ok, retrieveErr)
	}
	if got[0][0] != 0.1 || got[0][1] != 0.2 {
		t.Fatalf("retrieved embeddings mismatch: %#v", got)
	}

	if err := cache.Store(ctx, identifier, []types.Embedding{{9, 9}}, asembedding.StoreOptions{}); err != nil {
		t.Fatalf("Store without overwrite returned error: %v", err)
	}
	got, ok, retrieveErr = cache.Retrieve(ctx, identifier)
	if retrieveErr != nil || !ok || got[0][0] != 0.1 {
		t.Fatalf("Store without overwrite should keep original, got=%#v ok=%v err=%v", got, ok, retrieveErr)
	}

	if err := cache.Store(ctx, identifier, []types.Embedding{{9, 9}}, asembedding.StoreOptions{Overwrite: true}); err != nil {
		t.Fatalf("Store with overwrite returned error: %v", err)
	}
	got, ok, retrieveErr = cache.Retrieve(ctx, identifier)
	if retrieveErr != nil || !ok || got[0][0] != 9 {
		t.Fatalf("Store with overwrite should replace value, got=%#v ok=%v err=%v", got, ok, retrieveErr)
	}

	if err := cache.Remove(ctx, identifier); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if _, ok, err := cache.Retrieve(ctx, identifier); err != nil || ok {
		t.Fatalf("removed cache should miss, ok=%v err=%v", ok, err)
	}
	if err := cache.Remove(ctx, identifier); !errors.Is(err, asembedding.ErrCacheNotFound) {
		t.Fatalf("missing remove should return ErrCacheNotFound, got %v", err)
	}

	if err := cache.Store(ctx, "a", []types.Embedding{{1}}, asembedding.StoreOptions{}); err != nil {
		t.Fatalf("Store a returned error: %v", err)
	}
	if err := cache.Store(ctx, "b", []types.Embedding{{2}}, asembedding.StoreOptions{}); err != nil {
		t.Fatalf("Store b returned error: %v", err)
	}
	if err := cache.Clear(ctx); err != nil {
		t.Fatalf("Clear returned error: %v", err)
	}
	entries, readErr := os.ReadDir(cache.Dir())
	if readErr != nil {
		t.Fatalf("ReadDir returned error: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("Clear should remove cache files, got %d entries", len(entries))
	}
}

func TestFileCacheMaintainsMaxFileNumberAndSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, newErr := asembedding.NewFileCache(
		t.TempDir(),
		asembedding.WithMaxFileNumber(2),
		asembedding.WithMaxCacheSizeBytes(512),
	)
	if newErr != nil {
		t.Fatalf("NewFileCache returned error: %v", newErr)
	}

	for _, id := range []string{"one", "two", "three"} {
		if err := cache.Store(ctx, id, []types.Embedding{{1, 2, 3}}, asembedding.StoreOptions{}); err != nil {
			t.Fatalf("Store %q returned error: %v", id, err)
		}
	}

	jsonFiles, globErr := filepath.Glob(filepath.Join(cache.Dir(), "*.json"))
	if globErr != nil {
		t.Fatalf("Glob returned error: %v", globErr)
	}
	if len(jsonFiles) > 2 {
		t.Fatalf("max file number should retain at most 2 files, got %d", len(jsonFiles))
	}

	largeEmbedding := make(types.Embedding, 256)
	for i := range largeEmbedding {
		largeEmbedding[i] = float64(i)
	}
	if err := cache.Store(ctx, "large", []types.Embedding{largeEmbedding}, asembedding.StoreOptions{}); err != nil {
		t.Fatalf("Store large returned error: %v", err)
	}
	size, sizeErr := cache.SizeBytes()
	if sizeErr != nil {
		t.Fatalf("SizeBytes returned error: %v", sizeErr)
	}
	if size > 512 {
		t.Fatalf("cache size should be maintained under limit, got %d", size)
	}
}

func TestFileCacheErrorBranches(t *testing.T) {
	t.Parallel()

	if _, err := asembedding.NewFileCache(t.TempDir(), asembedding.WithMaxFileNumber(-1)); !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("negative max file number should be invalid, got %v", err)
	}
	if _, err := asembedding.NewFileCache(t.TempDir(), asembedding.WithMaxCacheSizeBytes(-1)); !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("negative max cache size should be invalid, got %v", err)
	}

	var nilCache *asembedding.FileCache
	if nilCache.Dir() != "" {
		t.Fatalf("nil Dir should return empty string")
	}
	if err := nilCache.Store(context.Background(), "id", nil, asembedding.StoreOptions{}); !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("nil Store should return invalid input, got %v", err)
	}
	if _, ok, err := nilCache.Retrieve(context.Background(), "id"); ok || !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("nil Retrieve mismatch: ok=%v err=%v", ok, err)
	}
	if err := nilCache.Remove(context.Background(), "id"); !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("nil Remove should return invalid input, got %v", err)
	}
	if err := nilCache.Clear(context.Background()); !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("nil Clear should return invalid input, got %v", err)
	}
	if _, err := nilCache.SizeBytes(); !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("nil SizeBytes should return invalid input, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cache, err := asembedding.NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache returned error: %v", err)
	}
	if err := cache.Store(ctx, "id", nil, asembedding.StoreOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Store error = %v", err)
	}
	if _, _, err := cache.Retrieve(ctx, "id"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Retrieve error = %v", err)
	}
	if err := cache.Remove(ctx, "id"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Remove error = %v", err)
	}
	if err := cache.Clear(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Clear error = %v", err)
	}

	if err := cache.Store(context.Background(), math.Inf(1), nil, asembedding.StoreOptions{}); err == nil {
		t.Fatal("non-JSON identifier should fail Store")
	}
	if _, _, err := cache.Retrieve(context.Background(), math.Inf(1)); err == nil {
		t.Fatal("non-JSON identifier should fail Retrieve")
	}
	if err := cache.Remove(context.Background(), math.Inf(1)); err == nil {
		t.Fatal("non-JSON identifier should fail Remove")
	}

	if err := cache.Clear(context.Background()); err != nil {
		t.Fatalf("Clear empty cache returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cache.Dir(), "ignored.tmp"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile non-cache fixture returned error: %v", err)
	}
	if size, err := cache.SizeBytes(); err != nil || size != 0 {
		t.Fatalf("SizeBytes should ignore non-json files, size=%d err=%v", size, err)
	}
	if err := cache.Store(context.Background(), "corrupt", []types.Embedding{{1}}, asembedding.StoreOptions{}); err != nil {
		t.Fatalf("Store corrupt seed returned error: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(cache.Dir(), "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("Glob cache files = %#v, %v", files, err)
	}
	if err := os.WriteFile(files[0], []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt cache returned error: %v", err)
	}
	if _, _, err := cache.Retrieve(context.Background(), "corrupt"); err == nil {
		t.Fatal("corrupt cache JSON should fail Retrieve")
	}
}
