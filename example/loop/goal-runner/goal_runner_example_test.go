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

package main

import (
	"context"
	"strings"
	"testing"

	automationstore "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/store"
)

func TestGoalRunnerExampleRequiresDashScopeAPIKey(t *testing.T) {
	t.Setenv("AI_DASHSCOPE_API_KEY", "")

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "AI_DASHSCOPE_API_KEY") {
		t.Fatalf("run error = %v, want missing API key error", err)
	}
}

func TestGoalRunnerRunStopsJoinsRecordedStopReasons(t *testing.T) {
	t.Parallel()

	runs := []automationstore.RunRecord{
		{StopReason: "verifier_failed"},
		{StopReason: "completed"},
	}

	if got := runStops(runs); got != "verifier_failed,completed" {
		t.Fatalf("runStops = %q, want verifier_failed,completed", got)
	}
}
