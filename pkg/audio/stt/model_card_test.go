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

package stt_test

import (
	"testing"
	"testing/fstest"

	"github.com/yuluo-yx/agentscope-go/pkg/audio/stt"
)

func TestSTTModelCardAppliesDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	card, err := stt.ParseModelCardYAML([]byte(`
name: paraformer-v2
label: Paraformer V2
parameter_overrides:
  language:
    default: zh
  sample_rate:
    maximum: 48000
`), stt.CommonParameterSchema())
	if err != nil {
		t.Fatalf("ParseModelCardYAML returned error: %v", err)
	}
	if card.Type != stt.ModelCardTypeSTT || card.InputTypes[0] != "audio/wav" || card.OutputTypes[0] != "text/plain" || card.Realtime {
		t.Fatalf("STT model card defaults mismatch: %#v", card)
	}
	properties := card.ParameterSchema["properties"].(map[string]any)
	if properties["language"].(map[string]any)["default"] != "zh" {
		t.Fatalf("language override not merged: %#v", properties["language"])
	}
	if properties["sample_rate"].(map[string]any)["maximum"] != 48000 {
		t.Fatalf("sample_rate override not merged: %#v", properties["sample_rate"])
	}
}

func TestSTTModelCardsValidateCloneAndLoadFromFS(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"models/b.yaml":   {Data: []byte("name: b\nlabel: B\nstatus: deprecated\n")},
		"models/a.yaml":   {Data: []byte("name: a\nlabel: A\nstatus: active\nparameter_overrides:\n  language:\n    hidden: true\n")},
		"models/skip.txt": {Data: []byte("ignored")},
	}
	cards, err := stt.LoadModelCardsFS(files, "models", stt.CommonParameterSchema())
	if err != nil {
		t.Fatalf("LoadModelCardsFS returned error: %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "a" || cards[1].Name != "b" {
		t.Fatalf("cards should load YAML files sorted by name: %#v", cards)
	}
	if _, exists := cards[0].ParameterSchema["properties"].(map[string]any)["language"]; exists {
		t.Fatalf("hidden override should remove language property: %#v", cards[0].ParameterSchema)
	}
	if err := cards[0].Validate(); err != nil {
		t.Fatalf("valid card should pass validation: %v", err)
	}

	clone := cards[0].Clone()
	clone.InputTypes[0] = "mutated"
	clone.ParameterSchema["properties"].(map[string]any)["sample_rate"].(map[string]any)["title"] = "Mutated"
	if cards[0].InputTypes[0] == "mutated" ||
		cards[0].ParameterSchema["properties"].(map[string]any)["sample_rate"].(map[string]any)["title"] == "Mutated" {
		t.Fatalf("Clone should deep-copy model card fields: original=%#v clone=%#v", cards[0], clone)
	}

	invalids := []stt.ModelCard{
		{Label: "missing name", Status: stt.ModelStatusActive},
		{Name: "missing-label", Status: stt.ModelStatusActive},
		{Name: "bad-status", Label: "Bad", Status: stt.ModelStatus("preview")},
	}
	for _, card := range invalids {
		if err := card.Validate(); err == nil {
			t.Fatalf("Validate should reject invalid card: %#v", card)
		}
	}
	if _, err := stt.LoadModelCardsFS(fstest.MapFS{"models/bad.yaml": {Data: []byte("name: [")}}, "models", nil); err == nil {
		t.Fatal("LoadModelCardsFS should wrap YAML parse errors")
	}
}
