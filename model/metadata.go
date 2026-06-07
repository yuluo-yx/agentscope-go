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

package model

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/yuluo-yx/agentscope-go/types"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// ModelCardType identifies the type of model described by a model card.
type ModelCardType string

const (
	// ModelCardTypeChat describes a chat/generation model.
	ModelCardTypeChat ModelCardType = "chat_model"
)

// ModelStatus is the lifecycle status of a model card.
type ModelStatus string

const (
	// ModelStatusActive marks a model as supported for new use.
	ModelStatusActive ModelStatus = "active"
	// ModelStatusDeprecated marks a model as available but not preferred.
	ModelStatusDeprecated ModelStatus = "deprecated"
	// ModelStatusSunset marks a model as no longer supported.
	ModelStatusSunset ModelStatus = "sunset"
)

// ModelCapability is one dimension in the model capability matrix.
type ModelCapability string

const (
	ModelCapabilityText             ModelCapability = "text"
	ModelCapabilityTools            ModelCapability = "tools"
	ModelCapabilityImage            ModelCapability = "image"
	ModelCapabilityAudio            ModelCapability = "audio"
	ModelCapabilityVideo            ModelCapability = "video"
	ModelCapabilityStructuredOutput ModelCapability = "structured_output"
	ModelCapabilityEmbedding        ModelCapability = "embedding"
	ModelCapabilityGeneration       ModelCapability = "generation"
)

// ModelCapabilities records supported model capabilities.
type ModelCapabilities map[ModelCapability]bool

// ModelCardDefaults describes provider-level defaults applied to copied model cards.
type ModelCardDefaults struct {
	Capabilities    ModelCapabilities
	ParameterSchema map[string]any
	Extra           map[string]any
}

// NewModelCardDefaults builds provider defaults for Python-compatible YAML cards.
func NewModelCardDefaults(provider string, capabilities ModelCapabilities, extra map[string]any) ModelCardDefaults {
	metadata := utils.CloneAnyMap(extra)
	if metadata == nil {
		metadata = map[string]any{}
	}
	if provider != "" {
		if _, ok := metadata["provider"]; !ok {
			metadata["provider"] = provider
		}
	}
	return ModelCardDefaults{
		Capabilities:    capabilities.Clone(),
		ParameterSchema: CommonChatParameterSchema(),
		Extra:           metadata,
	}
}

// Clone returns a deep copy of model capabilities.
func (c ModelCapabilities) Clone() ModelCapabilities {
	if c == nil {
		return nil
	}
	cp := make(ModelCapabilities, len(c))
	for key, value := range c {
		cp[key] = value
	}
	return cp
}

// CommonChatParameterSchema returns the shared chat generation parameter schema.
func CommonChatParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"max_tokens": map[string]any{
				"type":    "integer",
				"minimum": 1,
			},
			"thinking_enable": map[string]any{
				"type": "boolean",
			},
			"thinking_budget": map[string]any{
				"type":    "integer",
				"minimum": 1,
			},
			"reasoning_effort": map[string]any{
				"type": "string",
				"enum": []any{"none", "minimal", "low", "medium", "high", "xhigh"},
			},
			"temperature": map[string]any{
				"type":    "number",
				"minimum": 0,
				"maximum": 2,
			},
			"top_p": map[string]any{
				"type":             "number",
				"exclusiveMinimum": 0,
				"maximum":          1,
			},
			"parallel_tool_calls": map[string]any{
				"type": "boolean",
			},
		},
		"additionalProperties": false,
	}
}

// ModelCard describes a model and its provider-facing capability metadata.
type ModelCard struct {
	Type               ModelCardType             `json:"type" yaml:"type"`
	Name               string                    `json:"name" yaml:"name"`
	Label              string                    `json:"label" yaml:"label"`
	Status             ModelStatus               `json:"status" yaml:"status"`
	DeprecatedAt       string                    `json:"deprecated_at,omitempty" yaml:"deprecated_at,omitempty"`
	InputTypes         []string                  `json:"input_types" yaml:"input_types"`
	OutputTypes        []string                  `json:"output_types" yaml:"output_types"`
	ContextSize        int                       `json:"context_size" yaml:"context_size"`
	OutputSize         int                       `json:"output_size" yaml:"output_size"`
	Capabilities       ModelCapabilities         `json:"capabilities" yaml:"capabilities"`
	ParameterSchema    map[string]any            `json:"parameter_schema" yaml:"parameter_schema"`
	ParameterOverrides map[string]map[string]any `json:"parameter_overrides,omitempty" yaml:"parameter_overrides,omitempty"`
	Extra              map[string]any            `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// CapabilityError reports that a provider cannot satisfy a requested capability.
type CapabilityError struct {
	Model      string
	Capability ModelCapability
}

// Error returns an auditable capability error string.
func (e *CapabilityError) Error() string {
	if e == nil {
		return "<nil>"
	}
	model := e.Model
	if model == "" {
		model = "<unknown>"
	}
	return fmt.Sprintf("model %s does not support capability %s", model, e.Capability)
}

// ModelLister is implemented by providers that expose local model metadata.
type ModelLister interface {
	ListModels() ([]ModelCard, error)
}

// StructuredOutputRequest asks a provider to produce JSON matching Schema.
type StructuredOutputRequest struct {
	CallRequest
	Name   string
	Schema types.JSONSchema
	Strict bool
}

// StructuredOutputModel is an optional interface for stable structured output support.
type StructuredOutputModel interface {
	GenerateStructured(context.Context, StructuredOutputRequest) (*StructuredResponse, error)
}

// ParseModelCardYAML parses one YAML model card and applies defaults.
func ParseModelCardYAML(data []byte) (ModelCard, error) {
	var card ModelCard
	if err := yaml.Unmarshal(data, &card); err != nil {
		return ModelCard{}, err
	}
	applyModelCardDefaults(&card)
	if err := card.Validate(); err != nil {
		return ModelCard{}, err
	}
	return card, nil
}

// LoadModelCardsFS loads all .yaml/.yml cards from an fs.FS directory.
func LoadModelCardsFS(files fs.FS, dir string) ([]ModelCard, error) {
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
		card, err := ParseModelCardYAML(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].Name < cards[j].Name })
	return cards, nil
}

// LoadModelCardsFSWithDefaults loads model cards and applies provider defaults.
func LoadModelCardsFSWithDefaults(files fs.FS, dir string, defaults ModelCardDefaults) ([]ModelCard, error) {
	cards, err := LoadModelCardsFS(files, dir)
	if err != nil {
		return nil, err
	}
	for index := range cards {
		ApplyModelCardDefaults(&cards[index], defaults)
	}
	return cards, nil
}

// ApplyModelCardDefaults completes a model card without changing provider YAML ownership.
func ApplyModelCardDefaults(card *ModelCard, defaults ModelCardDefaults) {
	if card == nil {
		return
	}
	applyModelCardDefaults(card)
	inferred := inferCapabilities(*card)
	for _, capability := range allModelCapabilities() {
		if _, ok := card.Capabilities[capability]; !ok {
			card.Capabilities[capability] = inferred[capability]
		}
	}
	for capability, supported := range defaults.Capabilities {
		card.Capabilities[capability] = supported
	}
	if isEmptyParameterSchema(card.ParameterSchema) && defaults.ParameterSchema != nil {
		card.ParameterSchema = utils.CloneAnyMap(defaults.ParameterSchema)
	}
	if card.Extra == nil {
		card.Extra = map[string]any{}
	}
	for key, value := range defaults.Extra {
		if _, ok := card.Extra[key]; !ok {
			card.Extra[key] = value
		}
	}
}

// Validate checks required model card fields.
func (c ModelCard) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("model card: name is required")
	}
	if strings.TrimSpace(c.Label) == "" {
		return fmt.Errorf("model card %s: label is required", c.Name)
	}
	if c.ContextSize <= 0 {
		return fmt.Errorf("model card %s: context_size must be positive", c.Name)
	}
	if c.OutputSize <= 0 {
		return fmt.Errorf("model card %s: output_size must be positive", c.Name)
	}
	if c.Status != ModelStatusActive && c.Status != ModelStatusDeprecated && c.Status != ModelStatusSunset {
		return fmt.Errorf("model card %s: unsupported status %q", c.Name, c.Status)
	}
	return nil
}

// Clone returns a deep copy of the model card.
func (c ModelCard) Clone() ModelCard {
	cp := c
	cp.InputTypes = append([]string(nil), c.InputTypes...)
	cp.OutputTypes = append([]string(nil), c.OutputTypes...)
	if c.Capabilities != nil {
		cp.Capabilities = make(ModelCapabilities, len(c.Capabilities))
		for key, value := range c.Capabilities {
			cp.Capabilities[key] = value
		}
	}
	cp.ParameterSchema = utils.CloneAnyMap(c.ParameterSchema)
	if c.ParameterOverrides != nil {
		cp.ParameterOverrides = make(map[string]map[string]any, len(c.ParameterOverrides))
		for key, value := range c.ParameterOverrides {
			cp.ParameterOverrides[key] = utils.CloneAnyMap(value)
		}
	}
	cp.Extra = utils.CloneAnyMap(c.Extra)
	return cp
}

// Supports reports whether a card declares the given capability.
func (c ModelCard) Supports(capability ModelCapability) bool {
	return c.Capabilities != nil && c.Capabilities[capability]
}

// Require returns a typed error when capability is not supported.
func (c ModelCard) Require(capability ModelCapability) error {
	if c.Supports(capability) {
		return nil
	}
	return &CapabilityError{Model: c.Name, Capability: capability}
}

func applyModelCardDefaults(card *ModelCard) {
	if card.Type == "" {
		card.Type = ModelCardTypeChat
	}
	if card.Status == "" {
		card.Status = ModelStatusActive
	}
	if len(card.InputTypes) == 0 {
		card.InputTypes = []string{"text"}
	}
	if len(card.OutputTypes) == 0 {
		card.OutputTypes = []string{"text"}
	}
	if card.Capabilities == nil {
		card.Capabilities = ModelCapabilities{}
	}
	if card.ParameterSchema == nil {
		card.ParameterSchema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if card.ParameterOverrides == nil {
		card.ParameterOverrides = map[string]map[string]any{}
	}
	if card.Extra == nil {
		card.Extra = map[string]any{}
	}
}

func inferCapabilities(card ModelCard) ModelCapabilities {
	capabilities := ModelCapabilities{
		ModelCapabilityText:             false,
		ModelCapabilityTools:            false,
		ModelCapabilityImage:            false,
		ModelCapabilityAudio:            false,
		ModelCapabilityVideo:            false,
		ModelCapabilityStructuredOutput: false,
		ModelCapabilityEmbedding:        card.Type == "embedding_model",
		ModelCapabilityGeneration:       card.Type == "" || card.Type == ModelCardTypeChat,
	}
	for _, mediaType := range append(append([]string(nil), card.InputTypes...), card.OutputTypes...) {
		switch {
		case strings.HasPrefix(mediaType, "text/") || mediaType == "application/x-thinking":
			capabilities[ModelCapabilityText] = true
		case strings.HasPrefix(mediaType, "image/"):
			capabilities[ModelCapabilityImage] = true
		case strings.HasPrefix(mediaType, "audio/"):
			capabilities[ModelCapabilityAudio] = true
		case strings.HasPrefix(mediaType, "video/"):
			capabilities[ModelCapabilityVideo] = true
		}
	}
	return capabilities
}

func allModelCapabilities() []ModelCapability {
	return []ModelCapability{
		ModelCapabilityText,
		ModelCapabilityTools,
		ModelCapabilityImage,
		ModelCapabilityAudio,
		ModelCapabilityVideo,
		ModelCapabilityStructuredOutput,
		ModelCapabilityEmbedding,
		ModelCapabilityGeneration,
	}
}

func isEmptyParameterSchema(schema map[string]any) bool {
	if schema == nil {
		return true
	}
	if len(schema) == 0 {
		return true
	}
	properties, hasProperties := schema["properties"].(map[string]any)
	return schema["type"] == "object" && (!hasProperties || len(properties) == 0)
}
