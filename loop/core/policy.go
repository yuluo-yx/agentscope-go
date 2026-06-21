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

package core

const defaultWrapUpHint = "<system-reminder>The current Loop Engineering budget or stop condition has been reached. Do not invoke more tools. Summarize completed work, evidence, blockers, and the next action for the user.</system-reminder>"

// Policy bounds one loop run.
type Policy struct {
	MaxIterations   int    `json:"max_iterations,omitempty"`
	MaxModelCalls   int    `json:"max_model_calls,omitempty"`
	MaxToolCalls    int    `json:"max_tool_calls,omitempty"`
	MaxInputTokens  int    `json:"max_input_tokens,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	MaxAttempts     int    `json:"max_attempts,omitempty"`
	WrapUpHint      string `json:"wrap_up_hint,omitempty"`
}

// DefaultPolicy returns conservative defaults for the requested loop mode.
func DefaultPolicy(mode Mode) Policy {
	switch mode {
	case ModeAssisted:
		return Policy{MaxIterations: 6, MaxModelCalls: 8, MaxToolCalls: 12, MaxAttempts: 3, WrapUpHint: defaultWrapUpHint}
	case ModeUnattended:
		return Policy{MaxIterations: 6, MaxModelCalls: 8, MaxToolCalls: 10, MaxAttempts: 3, WrapUpHint: defaultWrapUpHint}
	default:
		return Policy{MaxIterations: 3, MaxModelCalls: 4, MaxToolCalls: 6, MaxAttempts: 1, WrapUpHint: defaultWrapUpHint}
	}
}

func (p Policy) withDefaults(mode Mode) Policy {
	defaults := DefaultPolicy(mode)
	if p.MaxIterations == 0 {
		p.MaxIterations = defaults.MaxIterations
	}
	if p.MaxModelCalls == 0 {
		p.MaxModelCalls = defaults.MaxModelCalls
	}
	if p.MaxToolCalls == 0 {
		p.MaxToolCalls = defaults.MaxToolCalls
	}
	if p.MaxAttempts == 0 {
		p.MaxAttempts = defaults.MaxAttempts
	}
	if p.WrapUpHint == "" {
		p.WrapUpHint = defaults.WrapUpHint
	}
	return p
}

func (p Policy) hasAnyBound() bool {
	return p.MaxIterations > 0 ||
		p.MaxModelCalls > 0 ||
		p.MaxToolCalls > 0 ||
		p.MaxInputTokens > 0 ||
		p.MaxOutputTokens > 0 ||
		p.MaxAttempts > 0
}
