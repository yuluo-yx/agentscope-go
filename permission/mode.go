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

// PermissionMode controls permission engine decisions on default paths.
type PermissionMode string

const (
	ModeDefault     PermissionMode = "default"
	ModeAcceptEdits PermissionMode = "accept_edits"
	ModeExplore     PermissionMode = "explore"
	ModeBypass      PermissionMode = "bypass"
	ModeDontAsk     PermissionMode = "dont_ask"
)

// Behavior represents the action chosen by a permission check.
type Behavior string

const (
	BehaviorAllow       Behavior = "allow"
	BehaviorDeny        Behavior = "deny"
	BehaviorAsk         Behavior = "ask"
	BehaviorPassthrough Behavior = "passthrough"
)

type PermissionBehavior = Behavior
