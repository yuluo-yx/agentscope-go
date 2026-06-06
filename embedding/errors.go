package embedding

import "errors"

var (
	// ErrUnsupportedModality indicates that the provider does not support a requested modality.
	ErrUnsupportedModality = errors.New("embedding: unsupported modality")
	// ErrInvalidEmbeddingInput indicates that an embedding input is malformed or invalid.
	ErrInvalidEmbeddingInput = errors.New("embedding: invalid input")
	// ErrInvalidEmbeddingDimension indicates that the configured embedding dimension is invalid.
	ErrInvalidEmbeddingDimension = errors.New("embedding: invalid dimension")
	// ErrCacheNotFound indicates that a cache entry does not exist.
	ErrCacheNotFound = errors.New("embedding: cache entry not found")
)
