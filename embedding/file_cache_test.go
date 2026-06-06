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
	cache, err := asembedding.NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache returned error: %v", err)
	}

	identifier := map[string]any{"provider": "mock", "input": []string{"hello"}}
	if _, ok, err := cache.Retrieve(ctx, identifier); err != nil || ok {
		t.Fatalf("empty cache should miss, ok=%v err=%v", ok, err)
	}

	first := []types.Embedding{{0.1, 0.2}}
	if err := cache.Store(ctx, identifier, first, asembedding.StoreOptions{}); err != nil {
		t.Fatalf("Store returned error: %v", err)
	}
	got, ok, err := cache.Retrieve(ctx, identifier)
	if err != nil || !ok {
		t.Fatalf("Retrieve should hit, ok=%v err=%v", ok, err)
	}
	if got[0][0] != 0.1 || got[0][1] != 0.2 {
		t.Fatalf("retrieved embeddings mismatch: %#v", got)
	}

	if err := cache.Store(ctx, identifier, []types.Embedding{{9, 9}}, asembedding.StoreOptions{}); err != nil {
		t.Fatalf("Store without overwrite returned error: %v", err)
	}
	got, ok, err = cache.Retrieve(ctx, identifier)
	if err != nil || !ok || got[0][0] != 0.1 {
		t.Fatalf("Store without overwrite should keep original, got=%#v ok=%v err=%v", got, ok, err)
	}

	if err := cache.Store(ctx, identifier, []types.Embedding{{9, 9}}, asembedding.StoreOptions{Overwrite: true}); err != nil {
		t.Fatalf("Store with overwrite returned error: %v", err)
	}
	got, ok, err = cache.Retrieve(ctx, identifier)
	if err != nil || !ok || got[0][0] != 9 {
		t.Fatalf("Store with overwrite should replace value, got=%#v ok=%v err=%v", got, ok, err)
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
	entries, err := os.ReadDir(cache.Dir())
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Clear should remove cache files, got %d entries", len(entries))
	}
}

func TestFileCacheMaintainsMaxFileNumberAndSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, err := asembedding.NewFileCache(
		t.TempDir(),
		asembedding.WithMaxFileNumber(2),
		asembedding.WithMaxCacheSizeBytes(512),
	)
	if err != nil {
		t.Fatalf("NewFileCache returned error: %v", err)
	}

	for _, id := range []string{"one", "two", "three"} {
		if err := cache.Store(ctx, id, []types.Embedding{{1, 2, 3}}, asembedding.StoreOptions{}); err != nil {
			t.Fatalf("Store %q returned error: %v", id, err)
		}
	}

	jsonFiles, err := filepath.Glob(filepath.Join(cache.Dir(), "*.json"))
	if err != nil {
		t.Fatalf("Glob returned error: %v", err)
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
	size, err := cache.SizeBytes()
	if err != nil {
		t.Fatalf("SizeBytes returned error: %v", err)
	}
	if size > 512 {
		t.Fatalf("cache size should be maintained under limit, got %d", size)
	}
}
