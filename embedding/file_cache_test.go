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
