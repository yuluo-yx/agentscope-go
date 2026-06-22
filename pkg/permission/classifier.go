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

const (
	// AutoMaxConsecutiveDenials is the number of consecutive classifier denials after which
	// auto mode falls back to an interactive ask decision.
	AutoMaxConsecutiveDenials = 3
	// AutoMaxTotalDenials is the total classifier denial budget for one permission context.
	AutoMaxTotalDenials = 20
)

// AutoPermissionClassifier classifies one otherwise-interactive tool permission request.
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
	ConsecutiveDenials int `json:"consecutive_denials"`
	TotalDenials       int `json:"total_denials"`
}
