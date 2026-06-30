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

import "context"

// Fail-safe mechanism: when the engine rejects three times consecutively
// or twenty times within a single conversation turn, the classifier is
// deemed unable to make a correct decision. The system automatically
// falls back to interactive inquiry mode, allowing the user to participate
// in the decision-making process and mitigating risks from misclassification.
const (
	// AutoMaxConsecutiveDenials is the number of consecutive classifier denials after which
	// auto mode falls back to an interactive ask decision.
	AutoMaxConsecutiveDenials = 3
	// AutoMaxTotalDenials is the total classifier denial budget for one permission context.
	AutoMaxTotalDenials = 20
)

// AutoPermissionClassifier is a user-defined classifier implementation for Auto mode,
// injected into the permission engine by the caller.
//
// When a tool call requires interactive permission confirmation, the engine delegates
// the decision to this classifier. An AI model determines whether to allow or deny
// the request, eliminating the need for user interaction.
//
// This interface does not provide a default implementation.
// Users are expected to supply their own implementation and inject it
// when creating the permission engine:
//
//	engine := permission.NewEngine(ctx,
//	    permission.WithAutoPermissionClassifier(myClassifier),
//	    permission.WithAutoPermissionTranscript(sanitizedTranscript),
//	)
type AutoPermissionClassifier interface {
	Classify(context.Context, ClassifierRequest) (*Decision, error)
}

// ClassifierRequest contains the classifier-facing tool action and sanitized transcript.
type ClassifierRequest struct {
	ToolName           string                                `json:"tool_name"`
	ToolDescription    string                                `json:"tool_description,omitempty"`
	ToolReadOnly       bool                                  `json:"tool_read_only"`
	Input              map[string]any                        `json:"input,omitempty"`
	Action             string                                `json:"action"`
	Transcript         string                                `json:"transcript,omitempty"`
	WorkingDirectories map[string]AdditionalWorkingDirectory `json:"working_directories,omitempty"`
	AllowRules         map[string][]Rule                     `json:"allow_rules,omitempty"`
	DenyRules          map[string][]Rule                     `json:"deny_rules,omitempty"`
	AskRules           map[string][]Rule                     `json:"ask_rules,omitempty"`
	DenialState        AutoDenialState                       `json:"denial_state"`
}

// AutoDenialState tracks classifier denials for safe fallback to interactive prompting.
type AutoDenialState struct {
	// ConsecutiveDenials tracks the number of consecutive classifier rejections.
	// Resets to zero on each allow.
	ConsecutiveDenials int `json:"consecutive_denials"`
	// TotalDenials tracks the cumulative rejection count over the entire context lifetime.
	// Never resets.
	TotalDenials int `json:"total_denials"`
}
