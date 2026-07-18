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

	"github.com/yuluo-yx/agentscope-go/pkg/types"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

const (
	defaultCompressionPrompt = "<system-hint>You have been working on the task described above but have not yet completed it. Now write a continuation summary that will allow you to resume work efficiently in a future context window where the conversation history will be replaced with this summary. Your summary should be structured, concise, and actionable.\nThe current time is {current_time}.\nThis summary may itself be summarized again later, and the conversation history it refers to will be gone, so every reference must be self-contained — resolve anything that depends on the vanished context into an absolute, fully-qualified form:\n- Time: convert relative expressions ('today', 'now', 'yesterday', 'tomorrow', 'recently') to absolute dates using the current time above; re-anchor them even if an earlier summary already wrote them relatively.\n- Names & pointers: use file paths, symbol names, PR/issue numbers, IDs, URLs, and exact commands/error strings verbatim instead of 'this file', 'the above', 'the second approach', 'the 5 failing tests'.\n- In-flight work: record everything still pending, especially tools launched in the background whose results you are still waiting on — give each one's id and a short note of what it is doing — and mark each item's owner (user request vs your own decision) and status (done / pending / blocked).\n</system-hint>"
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
		ToolResultLimit:   50000,
	}
}

// Clone returns a deep copy of context configuration.
func (c ContextConfig) Clone() ContextConfig {
	cp := c
	cp.SummarySchema = utils.CloneAnyMap(c.SummarySchema)
	return cp
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
		"task_overview":         map[string]any{"type": "string"},
		"current_state":         map[string]any{"type": "string"},
		"important_discoveries": map[string]any{"type": "string"},
		"next_steps":            map[string]any{"type": "string"},
		"context_to_preserve":   map[string]any{"type": "string"},
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
	MaxRetries    int               `json:"max_retries"`
	FallbackModel ChatModel         `json:"-"`
	ToolChoice    *types.ToolChoice `json:"tool_choice,omitempty"`
}

// DefaultModelConfig returns default model configuration.
func DefaultModelConfig() ModelConfig {
	return ModelConfig{MaxRetries: 3}
}

// Clone returns a deep copy of model configuration.
func (c ModelConfig) Clone() ModelConfig {
	cp := c
	cp.ToolChoice = c.ToolChoice.Clone()
	return cp
}

// Validate validates model configuration.
func (c ModelConfig) Validate() error {
	if c.MaxRetries <= 0 {
		return fmt.Errorf("agentscope: max retries must be greater than 0")
	}
	if err := c.ToolChoice.Validate(nil); err != nil {
		return fmt.Errorf("agentscope: invalid tool choice: %w", err)
	}
	return nil
}

// AgentConfig groups all Agent runtime configuration knobs.
type AgentConfig struct {
	Model   ModelConfig   `json:"model"`
	Context ContextConfig `json:"context"`
	ReAct   ReActConfig   `json:"react"`
}

// DefaultAgentConfig returns the complete default Agent configuration.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		Model:   DefaultModelConfig(),
		Context: DefaultContextConfig(),
		ReAct:   DefaultReActConfig(),
	}
}

// Clone returns a deep copy of the complete Agent configuration.
func (c AgentConfig) Clone() AgentConfig {
	return AgentConfig{
		Model:   c.Model.Clone(),
		Context: c.Context.Clone(),
		ReAct:   c.ReAct,
	}
}

// Validate validates every Agent configuration section.
func (c AgentConfig) Validate() error {
	if err := c.Model.Validate(); err != nil {
		return fmt.Errorf("agentscope: invalid model config: %w", err)
	}
	if err := c.Context.Validate(); err != nil {
		return fmt.Errorf("agentscope: invalid context config: %w", err)
	}
	if err := c.ReAct.Validate(); err != nil {
		return fmt.Errorf("agentscope: invalid ReAct config: %w", err)
	}
	return nil
}
