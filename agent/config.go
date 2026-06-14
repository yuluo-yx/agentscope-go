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

package agent

import (
	"fmt"
)

const (
	defaultCompressionPrompt = "<system-hint>You have been working on the task described above but have not yet completed it. Now write a continuation summary that will allow you to resume work efficiently in a future context window where the conversation history will be replaced with this summary. Your summary should be structured, concise, and actionable.</system-hint>"
	defaultSummaryTemplate   = "<system-info>Here is a summary of your previous work\n# Task Overview\n{task_overview}\n\n# Current State\n{current_state}\n\n# Important Discoveries\n{important_discoveries}\n\n# Next Steps\n{next_steps}\n\n# Context to Preserve\n{context_to_preserve}</system-info>"
)

// ContextConfig controls context compression thresholds, retention, and summaries.
type ContextConfig struct {
	TriggerRatio      float64        `json:"trigger_ratio"`
	ReserveRatio      float64        `json:"reserve_ratio"`
	MaxTokens         int            `json:"max_tokens,omitempty"`
	CompressionPrompt string         `json:"compression_prompt"`
	SummaryTemplate   string         `json:"summary_template"`
	SummarySchema     map[string]any `json:"summary_schema"`
	ToolResultLimit   int            `json:"tool_result_limit"`
}

// DefaultContextConfig returns context defaults aligned with the Python version.
func DefaultContextConfig() ContextConfig {

	return ContextConfig{
		TriggerRatio:      0.8,
		ReserveRatio:      0.1,
		MaxTokens:         0,
		CompressionPrompt: defaultCompressionPrompt,
		SummaryTemplate:   defaultSummaryTemplate,
		SummarySchema:     DefaultSummarySchema(),
		ToolResultLimit:   3000,
	}
}

// Validate validates context configuration.
func (c ContextConfig) Validate() error {
	if c.TriggerRatio <= 0 || c.TriggerRatio >= 0.9 {
		return fmt.Errorf("agentscope: trigger ratio must be > 0 and < 0.9")
	}
	if c.ReserveRatio <= 0 || c.ReserveRatio >= 0.9 {
		return fmt.Errorf("agentscope: reserve ratio must be > 0 and < 0.9")
	}
	if c.ReserveRatio >= c.TriggerRatio {
		return fmt.Errorf("agentscope: reserve ratio must be smaller than trigger ratio")
	}
	if c.MaxTokens < 0 {
		return fmt.Errorf("agentscope: max tokens must be non-negative")
	}
	if c.ToolResultLimit <= 0 {
		return fmt.Errorf("agentscope: tool result limit must be positive")
	}
	return nil
}

// DefaultSummarySchema returns the JSON Schema for structured context summaries.
func DefaultSummarySchema() map[string]any {
	properties := map[string]any{
		"task_overview":         map[string]any{"type": "string", "maxLength": 300},
		"current_state":         map[string]any{"type": "string", "maxLength": 300},
		"important_discoveries": map[string]any{"type": "string", "maxLength": 300},
		"next_steps":            map[string]any{"type": "string", "maxLength": 200},
		"context_to_preserve":   map[string]any{"type": "string", "maxLength": 300},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             []any{"task_overview", "current_state", "important_discoveries", "next_steps", "context_to_preserve"},
	}
}

// ReActConfig controls the reasoning-action loop for one reply.
type ReActConfig struct {
	MaxIters     int  `json:"max_iters"`
	StopOnReject bool `json:"stop_on_reject"`
}

// DefaultReActConfig returns default ReAct loop configuration.
func DefaultReActConfig() ReActConfig {
	return ReActConfig{MaxIters: 20, StopOnReject: false}
}

// Validate validates ReAct loop configuration.
func (c ReActConfig) Validate() error {
	if c.MaxIters <= 0 {
		return fmt.Errorf("agentscope: max iters must be positive")
	}
	return nil
}

// ModelConfig controls model retries and fallback model behavior.
type ModelConfig struct {
	MaxRetries    int       `json:"max_retries"`
	FallbackModel ChatModel `json:"-"`
}

// DefaultModelConfig returns default model configuration.
func DefaultModelConfig() ModelConfig {
	return ModelConfig{MaxRetries: 3}
}

// Validate validates model configuration.
func (c ModelConfig) Validate() error {
	if c.MaxRetries <= 0 {
		return fmt.Errorf("agentscope: max retries must be greater than 0")
	}
	return nil
}
