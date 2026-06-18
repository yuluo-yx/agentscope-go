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

// ContextStatusLevel describes the current model context pressure level.
type ContextStatusLevel string

const (
	ContextStatusNormal   ContextStatusLevel = "normal"
	ContextStatusWarning  ContextStatusLevel = "warning"
	ContextStatusCompact  ContextStatusLevel = "compact"
	ContextStatusBlocking ContextStatusLevel = "blocking"
)

// ContextStatus records the latest context-window pressure computed by an Agent.
type ContextStatus struct {
	Level             ContextStatusLevel `json:"level"`
	Strategy          string             `json:"strategy"`
	MaxTokens         int                `json:"max_tokens"`
	UsedTokens        int                `json:"used_tokens"`
	RemainingTokens   int                `json:"remaining_tokens"`
	WarningThreshold  int                `json:"warning_threshold"`
	CompactThreshold  int                `json:"compact_threshold"`
	BlockingThreshold int                `json:"blocking_threshold"`
	Message           string             `json:"message,omitempty"`
}

// Clone returns a deep copy of the context status.
func (s *ContextStatus) Clone() *ContextStatus {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}
