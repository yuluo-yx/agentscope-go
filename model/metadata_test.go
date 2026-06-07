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
	"testing"

	modelpkg "github.com/yuluo-yx/agentscope-go/model"
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

func TestStructuredOutputInterfaceIsOptional(t *testing.T) {
	t.Parallel()

	var model modelpkg.ChatModel = fakeChatModel{response: modelpkg.NewChatResponse(nil, true)}
	if _, ok := model.(modelpkg.StructuredOutputModel); ok {
		t.Fatal("fake ChatModel should not implement optional structured output by default")
	}
}
