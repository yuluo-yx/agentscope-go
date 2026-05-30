// Copyright 20\d\d AgentScope Go
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

package anthropic

import "fmt"

const defaultMaxTokens int64 = 4096

// ChatParameters stores Anthropic Messages API generation parameters.
type ChatParameters struct {
	MaxTokens            *int64
	ThinkingBudgetTokens *int64
	Temperature          *float64
	TopP                 *float64
	TopK                 *int64
	ThinkingDisplay      string
}

// Validate checks whether Anthropic parameters satisfy SDK and API constraints.
func (p ChatParameters) Validate() error {
	if err := validateTokenLimits(p.MaxTokens, p.ThinkingBudgetTokens); err != nil {
		return err
	}
	if err := validateSampling(p.Temperature, p.TopP, p.TopK); err != nil {
		return err
	}
	return validateThinkingDisplay(p.ThinkingDisplay)
}

// Clone returns a deep copy so later caller changes do not affect the provider.
func (p ChatParameters) Clone() ChatParameters {
	clone := p
	if p.MaxTokens != nil {
		value := *p.MaxTokens
		clone.MaxTokens = &value
	}
	if p.ThinkingBudgetTokens != nil {
		value := *p.ThinkingBudgetTokens
		clone.ThinkingBudgetTokens = &value
	}
	if p.Temperature != nil {
		value := *p.Temperature
		clone.Temperature = &value
	}
	if p.TopP != nil {
		value := *p.TopP
		clone.TopP = &value
	}
	if p.TopK != nil {
		value := *p.TopK
		clone.TopK = &value
	}
	return clone
}

func validateTokenLimits(maxTokens, thinkingBudgetTokens *int64) error {
	if maxTokens != nil && *maxTokens <= 0 {
		return fmt.Errorf("max tokens must be positive")
	}
	if thinkingBudgetTokens == nil {
		return nil
	}
	resolvedMaxTokens := defaultMaxTokens
	if maxTokens != nil {
		resolvedMaxTokens = *maxTokens
	}
	if *thinkingBudgetTokens < 1024 {
		return fmt.Errorf("thinking budget tokens must be at least 1024")
	}
	if *thinkingBudgetTokens >= resolvedMaxTokens {
		return fmt.Errorf("thinking budget tokens must be smaller than max tokens")
	}
	return nil
}

func validateSampling(temperature, topP *float64, topK *int64) error {
	if temperature != nil && (*temperature < 0 || *temperature > 1) {
		return fmt.Errorf("temperature must be between 0 and 1")
	}
	if topP != nil && (*topP <= 0 || *topP > 1) {
		return fmt.Errorf("top p must be in (0, 1]")
	}
	if topK != nil && *topK <= 0 {
		return fmt.Errorf("top k must be positive")
	}
	return nil
}

func validateThinkingDisplay(display string) error {
	switch display {
	case "", "summarized", "omitted":
		return nil
	default:
		return fmt.Errorf("unsupported thinking display %q", display)
	}
}
