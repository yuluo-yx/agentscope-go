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

	automationstore "github.com/yuluo-yx/agentscope-go/loop/automation/store"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestGoalRunnerExampleContinuesUntilVerifierPasses(t *testing.T) {
	t.Parallel()

	if err := run(context.Background()); err != nil {
		t.Fatalf("run returned error: %v", err)
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

func TestGoalRunnerScriptedModelReportsMissingResponse(t *testing.T) {
	t.Parallel()

	scripted := &scriptedChatModel{}

	_, err := scripted.Stream(context.Background(), asmodel.CallRequest{})
	if err == nil || !strings.Contains(err.Error(), "no response") {
		t.Fatalf("Stream error = %v, want missing response error", err)
	}
}
