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

import (
	"fmt"
	"strings"
)

// Validate checks whether a loop spec is internally consistent.
func Validate(spec Spec) error {
	if strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("loop: name is empty")
	}
	if strings.TrimSpace(spec.Goal) == "" {
		return fmt.Errorf("loop: goal is empty")
	}
	if !validMode(spec.Mode) {
		return fmt.Errorf("loop: unsupported mode %q", spec.Mode)
	}
	if err := validatePolicy(spec.Policy); err != nil {
		return err
	}
	if spec.Mode == ModeUnattended {
		if !spec.Policy.hasAnyBound() {
			return fmt.Errorf("loop: unattended mode requires bounded policy")
		}
		if len(spec.HumanGates) == 0 {
			return fmt.Errorf("loop: unattended mode requires at least one human gate")
		}
	}
	return nil
}

func validatePolicy(policy Policy) error {
	switch {
	case policy.MaxIterations < 0:
		return fmt.Errorf("loop: max iterations must be non-negative")
	case policy.MaxModelCalls < 0:
		return fmt.Errorf("loop: max model calls must be non-negative")
	case policy.MaxToolCalls < 0:
		return fmt.Errorf("loop: max tool calls must be non-negative")
	case policy.MaxInputTokens < 0:
		return fmt.Errorf("loop: max input tokens must be non-negative")
	case policy.MaxOutputTokens < 0:
		return fmt.Errorf("loop: max output tokens must be non-negative")
	case policy.MaxAttempts < 0:
		return fmt.Errorf("loop: max attempts must be non-negative")
	default:
		return nil
	}
}

// NormalizeSpec applies mode and policy defaults without mutating the input.
func NormalizeSpec(spec Spec) Spec {
	spec = spec.clone()
	if spec.Mode == "" {
		spec.Mode = ModeReportOnly
	}
	spec.Policy = spec.Policy.withDefaults(spec.Mode)
	return spec
}
