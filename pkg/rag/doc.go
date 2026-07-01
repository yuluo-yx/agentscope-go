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

// Package rag provides retrieval-augmented generation building blocks.
//
// The package owns core data structures, parser and chunker contracts,
// KnowledgeBase orchestration, and the VectorStore interface. Concrete vector
// database clients are intentionally left to extension modules so applications
// only import the backends they use.
package rag
