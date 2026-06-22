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

package model_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
)

func TestModelCardYAMLLoadingAndCapabilityMatrix(t *testing.T) {
	t.Parallel()

	raw := []byte(`
name: gpt-4o-mini
label: GPT-4o mini
status: active
input_types:
  - text
  - image
  - audio
output_types:
  - text
  - audio
context_size: 128000
output_size: 16384
capabilities:
  text: true
  tools: true
  image: true
  audio: true
  video: false
  structured_output: true
  embedding: false
  generation: true
parameter_schema:
  type: object
  properties:
    temperature:
      type: number
      minimum: 0
      maximum: 2
parameter_overrides:
  max_tokens:
    maximum: 16384
extra:
  api: chat_completions
`)

	card, err := modelpkg.ParseModelCardYAML(raw)
	if err != nil {
		t.Fatalf("ParseModelCardYAML returned error: %v", err)
	}
	if card.Type != modelpkg.ModelCardTypeChat || card.Name != "gpt-4o-mini" || card.Label != "GPT-4o mini" {
		t.Fatalf("model card identity mismatch: %#v", card)
	}
	if card.ContextSize != 128000 || card.OutputSize != 16384 {
		t.Fatalf("model sizes not loaded: %#v", card)
	}
	if !card.Supports(modelpkg.ModelCapabilityTools) || !card.Supports(modelpkg.ModelCapabilityStructuredOutput) {
		t.Fatalf("capability matrix should include tools and structured output: %#v", card.Capabilities)
	}
	if card.Supports(modelpkg.ModelCapabilityVideo) {
		t.Fatalf("video should be distinguishable and disabled: %#v", card.Capabilities)
	}
	if card.ParameterSchema["type"] != "object" || card.ParameterOverrides["max_tokens"]["maximum"] != 16384 {
		t.Fatalf("parameter schema or overrides not preserved: %#v overrides=%#v", card.ParameterSchema, card.ParameterOverrides)
	}
	clone := card.Clone()
	clone.Capabilities[modelpkg.ModelCapabilityVideo] = true
	if card.Supports(modelpkg.ModelCapabilityVideo) {
		t.Fatalf("Clone should deep-copy capability map")
	}
}

func TestCapabilityRejectionUsesTypedError(t *testing.T) {
	t.Parallel()

	card := modelpkg.ModelCard{Name: "text-only", Capabilities: modelpkg.ModelCapabilities{modelpkg.ModelCapabilityText: true}}
	err := card.Require(modelpkg.ModelCapabilityVideo)
	if err == nil {
		t.Fatal("missing capability should return an error")
	}
	var capabilityErr *modelpkg.CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != modelpkg.ModelCapabilityVideo || capabilityErr.Model != "text-only" {
		t.Fatalf("capability error metadata mismatch: %#v err=%v", capabilityErr, err)
	}
}

func TestApplyModelCardDefaultsMergesPythonParameterOverrides(t *testing.T) {
	t.Parallel()

	raw := []byte(`
name: audio-model
label: Audio Model
status: active
input_types:
  - text/plain
output_types:
  - text/plain
  - audio/wav
context_size: 128000
output_size: 42
parameter_overrides:
  max_tokens:
    maximum: 42
  thinking_enable:
    hidden: true
  voice:
    default: alloy
    enum:
      - alloy
      - nova
`)

	card, err := modelpkg.ParseModelCardYAML(raw)
	if err != nil {
		t.Fatalf("ParseModelCardYAML returned error: %v", err)
	}
	modelpkg.ApplyModelCardDefaults(&card, modelpkg.NewModelCardDefaults("openai", nil, nil))

	properties := schemaProperties(t, card.ParameterSchema)
	maxTokens := properties["max_tokens"].(map[string]any)
	if fmt.Sprint(maxTokens["maximum"]) != "42" {
		t.Fatalf("max_tokens override should be merged into schema: %#v", maxTokens)
	}
	if _, exists := properties["thinking_enable"]; exists {
		t.Fatalf("hidden parameter should be removed from schema: %#v", properties["thinking_enable"])
	}
	voice := properties["voice"].(map[string]any)
	if voice["default"] != "alloy" {
		t.Fatalf("voice default not merged: %#v", voice)
	}
	enumValues := voice["enum"].([]any)
	if len(enumValues) != 2 || enumValues[0] != "alloy" || enumValues[1] != "nova" {
		t.Fatalf("voice enum not merged: %#v", enumValues)
	}
}

func TestApplyModelCardDefaultsHidesVoiceForNonAudioOutput(t *testing.T) {
	t.Parallel()

	raw := []byte(`
name: text-model
label: Text Model
status: active
input_types:
  - text/plain
output_types:
  - text/plain
context_size: 128000
output_size: 42
parameter_overrides:
  max_tokens:
    maximum: 42
  voice:
    default: alloy
`)

	card, err := modelpkg.ParseModelCardYAML(raw)
	if err != nil {
		t.Fatalf("ParseModelCardYAML returned error: %v", err)
	}
	modelpkg.ApplyModelCardDefaults(&card, modelpkg.NewModelCardDefaults("openai", nil, nil))

	properties := schemaProperties(t, card.ParameterSchema)
	if _, exists := properties["voice"]; exists {
		t.Fatalf("non-audio output cards should hide voice: %#v", properties["voice"])
	}
	if fmt.Sprint(properties["max_tokens"].(map[string]any)["maximum"]) != "42" {
		t.Fatalf("max_tokens maximum should still be applied: %#v", properties["max_tokens"])
	}
}

func TestStructuredOutputInterfaceIsOptional(t *testing.T) {
	t.Parallel()

	var model modelpkg.ChatModel = fakeChatModel{response: modelpkg.NewChatResponse(nil, true)}
	if _, ok := model.(modelpkg.StructuredOutputModel); ok {
		t.Fatal("fake ChatModel should not implement optional structured output by default")
	}
}

func TestModelCardDefaultsLoadAndValidationBranches(t *testing.T) {
	t.Parallel()

	if (*modelpkg.CapabilityError)(nil).Error() != "<nil>" {
		t.Fatalf("nil CapabilityError should format as <nil>")
	}
	if got := (&modelpkg.CapabilityError{Capability: modelpkg.ModelCapabilityAudio}).Error(); !strings.Contains(got, "<unknown>") {
		t.Fatalf("empty model CapabilityError should use unknown model, got %q", got)
	}
	if modelpkg.ModelCapabilities(nil).Clone() != nil {
		t.Fatalf("nil capabilities clone should stay nil")
	}
	modelpkg.ApplyModelCardDefaults(nil, modelpkg.ModelCardDefaults{})

	cards, err := modelpkg.LoadModelCardsFSWithDefaults(fstest.MapFS{
		"models/b.yaml": {Data: []byte("name: b\nlabel: B\ncontext_size: 8\noutput_size: 2\noutput_types:\n  - audio/wav\n")},
		"models/a.yml":  {Data: []byte("name: a\nlabel: A\ncontext_size: 8\noutput_size: 2\n")},
		"models/readme": {Data: []byte("ignored")},
	}, "models", modelpkg.NewModelCardDefaults(
		"unit",
		modelpkg.ModelCapabilities{modelpkg.ModelCapabilityTools: true},
		map[string]any{"owner": "tests"},
	))
	if err != nil {
		t.Fatalf("LoadModelCardsFSWithDefaults returned error: %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "a" || cards[1].Name != "b" {
		t.Fatalf("cards should be sorted and ignore non-yaml files: %#v", cards)
	}
	audio := cards[1]
	if !audio.Supports(modelpkg.ModelCapabilityAudio) || !audio.Supports(modelpkg.ModelCapabilityTools) {
		t.Fatalf("defaults should merge inferred and provider capabilities: %#v", audio.Capabilities)
	}
	if audio.Extra["provider"] != "unit" || audio.Extra["owner"] != "tests" {
		t.Fatalf("defaults should merge extra metadata: %#v", audio.Extra)
	}
	if err := audio.Require(modelpkg.ModelCapabilityAudio); err != nil {
		t.Fatalf("Require supported capability returned error: %v", err)
	}
	audioClone := audio.Clone()
	audioClone.OutputTypes[0] = "changed"
	audioClone.Extra["owner"] = "changed"
	if audio.OutputTypes[0] == "changed" || audio.Extra["owner"] == "changed" {
		t.Fatalf("Clone should deep-copy slices and maps")
	}

	if _, err := modelpkg.LoadModelCardsFS(fstest.MapFS{}, "missing"); err == nil {
		t.Fatal("missing directory should return an error")
	}
	_, err = modelpkg.LoadModelCardsFS(fstest.MapFS{
		"models/bad.yaml": {Data: []byte("name: bad\nlabel: Bad\ncontext_size: 0\noutput_size: 1\n")},
	}, "models")
	if err == nil || !strings.Contains(err.Error(), "bad.yaml") {
		t.Fatalf("invalid card error should include filename, got %v", err)
	}
	if _, err := modelpkg.ParseModelCardYAML([]byte(":")); err == nil {
		t.Fatal("invalid YAML should return an error")
	}

	cases := []modelpkg.ModelCard{
		{},
		{Name: "missing-label", Status: modelpkg.ModelStatusActive, ContextSize: 1, OutputSize: 1},
		{Name: "bad-context", Label: "Bad", Status: modelpkg.ModelStatusActive, OutputSize: 1},
		{Name: "bad-output", Label: "Bad", Status: modelpkg.ModelStatusActive, ContextSize: 1},
		{Name: "bad-status", Label: "Bad", Status: "preview", ContextSize: 1, OutputSize: 1},
	}
	for _, tt := range cases {
		if err := tt.Validate(); err == nil {
			t.Fatalf("Validate should reject %#v", tt)
		}
	}
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing or wrong type: %#v", schema)
	}
	return properties
}
