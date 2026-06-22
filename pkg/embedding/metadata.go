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

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// ModelCardType identifies the type of embedding model described by a card.
type ModelCardType string

const (
	// ModelCardTypeEmbedding describes an embedding model.
	ModelCardTypeEmbedding ModelCardType = "embedding_model"
)

// ModelStatus is the lifecycle status of an embedding model card.
type ModelStatus string

const (
	// ModelStatusActive marks a model as supported for new use.
	ModelStatusActive ModelStatus = "active"
	// ModelStatusDeprecated marks a model as available but not preferred.
	ModelStatusDeprecated ModelStatus = "deprecated"
	// ModelStatusSunset marks a model as no longer supported.
	ModelStatusSunset ModelStatus = "sunset"
)

// ModelCard describes an embedding model and its provider-facing metadata.
type ModelCard struct {
	Type               ModelCardType             `json:"type" yaml:"type"`
	Name               string                    `json:"name" yaml:"name"`
	Label              string                    `json:"label" yaml:"label"`
	Status             ModelStatus               `json:"status" yaml:"status"`
	InputTypes         []string                  `json:"input_types" yaml:"input_types"`
	OutputTypes        []string                  `json:"output_types" yaml:"output_types"`
	ContextSize        int                       `json:"context_size,omitempty" yaml:"context_size,omitempty"`
	ParameterSchema    map[string]any            `json:"parameter_schema" yaml:"parameter_schema"`
	ParameterOverrides map[string]map[string]any `json:"parameter_overrides,omitempty" yaml:"parameter_overrides,omitempty"`
}

// CommonParameterSchema returns the shared embedding parameter schema.
func CommonParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dimensions": map[string]any{
				"type":        "integer",
				"title":       "Dimensions",
				"description": "The output embedding vector dimensions.",
				"minimum":     1,
			},
		},
		"required": []any{},
	}
}

// ParseModelCardYAML parses one Python-compatible YAML embedding card.
func ParseModelCardYAML(data []byte, baseSchema map[string]any) (ModelCard, error) {
	var raw embeddingModelCardYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ModelCard{}, err
	}
	card := ModelCard{
		Type:               raw.Type,
		Name:               raw.Name,
		Label:              raw.Label,
		Status:             raw.Status,
		InputTypes:         append([]string(nil), raw.InputTypes...),
		OutputTypes:        append([]string(nil), raw.OutputTypes...),
		ContextSize:        raw.ContextSize,
		ParameterOverrides: cloneOverrideMap(raw.ParameterOverrides),
	}
	applyModelCardDefaults(&card)
	card.ParameterSchema = mergeParameterSchema(baseSchema, card.ParameterOverrides)
	if err := card.Validate(); err != nil {
		return ModelCard{}, err
	}
	return card, nil
}

// LoadModelCardsFS loads all .yaml/.yml embedding cards from an fs.FS directory.
func LoadModelCardsFS(files fs.FS, dir string, baseSchema map[string]any) ([]ModelCard, error) {
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return nil, err
	}
	cards := make([]ModelCard, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		data, err := fs.ReadFile(files, strings.TrimRight(dir, "/")+"/"+name)
		if err != nil {
			return nil, err
		}
		card, err := ParseModelCardYAML(data, baseSchema)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].Name < cards[j].Name })
	return cards, nil
}

// Validate checks required model card fields.
func (c ModelCard) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("embedding model card: name is required")
	}
	if strings.TrimSpace(c.Label) == "" {
		return fmt.Errorf("embedding model card %s: label is required", c.Name)
	}
	if c.ContextSize < 0 {
		return fmt.Errorf("embedding model card %s: context_size must be non-negative", c.Name)
	}
	if c.Status != ModelStatusActive && c.Status != ModelStatusDeprecated && c.Status != ModelStatusSunset {
		return fmt.Errorf("embedding model card %s: unsupported status %q", c.Name, c.Status)
	}
	return nil
}

// Clone returns a deep copy of the model card.
func (c ModelCard) Clone() ModelCard {
	cp := c
	cp.InputTypes = append([]string(nil), c.InputTypes...)
	cp.OutputTypes = append([]string(nil), c.OutputTypes...)
	cp.ParameterSchema = utils.CloneAnyMap(c.ParameterSchema)
	cp.ParameterOverrides = cloneOverrideMap(c.ParameterOverrides)
	return cp
}

type embeddingModelCardYAML struct {
	Type               ModelCardType             `yaml:"type"`
	Name               string                    `yaml:"name"`
	Label              string                    `yaml:"label"`
	Status             ModelStatus               `yaml:"status"`
	InputTypes         []string                  `yaml:"input_types"`
	OutputTypes        []string                  `yaml:"output_types"`
	ContextSize        int                       `yaml:"context_size"`
	ParameterOverrides map[string]map[string]any `yaml:"parameter_overrides"`
}

func applyModelCardDefaults(card *ModelCard) {
	if card.Type == "" {
		card.Type = ModelCardTypeEmbedding
	}
	if card.Status == "" {
		card.Status = ModelStatusActive
	}
	if len(card.InputTypes) == 0 {
		card.InputTypes = []string{"text/plain"}
	}
	if len(card.OutputTypes) == 0 {
		card.OutputTypes = []string{"application/x-embedding"}
	}
	if card.ParameterOverrides == nil {
		card.ParameterOverrides = map[string]map[string]any{}
	}
}

func mergeParameterSchema(baseSchema map[string]any, overrides map[string]map[string]any) map[string]any {
	schema := utils.CloneAnyMap(baseSchema)
	if schema == nil {
		schema = CommonParameterSchema()
	}
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
	}
	for paramName, override := range overrides {
		if override == nil || override["hidden"] == true {
			delete(properties, paramName)
			continue
		}
		existing, ok := properties[paramName].(map[string]any)
		if !ok {
			continue
		}
		merged := utils.CloneAnyMap(existing)
		for key, value := range override {
			merged[key] = utils.CloneAny(value)
		}
		properties[paramName] = merged
	}
	schema["type"] = "object"
	schema["properties"] = properties
	if _, ok := schema["required"]; !ok {
		schema["required"] = []any{}
	}
	return schema
}

func cloneOverrideMap(in map[string]map[string]any) map[string]map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]map[string]any, len(in))
	for key, value := range in {
		out[key] = utils.CloneAnyMap(value)
	}
	return out
}
