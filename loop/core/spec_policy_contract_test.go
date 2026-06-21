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

package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/loop/core"
)

func TestNormalizeSpecAppliesDefaultsWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	spec := core.Spec{
		Name:            "daily-triage",
		Goal:            "scan repository signals",
		SuccessCriteria: []core.SuccessCriterion{{Name: "report"}},
		Scope: core.Scope{
			Paths:    []string{"."},
			Metadata: map[string]any{"owner": "automation"},
		},
		HumanGates: []core.HumanGate{{Name: "release", MatchPaths: []string{"release/**"}}},
		Metadata:   map[string]any{"risk": "low"},
	}

	normalized := core.NormalizeSpec(spec)
	normalized.Scope.Paths[0] = "mutated"
	normalized.Scope.Metadata["owner"] = "mutated"
	normalized.HumanGates[0].MatchPaths[0] = "mutated"
	normalized.Metadata["risk"] = "mutated"

	if normalized.Mode != core.ModeReportOnly || normalized.Policy.MaxAttempts != 1 || normalized.Policy.WrapUpHint == "" {
		t.Fatalf("NormalizeSpec did not apply report-only defaults: %#v", normalized)
	}
	if spec.Mode != "" || spec.Scope.Paths[0] != "." || spec.Scope.Metadata["owner"] != "automation" ||
		spec.HumanGates[0].MatchPaths[0] != "release/**" || spec.Metadata["risk"] != "low" {
		t.Fatalf("NormalizeSpec mutated input spec: %#v", spec)
	}
}

func TestCloneSpecCopiesNestedSlicesAndMetadata(t *testing.T) {
	t.Parallel()

	spec := core.Spec{
		Name:            "release-check",
		Goal:            "verify release readiness",
		NonGoals:        []string{"merge code"},
		SuccessCriteria: []core.SuccessCriterion{{Name: "tests"}},
		Scope: core.Scope{
			Paths:      []string{"loop"},
			ToolNames:  []string{"Bash"},
			TaskLabels: []string{"release"},
			Metadata:   map[string]any{"channel": "stable"},
		},
		HumanGates: []core.HumanGate{{Name: "security", MatchPaths: []string{"SECURITY.md"}}},
		Metadata:   map[string]any{"priority": "high"},
	}

	clone := core.CloneSpec(spec)
	clone.NonGoals[0] = "mutated"
	clone.SuccessCriteria[0].Name = "mutated"
	clone.Scope.Paths[0] = "mutated"
	clone.Scope.ToolNames[0] = "mutated"
	clone.Scope.TaskLabels[0] = "mutated"
	clone.Scope.Metadata["channel"] = "mutated"
	clone.HumanGates[0].MatchPaths[0] = "mutated"
	clone.Metadata["priority"] = "mutated"

	if spec.NonGoals[0] != "merge code" || spec.SuccessCriteria[0].Name != "tests" ||
		spec.Scope.Paths[0] != "loop" || spec.Scope.ToolNames[0] != "Bash" ||
		spec.Scope.TaskLabels[0] != "release" || spec.Scope.Metadata["channel"] != "stable" ||
		spec.HumanGates[0].MatchPaths[0] != "SECURITY.md" || spec.Metadata["priority"] != "high" {
		t.Fatalf("CloneSpec did not isolate nested fields: %#v", spec)
	}
}

func TestValidateRejectsEachNegativePolicyBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		policy   core.Policy
		contains string
	}{
		{name: "max iterations", policy: core.Policy{MaxIterations: -1}, contains: "max iterations"},
		{name: "max tool calls", policy: core.Policy{MaxToolCalls: -1}, contains: "max tool calls"},
		{name: "max input tokens", policy: core.Policy{MaxInputTokens: -1}, contains: "max input tokens"},
		{name: "max output tokens", policy: core.Policy{MaxOutputTokens: -1}, contains: "max output tokens"},
		{name: "max attempts", policy: core.Policy{MaxAttempts: -1}, contains: "max attempts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := core.Spec{Name: "budget-check", Goal: "reject invalid budget", Mode: core.ModeReportOnly, Policy: tt.policy}
			err := core.Validate(spec)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("Validate error = %v, want containing %q", err, tt.contains)
			}
		})
	}
}

func TestObserverAndVerifierFuncHandleNilAndCallback(t *testing.T) {
	t.Parallel()

	if err := (core.ObserverFunc(nil)).ObserveLoop(context.Background(), core.RunEvent{}); err != nil {
		t.Fatalf("nil ObserverFunc returned error: %v", err)
	}
	observed := false
	if err := (core.ObserverFunc(func(_ context.Context, event core.RunEvent) error {
		observed = event.Type == core.EventStart
		return nil
	})).ObserveLoop(context.Background(), core.RunEvent{Type: core.EventStart}); err != nil {
		t.Fatalf("ObserverFunc returned error: %v", err)
	}
	if !observed {
		t.Fatalf("ObserverFunc did not receive event")
	}

	result, err := (core.VerifierFunc(nil)).Verify(context.Background(), core.VerificationInput{})
	if err != nil || result.Passed {
		t.Fatalf("nil VerifierFunc = %#v, %v", result, err)
	}
	result, err = (core.VerifierFunc(func(context.Context, core.VerificationInput) (core.VerificationResult, error) {
		return core.VerificationResult{Passed: true, Reason: "accepted"}, nil
	})).Verify(context.Background(), core.VerificationInput{})
	if err != nil || !result.Passed || result.Reason != "accepted" {
		t.Fatalf("VerifierFunc callback result = %#v, %v", result, err)
	}
}
