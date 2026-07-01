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

package rag

import "errors"

var (
	// ErrInvalidInput indicates that a RAG request or configuration is invalid.
	ErrInvalidInput = errors.New("rag: invalid input")
	// ErrUnsupportedContent indicates that a content block cannot be embedded or chunked.
	ErrUnsupportedContent = errors.New("rag: unsupported content")
	// ErrEmbeddingMismatch indicates that an embedding model returned a vector count different from the input count.
	ErrEmbeddingMismatch = errors.New("rag: embedding count mismatch")
)
