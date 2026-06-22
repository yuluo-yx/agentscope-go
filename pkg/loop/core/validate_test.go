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
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/loop/core"
)

func TestValidateRejectsInvalidSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     core.Spec
		contains string
	}{
		{
			name:     "empty name",
			spec:     core.Spec{Goal: "triage issues", Mode: core.ModeReportOnly},
			contains: "name",
		},
		{
			name:     "empty goal",
			spec:     core.Spec{Name: "daily-triage", Mode: core.ModeReportOnly},
			contains: "goal",
		},
		{
			name:     "unknown mode",
			spec:     core.Spec{Name: "daily-triage", Goal: "triage issues", Mode: core.Mode("bad")},
			contains: "mode",
		},
		{
			name: "negative budget",
			spec: core.Spec{
				Name:   "daily-triage",
				Goal:   "triage issues",
				Mode:   core.ModeReportOnly,
				Policy: core.Policy{MaxModelCalls: -1},
			},
			contains: "max model calls",
		},
		{
			name: "unattended missing human gate",
			spec: core.Spec{
				Name:   "ci-sweeper",
				Goal:   "fix ci failures",
				Mode:   core.ModeUnattended,
				Policy: core.Policy{MaxAttempts: 3, MaxModelCalls: 6},
			},
			contains: "human gate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := core.Validate(tt.spec)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("Validate error = %v, want substring %q", err, tt.contains)
			}
		})
	}
}

func TestDefaultPolicyByMode(t *testing.T) {
	t.Parallel()

	report := core.DefaultPolicy(core.ModeReportOnly)
	assisted := core.DefaultPolicy(core.ModeAssisted)
	unattended := core.DefaultPolicy(core.ModeUnattended)

	if report.MaxAttempts != 1 {
		t.Fatalf("report-only max attempts = %d, want 1", report.MaxAttempts)
	}
	if assisted.MaxAttempts <= report.MaxAttempts {
		t.Fatalf("assisted max attempts = %d, want more than report-only %d", assisted.MaxAttempts, report.MaxAttempts)
	}
	if unattended.MaxAttempts <= 0 || unattended.MaxModelCalls <= 0 || unattended.MaxToolCalls <= 0 {
		t.Fatalf("unattended policy should set bounded defaults: %#v", unattended)
	}
	if unattended.WrapUpHint == "" {
		t.Fatalf("unattended policy should include wrap-up hint")
	}
}
