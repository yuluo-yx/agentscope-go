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

package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/yuluo-yx/agentscope-go/types"
)

const defaultCacheDir = "./.cache/embeddings"

// FileCacheOption configures a file cache.
type FileCacheOption func(*FileCache)

// WithMaxFileNumber sets the maximum number of cache files.
func WithMaxFileNumber(max int) FileCacheOption {
	return func(cache *FileCache) {
		cache.maxFileNumber = max
	}
}

// WithMaxCacheSizeBytes sets the maximum cache directory size in bytes.
func WithMaxCacheSizeBytes(max int64) FileCacheOption {
	return func(cache *FileCache) {
		cache.maxCacheSizeBytes = max
	}
}

// FileCache stores embedding cache entries as JSON files.
type FileCache struct {
	dir               string
	maxFileNumber     int
	maxCacheSizeBytes int64
	mu                sync.Mutex
}

type cacheFilePayload struct {
	Embeddings []types.Embedding `json:"embeddings"`
}

// NewFileCache creates a file cache.
func NewFileCache(cacheDir string, opts ...FileCacheOption) (*FileCache, error) {
	if cacheDir == "" {
		cacheDir = defaultCacheDir
	}
	abs, err := filepath.Abs(cacheDir)
	if err != nil {
		return nil, err
	}
	cache := &FileCache{dir: abs}
	for _, opt := range opts {
		opt(cache)
	}
	if cache.maxFileNumber < 0 {
		return nil, fmt.Errorf("%w: max file number must be non-negative", ErrInvalidEmbeddingInput)
	}
	if cache.maxCacheSizeBytes < 0 {
		return nil, fmt.Errorf("%w: max cache size must be non-negative", ErrInvalidEmbeddingInput)
	}
	return cache, nil
}

// Dir returns the cache directory and attempts to ensure it exists.
func (c *FileCache) Dir() string {
	if c == nil {
		return ""
	}
	_ = os.MkdirAll(c.dir, 0o755)
	return c.dir
}

// Store writes embeddings to the cache.
func (c *FileCache) Store(ctx context.Context, identifier any, embeddings []types.Embedding, options StoreOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("%w: nil file cache", ErrInvalidEmbeddingInput)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureDir(); err != nil {
		return err
	}
	path, err := c.pathFor(identifier)
	if err != nil {
		return err
	}
	_, statErr := os.Stat(path)
	if statErr == nil && !options.Overwrite {
		return nil
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}

	payload := cacheFilePayload{Embeddings: cloneEmbeddings(embeddings)}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".embedding-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return c.maintainLocked()
}

// Retrieve reads embeddings from the cache.
func (c *FileCache) Retrieve(ctx context.Context, identifier any) ([]types.Embedding, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if c == nil {
		return nil, false, fmt.Errorf("%w: nil file cache", ErrInvalidEmbeddingInput)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureDir(); err != nil {
		return nil, false, err
	}
	path, err := c.pathFor(identifier)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var payload cacheFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false, err
	}
	return cloneEmbeddings(payload.Embeddings), true, nil
}

// Remove deletes a cache entry.
func (c *FileCache) Remove(ctx context.Context, identifier any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("%w: nil file cache", ErrInvalidEmbeddingInput)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	path, err := c.pathFor(identifier)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrCacheNotFound, path)
		}
		return err
	}
	return nil
}

// Clear removes JSON cache files from the cache directory.
func (c *FileCache) Clear(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("%w: nil file cache", ErrInvalidEmbeddingInput)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureDir(); err != nil {
		return err
	}
	files, err := c.cacheFilesLocked()
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// SizeBytes returns the current cache directory size.
func (c *FileCache) SizeBytes() (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("%w: nil file cache", ErrInvalidEmbeddingInput)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sizeLocked()
}

func (c *FileCache) ensureDir() error {
	return os.MkdirAll(c.dir, 0o755)
}

func (c *FileCache) pathFor(identifier any) (string, error) {
	filename, err := filenameFor(identifier)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.dir, filename), nil
}

func filenameFor(identifier any) (string, error) {
	data, err := json.Marshal(identifier)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + ".json", nil
}

type cacheFileInfo struct {
	path  string
	mtime int64
	size  int64
}

func (c *FileCache) cacheFilesLocked() ([]cacheFileInfo, error) {
	entries, err := os.ReadDir(c.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]cacheFileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		files = append(files, cacheFileInfo{
			path:  filepath.Join(c.dir, entry.Name()),
			mtime: info.ModTime().UnixNano(),
			size:  info.Size(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].mtime < files[j].mtime
	})
	return files, nil
}

func (c *FileCache) maintainLocked() error {
	files, err := c.cacheFilesLocked()
	if err != nil {
		return err
	}
	if c.maxFileNumber > 0 && len(files) > c.maxFileNumber {
		removeCount := len(files) - c.maxFileNumber
		for _, file := range files[:removeCount] {
			if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		files = files[removeCount:]
	}
	if c.maxCacheSizeBytes > 0 {
		size := int64(0)
		for _, file := range files {
			size += file.size
		}
		for len(files) > 0 && size > c.maxCacheSizeBytes {
			file := files[0]
			if err := os.Remove(file.path); err != nil && !os.IsNotExist(err) {
				return err
			}
			size -= file.size
			files = files[1:]
		}
	}
	return nil
}

func (c *FileCache) sizeLocked() (int64, error) {
	files, err := c.cacheFilesLocked()
	if err != nil {
		return 0, err
	}
	size := int64(0)
	for _, file := range files {
		size += file.size
	}
	return size, nil
}

var _ EmbeddingCache = (*FileCache)(nil)
