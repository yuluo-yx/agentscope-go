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

package state

import (
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// LoopStopReason records why a loop run stopped.
type LoopStopReason string

const (
	LoopStopCompleted       LoopStopReason = "completed"
	LoopStopBudgetExceeded  LoopStopReason = "budget_exceeded"
	LoopStopMaxIterations   LoopStopReason = "max_iterations"
	LoopStopWaitingUser     LoopStopReason = "waiting_user"
	LoopStopWaitingExternal LoopStopReason = "waiting_external"
	LoopStopVerifierFailed  LoopStopReason = "verifier_failed"
	LoopStopError           LoopStopReason = "error"
)

// LoopHumanGate records one human handoff rule attached to a loop.
type LoopHumanGate struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	MatchPaths  []string `json:"match_paths,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

// LoopVerification stores the latest verifier decision for a loop run.
type LoopVerification struct {
	Passed     bool      `json:"passed"`
	Reason     string    `json:"reason,omitempty"`
	Evidence   []string  `json:"evidence,omitempty"`
	NextAction string    `json:"next_action,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

// Clone returns a deep copy of the verification result.
func (v *LoopVerification) Clone() *LoopVerification {
	if v == nil {
		return nil
	}
	cp := *v
	cp.Evidence = append([]string(nil), v.Evidence...)
	return &cp
}

// LoopRun stores a compact append-only summary for one Agent reply run.
type LoopRun struct {
	ReplyID      string         `json:"reply_id,omitempty"`
	StartedAt    time.Time      `json:"started_at,omitempty"`
	FinishedAt   time.Time      `json:"finished_at,omitempty"`
	Iterations   int            `json:"iterations,omitempty"`
	ModelCalls   int            `json:"model_calls,omitempty"`
	ToolCalls    int            `json:"tool_calls,omitempty"`
	InputTokens  int            `json:"input_tokens,omitempty"`
	OutputTokens int            `json:"output_tokens,omitempty"`
	StopReason   LoopStopReason `json:"stop_reason,omitempty"`
	CustomEvents []string       `json:"custom_events,omitempty"`
}

// Clone returns a deep copy of the run summary.
func (r LoopRun) Clone() LoopRun {
	cp := r
	cp.CustomEvents = append([]string(nil), r.CustomEvents...)
	return cp
}

// LoopContext stores framework-level loop engineering state for an Agent session.
type LoopContext struct {
	Name             string            `json:"name,omitempty"`
	Goal             string            `json:"goal,omitempty"`
	Mode             string            `json:"mode,omitempty"`
	NonGoals         []string          `json:"non_goals,omitempty"`
	SuccessCriteria  []string          `json:"success_criteria,omitempty"`
	ScopePaths       []string          `json:"scope_paths,omitempty"`
	ScopeTools       []string          `json:"scope_tools,omitempty"`
	ScopeLabels      []string          `json:"scope_labels,omitempty"`
	HumanGates       []LoopHumanGate   `json:"human_gates,omitempty"`
	Iteration        int               `json:"iteration,omitempty"`
	ModelCalls       int               `json:"model_calls,omitempty"`
	ToolCalls        int               `json:"tool_calls,omitempty"`
	InputTokens      int               `json:"input_tokens,omitempty"`
	OutputTokens     int               `json:"output_tokens,omitempty"`
	Attempts         int               `json:"attempts,omitempty"`
	StopReason       LoopStopReason    `json:"stop_reason,omitempty"`
	LastVerification *LoopVerification `json:"last_verification,omitempty"`
	Runs             []LoopRun         `json:"runs,omitempty"`
	Metadata         map[string]any    `json:"metadata,omitempty"`
	StartedAt        time.Time         `json:"started_at,omitempty"`
	UpdatedAt        time.Time         `json:"updated_at,omitempty"`
}

// Clone returns a deep copy of the loop context.
func (c *LoopContext) Clone() *LoopContext {
	if c == nil {
		return nil
	}
	cp := *c
	cp.NonGoals = append([]string(nil), c.NonGoals...)
	cp.SuccessCriteria = append([]string(nil), c.SuccessCriteria...)
	cp.ScopePaths = append([]string(nil), c.ScopePaths...)
	cp.ScopeTools = append([]string(nil), c.ScopeTools...)
	cp.ScopeLabels = append([]string(nil), c.ScopeLabels...)
	cp.HumanGates = make([]LoopHumanGate, 0, len(c.HumanGates))
	for _, gate := range c.HumanGates {
		gate.MatchPaths = append([]string(nil), gate.MatchPaths...)
		cp.HumanGates = append(cp.HumanGates, gate)
	}
	cp.LastVerification = c.LastVerification.Clone()
	cp.Runs = make([]LoopRun, 0, len(c.Runs))
	for _, run := range c.Runs {
		cp.Runs = append(cp.Runs, run.Clone())
	}
	cp.Metadata = utils.CloneAnyMap(c.Metadata)
	return &cp
}
