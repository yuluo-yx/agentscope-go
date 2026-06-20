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

package state_test

import (
	"encoding/json"
	"testing"

	"github.com/yuluo-yx/agentscope-go/state"
)

func TestLoopContextClone(t *testing.T) {
	t.Parallel()

	ctx := &state.LoopContext{
		Name:        "daily-triage",
		Goal:        "scan repository",
		Mode:        "report_only",
		ModelCalls:  1,
		StopReason:  state.LoopStopCompleted,
		Metadata:    map[string]any{"source": "test"},
		HumanGates:  []state.LoopHumanGate{{Name: "security", Description: "manual review"}},
		ScopePaths:  []string{"docs/**"},
		ScopeTools:  []string{"Read"},
		ScopeLabels: []string{"triage"},
		LastVerification: &state.LoopVerification{
			Passed:   true,
			Reason:   "tests passed",
			Evidence: []string{"go test ./..."},
		},
		Runs: []state.LoopRun{{
			ReplyID:      "reply-1",
			ModelCalls:   1,
			StopReason:   state.LoopStopCompleted,
			CustomEvents: []string{"loop.start"},
		}},
	}

	cloned := ctx.Clone()
	if cloned == ctx || cloned.LastVerification == ctx.LastVerification {
		t.Fatalf("Clone should deep-copy nested pointers")
	}
	cloned.Metadata["source"] = "changed"
	cloned.HumanGates[0].Name = "changed"
	cloned.ScopePaths[0] = "changed"
	cloned.LastVerification.Evidence[0] = "changed"
	cloned.Runs[0].CustomEvents[0] = "changed"

	if ctx.Metadata["source"] != "test" || ctx.HumanGates[0].Name != "security" || ctx.ScopePaths[0] != "docs/**" {
		t.Fatalf("Clone mutated source context: %#v", ctx)
	}
	if ctx.LastVerification.Evidence[0] != "go test ./..." || ctx.Runs[0].CustomEvents[0] != "loop.start" {
		t.Fatalf("Clone mutated nested state: %#v", ctx)
	}
}

func TestAgentStateCloneIncludesLoopContext(t *testing.T) {
	t.Parallel()

	agentState := state.NewAgentState()
	agentState.LoopContext = &state.LoopContext{Name: "daily-triage", Goal: "scan repository"}

	cloned := agentState.Clone()
	if cloned.LoopContext == nil || cloned.LoopContext == agentState.LoopContext {
		t.Fatalf("AgentState.Clone should deep-copy LoopContext")
	}
	cloned.LoopContext.Name = "changed"
	if agentState.LoopContext.Name != "daily-triage" {
		t.Fatalf("mutating clone should not affect source: %#v", agentState.LoopContext)
	}
}

func TestAgentStateJSONBackwardCompatibility(t *testing.T) {
	t.Parallel()

	var agentState state.AgentState
	if err := json.Unmarshal([]byte(`{"session_id":"session-1","context":[],"reply_id":"reply-1","cur_iter":0}`), &agentState); err != nil {
		t.Fatalf("Unmarshal old AgentState returned error: %v", err)
	}
	if agentState.LoopContext != nil {
		t.Fatalf("old AgentState JSON should not force loop context, got %#v", agentState.LoopContext)
	}
}
