package embedding

import (
	"context"

	"github.com/yuluo-yx/agentscope-go/types"
)

// StoreOptions controls cache write behavior.
type StoreOptions struct {
	Overwrite bool
}

// EmbeddingCache defines the interface for embedding caches.
type EmbeddingCache interface {
	Store(context.Context, any, []types.Embedding, StoreOptions) error
	Retrieve(context.Context, any) ([]types.Embedding, bool, error)
	Remove(context.Context, any) error
	Clear(context.Context) error
}
