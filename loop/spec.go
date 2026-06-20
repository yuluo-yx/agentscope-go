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

package loop

import "github.com/yuluo-yx/agentscope-go/utils"

// Mode describes how much autonomy a loop is allowed to exercise.
type Mode string

const (
	ModeReportOnly Mode = "report_only"
	ModeAssisted   Mode = "assisted"
	ModeUnattended Mode = "unattended"
)

// SuccessCriterion is one explicit condition the loop should satisfy.
type SuccessCriterion struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Scope narrows the files, tools, tasks, or application-defined metadata the loop watches.
type Scope struct {
	Paths      []string       `json:"paths,omitempty"`
	ToolNames  []string       `json:"tool_names,omitempty"`
	TaskLabels []string       `json:"task_labels,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// HumanGate records a condition that requires human review before autonomous action.
type HumanGate struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	MatchPaths  []string `json:"match_paths,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

// Spec is the public loop design contract attached to an Agent.
type Spec struct {
	Name            string             `json:"name"`
	Goal            string             `json:"goal"`
	NonGoals        []string           `json:"non_goals,omitempty"`
	SuccessCriteria []SuccessCriterion `json:"success_criteria,omitempty"`
	Scope           Scope              `json:"scope,omitempty"`
	Mode            Mode               `json:"mode"`
	Policy          Policy             `json:"policy,omitempty"`
	HumanGates      []HumanGate        `json:"human_gates,omitempty"`
	Metadata        map[string]any     `json:"metadata,omitempty"`
}

func (s Spec) clone() Spec {
	cp := s
	cp.NonGoals = append([]string(nil), s.NonGoals...)
	cp.SuccessCriteria = append([]SuccessCriterion(nil), s.SuccessCriteria...)
	cp.Scope.Paths = append([]string(nil), s.Scope.Paths...)
	cp.Scope.ToolNames = append([]string(nil), s.Scope.ToolNames...)
	cp.Scope.TaskLabels = append([]string(nil), s.Scope.TaskLabels...)
	cp.Scope.Metadata = utils.CloneAnyMap(s.Scope.Metadata)
	cp.HumanGates = make([]HumanGate, 0, len(s.HumanGates))
	for _, gate := range s.HumanGates {
		gate.MatchPaths = append([]string(nil), gate.MatchPaths...)
		cp.HumanGates = append(cp.HumanGates, gate)
	}
	cp.Metadata = utils.CloneAnyMap(s.Metadata)
	return cp
}

func validMode(mode Mode) bool {
	switch mode {
	case ModeReportOnly, ModeAssisted, ModeUnattended:
		return true
	default:
		return false
	}
}
