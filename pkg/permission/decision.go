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

package permission

// Decision is the permission engine decision for one tool call.
type Decision struct {
	// Behavior is the decision outcome.
	Behavior Behavior `json:"behavior"`
	// Message is a human-readable description of the decision, displayed to the user.
	Message string `json:"message"`
	// DecisionReason explains why the decision was made, e.g., the matched rule or mode: "Rule: git:*".
	DecisionReason string `json:"decision_reason,omitempty"`
	// UpdatedInput is the revised tool input after an allow decision.
	UpdatedInput map[string]any `json:"updated_input,omitempty"`
	// SuggestedRules are candidate rules presented to the user during a prompt.
	// The user may select one to authorize, or deny the request entirely.
	SuggestedRules []Rule `json:"suggested_rules,omitempty"`
}

type PermissionDecision = Decision
