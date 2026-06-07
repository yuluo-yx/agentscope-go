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
	"os"
	"strings"
	"testing"

	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/anthropic"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/model/deepseek"
	"github.com/yuluo-yx/agentscope-go/model/gemini"
	"github.com/yuluo-yx/agentscope-go/model/moonshot"
	"github.com/yuluo-yx/agentscope-go/model/ollama"
	"github.com/yuluo-yx/agentscope-go/model/openai"
	"github.com/yuluo-yx/agentscope-go/model/openairesponse"
	"github.com/yuluo-yx/agentscope-go/model/xai"
	"github.com/yuluo-yx/agentscope-go/model/zhipu"
)

func TestEveryProviderExposesCompleteModelMetadata(t *testing.T) {
	t.Parallel()

	providers := []struct {
		name string
		list func() ([]asmodel.ModelCard, error)
	}{
		{name: "anthropic", list: anthropic.ListModels},
		{name: "dashscope", list: dashscope.ListModels},
		{name: "deepseek", list: deepseek.ListModels},
		{name: "gemini", list: gemini.ListModels},
		{name: "moonshot", list: moonshot.ListModels},
		{name: "ollama", list: ollama.ListModels},
		{name: "openai", list: openai.ListModels},
		{name: "openai_response", list: openairesponse.ListModels},
		{name: "xai", list: xai.ListModels},
		{name: "zhipu", list: zhipu.ListModels},
	}
	for _, provider := range providers {
		t.Run(provider.name, func(t *testing.T) {
			t.Parallel()

			cards, err := provider.list()
			if err != nil {
				t.Fatalf("ListModels returned error: %v", err)
			}
			if len(cards) == 0 {
				t.Fatal("ListModels should load at least one model card")
			}
			for _, card := range cards {
				assertCompleteModelCard(t, provider.name, card)
			}
		})
	}
}

func TestOpenAIResponsesUsesDedicatedProviderPackage(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat("openai/models/chat"); !os.IsNotExist(err) {
		t.Fatalf("openai chat models should live directly in model/openai/models, stat err=%v", err)
	}
	if _, err := os.Stat("openai/models/response"); !os.IsNotExist(err) {
		t.Fatalf("OpenAI Responses models should not live under model/openai, stat err=%v", err)
	}
	if _, err := os.Stat("openairesponse/models"); err != nil {
		t.Fatalf("OpenAI Responses models should live under model/openairesponse/models: %v", err)
	}

	openAICards, err := openai.ListModels()
	if err != nil {
		t.Fatalf("openai.ListModels returned error: %v", err)
	}
	for _, card := range openAICards {
		if card.Extra["api"] == "responses" {
			t.Fatalf("openai.ListModels should not return Responses API cards: %#v", card)
		}
	}
	responseCards, err := openairesponse.ListModels()
	if err != nil {
		t.Fatalf("openairesponse.ListModels returned error: %v", err)
	}
	for _, card := range responseCards {
		if card.Extra["api"] != "responses" {
			t.Fatalf("openairesponse cards should identify Responses API: %#v", card)
		}
	}
}

func assertCompleteModelCard(t *testing.T, provider string, card asmodel.ModelCard) {
	t.Helper()
	if card.Name == "" || card.Label == "" || card.ContextSize <= 0 || card.OutputSize <= 0 {
		t.Fatalf("%s model card missing required fields: %#v", provider, card)
	}
	if len(card.InputTypes) == 0 || len(card.OutputTypes) == 0 {
		t.Fatalf("%s model card missing input/output types: %#v", provider, card)
	}
	for _, inputType := range card.InputTypes {
		if !strings.Contains(inputType, "/") {
			t.Fatalf("%s model card should use MIME-style input types, got %q in %#v", provider, inputType, card)
		}
	}
	for _, outputType := range card.OutputTypes {
		if !strings.Contains(outputType, "/") {
			t.Fatalf("%s model card should use MIME-style output types, got %q in %#v", provider, outputType, card)
		}
	}
	for _, capability := range []asmodel.ModelCapability{
		asmodel.ModelCapabilityText,
		asmodel.ModelCapabilityTools,
		asmodel.ModelCapabilityImage,
		asmodel.ModelCapabilityAudio,
		asmodel.ModelCapabilityVideo,
		asmodel.ModelCapabilityStructuredOutput,
		asmodel.ModelCapabilityEmbedding,
		asmodel.ModelCapabilityGeneration,
	} {
		if _, ok := card.Capabilities[capability]; !ok {
			t.Fatalf("%s model card %s missing capability %s: %#v", provider, card.Name, capability, card.Capabilities)
		}
	}
	if card.ParameterSchema["type"] != "object" {
		t.Fatalf("%s model card %s missing object parameter schema: %#v", provider, card.Name, card.ParameterSchema)
	}
}
