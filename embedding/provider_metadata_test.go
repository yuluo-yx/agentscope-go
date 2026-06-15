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
	"strings"
	"testing"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/embedding/dashscope"
	"github.com/yuluo-yx/agentscope-go/embedding/gemini"
	"github.com/yuluo-yx/agentscope-go/embedding/openai"
)

func TestPythonEmbeddingModelCardsAreLoadedByProviders(t *testing.T) {
	t.Parallel()

	providers := []struct {
		name string
		list func() ([]asembedding.ModelCard, error)
		want string
	}{
		{name: "dashscope", list: dashscope.ListModels, want: "qwen3-vl-embedding"},
		{name: "gemini", list: gemini.ListModels, want: "gemini-embedding-2"},
		{name: "openai", list: openai.ListModels, want: "text-embedding-3-large"},
	}
	for _, provider := range providers {
		t.Run(provider.name, func(t *testing.T) {
			t.Parallel()

			cards, err := provider.list()
			if err != nil {
				t.Fatalf("ListModels returned error: %v", err)
			}
			if len(cards) == 0 {
				t.Fatal("ListModels should load at least one embedding card")
			}
			found := false
			for _, card := range cards {
				assertCompleteEmbeddingCard(t, provider.name, card)
				if card.Name == provider.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected Python embedding model card %q in %#v", provider.want, cards)
			}
		})
	}
}

func TestEmbeddingModelCardParameterOverridesMatchPythonSemantics(t *testing.T) {
	t.Parallel()

	cards, err := dashscope.ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	textV4 := findEmbeddingCard(cards, "text-embedding-v4")
	properties := textV4.ParameterSchema["properties"].(map[string]any)
	dimensions := properties["dimensions"].(map[string]any)
	if dimensions["default"] != 1024 || len(dimensions["enum"].([]any)) != 8 {
		t.Fatalf("dimension override should be merged into schema: %#v", dimensions)
	}

	legacy := findEmbeddingCard(cards, "multimodal-embedding-v1")
	properties = legacy.ParameterSchema["properties"].(map[string]any)
	if _, ok := properties["dimensions"]; ok {
		t.Fatalf("hidden dimension override should remove property: %#v", legacy.ParameterSchema)
	}
}

func assertCompleteEmbeddingCard(t *testing.T, provider string, card asembedding.ModelCard) {
	t.Helper()
	if card.Type != asembedding.ModelCardTypeEmbedding || card.Name == "" || card.Label == "" {
		t.Fatalf("%s embedding card missing required fields: %#v", provider, card)
	}
	if len(card.InputTypes) == 0 || len(card.OutputTypes) == 0 {
		t.Fatalf("%s embedding card missing input/output types: %#v", provider, card)
	}
	for _, inputType := range card.InputTypes {
		if !strings.Contains(inputType, "/") {
			t.Fatalf("%s embedding card should use MIME-style input types, got %q in %#v", provider, inputType, card)
		}
	}
	if len(card.OutputTypes) != 1 || card.OutputTypes[0] != "application/x-embedding" {
		t.Fatalf("%s embedding card should output embeddings: %#v", provider, card)
	}
	if card.ParameterSchema["type"] != "object" {
		t.Fatalf("%s embedding card %s missing object parameter schema: %#v", provider, card.Name, card.ParameterSchema)
	}
}

func findEmbeddingCard(cards []asembedding.ModelCard, name string) asembedding.ModelCard {
	for _, card := range cards {
		if card.Name == name {
			return card
		}
	}
	return asembedding.ModelCard{}
}
