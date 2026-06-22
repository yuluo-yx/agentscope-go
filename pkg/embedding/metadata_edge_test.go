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
	"testing/fstest"

	asembedding "github.com/yuluo-yx/agentscope-go/pkg/embedding"
)

func TestEmbeddingModelCardDefaultsCloneAndValidationBranches(t *testing.T) {
	t.Parallel()

	card, err := asembedding.ParseModelCardYAML([]byte(`
name: text-mini
label: Text Mini
parameter_overrides:
  dimensions:
    default: 128
  hidden_parameter:
    hidden: true
`), nil)
	if err != nil {
		t.Fatalf("ParseModelCardYAML returned error: %v", err)
	}
	if card.Type != asembedding.ModelCardTypeEmbedding || card.Status != asembedding.ModelStatusActive {
		t.Fatalf("defaults not applied: %#v", card)
	}
	if len(card.InputTypes) != 1 || len(card.OutputTypes) != 1 {
		t.Fatalf("input/output defaults not applied: %#v", card)
	}
	properties := card.ParameterSchema["properties"].(map[string]any)
	if properties["dimensions"].(map[string]any)["default"] != 128 {
		t.Fatalf("dimension override not merged: %#v", properties["dimensions"])
	}
	if _, ok := properties["hidden_parameter"]; ok {
		t.Fatalf("hidden override should not create a property: %#v", properties)
	}

	clone := card.Clone()
	clone.InputTypes[0] = "changed"
	clone.ParameterSchema["type"] = "changed"
	clone.ParameterOverrides["dimensions"]["default"] = 256
	if card.InputTypes[0] == "changed" || card.ParameterSchema["type"] == "changed" || card.ParameterOverrides["dimensions"]["default"] == 256 {
		t.Fatalf("Clone should deep-copy slices and maps")
	}

	cases := []asembedding.ModelCard{
		{},
		{Name: "missing-label", Status: asembedding.ModelStatusActive},
		{Name: "negative-context", Label: "Negative", Status: asembedding.ModelStatusActive, ContextSize: -1},
		{Name: "bad-status", Label: "Bad", Status: "preview"},
	}
	for _, tt := range cases {
		if err := tt.Validate(); err == nil {
			t.Fatalf("Validate should reject %#v", tt)
		}
	}
}

func TestEmbeddingLoadModelCardsFSErrorsAndSorting(t *testing.T) {
	t.Parallel()

	cards, err := asembedding.LoadModelCardsFS(fstest.MapFS{
		"models/b.yaml": {Data: []byte("name: b\nlabel: B\n")},
		"models/a.yml":  {Data: []byte("name: a\nlabel: A\n")},
		"models/readme": {Data: []byte("ignored")},
	}, "models", asembedding.CommonParameterSchema())
	if err != nil {
		t.Fatalf("LoadModelCardsFS returned error: %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "a" || cards[1].Name != "b" {
		t.Fatalf("cards should be sorted and ignore non-yaml files: %#v", cards)
	}

	if _, err := asembedding.LoadModelCardsFS(fstest.MapFS{}, "missing", nil); err == nil {
		t.Fatal("missing directory should return an error")
	}
	_, err = asembedding.LoadModelCardsFS(fstest.MapFS{
		"models/bad.yaml": {Data: []byte("name: bad\nstatus: unsupported\n")},
	}, "models", nil)
	if err == nil || !strings.Contains(err.Error(), "bad.yaml") {
		t.Fatalf("invalid card error should include filename, got %v", err)
	}
	if _, err := asembedding.ParseModelCardYAML([]byte(":"), nil); err == nil {
		t.Fatal("invalid YAML should return an error")
	}
}
