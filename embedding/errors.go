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
