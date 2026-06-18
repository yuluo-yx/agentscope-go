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

package tts_test

import (
	"testing"
	"testing/fstest"

	"github.com/yuluo-yx/agentscope-go/audio/tts"
)

func TestTTSModelCardAppliesVoiceOverrides(t *testing.T) {
	t.Parallel()

	card, err := tts.ParseModelCardYAML([]byte(`
name: qwen3-tts-flash
label: Qwen3-TTS-Flash
voices: [Cherry, Serena]
parameter_overrides: {}
`), tts.CommonParameterSchema())
	if err != nil {
		t.Fatalf("ParseModelCardYAML returned error: %v", err)
	}
	if card.Type != tts.ModelCardTypeTTS || card.OutputTypes[0] != "audio/wav" || card.Realtime {
		t.Fatalf("TTS model card defaults mismatch: %#v", card)
	}
	voice := card.ParameterSchema["properties"].(map[string]any)["voice"].(map[string]any)
	if voice["default"] != "Cherry" || len(voice["enum"].([]any)) != 2 {
		t.Fatalf("voices should be injected into voice schema: %#v", voice)
	}
}

func TestTTSModelCardsValidateCloneAndLoadFromFS(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"models/b.yaml":   {Data: []byte("name: b\nlabel: B\nstatus: deprecated\nvoices: [B]\n")},
		"models/a.yaml":   {Data: []byte("name: a\nlabel: A\nstatus: active\nparameter_overrides:\n  voice:\n    title: Custom Voice\n")},
		"models/skip.txt": {Data: []byte("ignored")},
	}
	cards, err := tts.LoadModelCardsFS(files, "models", tts.CommonParameterSchema())
	if err != nil {
		t.Fatalf("LoadModelCardsFS returned error: %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "a" || cards[1].Name != "b" {
		t.Fatalf("cards should load YAML files sorted by name: %#v", cards)
	}
	if err := cards[0].Validate(); err != nil {
		t.Fatalf("valid card should pass validation: %v", err)
	}

	clone := cards[0].Clone()
	clone.InputTypes[0] = "mutated"
	clone.ParameterSchema["properties"].(map[string]any)["voice"].(map[string]any)["title"] = "Mutated"
	if cards[0].InputTypes[0] == "mutated" || cards[0].ParameterSchema["properties"].(map[string]any)["voice"].(map[string]any)["title"] == "Mutated" {
		t.Fatalf("Clone should deep-copy model card fields: original=%#v clone=%#v", cards[0], clone)
	}

	invalids := []tts.ModelCard{
		{Label: "missing name", Status: tts.ModelStatusActive},
		{Name: "missing-label", Status: tts.ModelStatusActive},
		{Name: "bad-status", Label: "Bad", Status: tts.ModelStatus("preview")},
	}
	for _, card := range invalids {
		if err := card.Validate(); err == nil {
			t.Fatalf("Validate should reject invalid card: %#v", card)
		}
	}
	if _, err := tts.LoadModelCardsFS(fstest.MapFS{"models/bad.yaml": {Data: []byte("name: [")}}, "models", nil); err == nil {
		t.Fatal("LoadModelCardsFS should wrap YAML parse errors")
	}
}
