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

package openai

import "fmt"

// ChatParameters stores OpenAI Chat Completions generation parameters.
type ChatParameters struct {
	MaxTokens         *int64
	ThinkingEnable    bool
	ReasoningEffort   string
	Temperature       *float64
	TopP              *float64
	ParallelToolCalls *bool
}

// Validate validates Chat Completions parameter ranges.
func (p ChatParameters) Validate() error {
	if p.MaxTokens != nil && *p.MaxTokens <= 0 {
		return fmt.Errorf("openai: max tokens must be positive")
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		return fmt.Errorf("openai: temperature must be between 0 and 2")
	}
	if p.TopP != nil && (*p.TopP <= 0 || *p.TopP > 1) {
		return fmt.Errorf("openai: top_p must be > 0 and <= 1")
	}
	switch p.ReasoningEffort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh":
		return nil
	default:
		return fmt.Errorf("openai: unsupported reasoning effort %q", p.ReasoningEffort)
	}
}

// Clone returns a parameter copy.
func (p ChatParameters) Clone() ChatParameters {
	cp := p
	if p.MaxTokens != nil {
		value := *p.MaxTokens
		cp.MaxTokens = &value
	}
	if p.Temperature != nil {
		value := *p.Temperature
		cp.Temperature = &value
	}
	if p.TopP != nil {
		value := *p.TopP
		cp.TopP = &value
	}
	if p.ParallelToolCalls != nil {
		value := *p.ParallelToolCalls
		cp.ParallelToolCalls = &value
	}
	return cp
}
