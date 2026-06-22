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

import "testing"

func TestContextCloneInitializesDefaultsAndDeepCopiesRules(t *testing.T) {
	t.Parallel()

	if clone := (*Context)(nil).Clone(); clone != nil {
		t.Fatalf("nil context clone should be nil: %#v", clone)
	}

	ctx := &Context{
		WorkingDirectories: map[string]AdditionalWorkingDirectory{
			"/repo": {Path: "/repo", Source: "test"},
		},
		AllowRules: map[string][]Rule{
			"Bash": {{ToolName: "Bash", RuleContent: "ls", Behavior: BehaviorAllow}},
		},
		DenyRules: map[string][]Rule{
			"Bash": {{ToolName: "Bash", RuleContent: "rm", Behavior: BehaviorDeny}},
		},
	}
	clone := ctx.Clone()
	if clone.Mode != ModeDefault {
		t.Fatalf("empty mode should default during clone: %q", clone.Mode)
	}
	if clone.WorkingDirectories["/repo"].Source != "test" || clone.AllowRules["Bash"][0].RuleContent != "ls" {
		t.Fatalf("clone content mismatch: %#v", clone)
	}
	if clone.AskRules == nil || clone.DenyRules == nil || clone.AllowRules == nil || clone.WorkingDirectories == nil {
		t.Fatalf("clone should initialize all maps: %#v", clone)
	}

	ctx.WorkingDirectories["/repo"] = AdditionalWorkingDirectory{Path: "/repo", Source: "mutated"}
	ctx.AllowRules["Bash"][0].RuleContent = "mutated"
	if clone.WorkingDirectories["/repo"].Source != "test" || clone.AllowRules["Bash"][0].RuleContent != "ls" {
		t.Fatalf("clone should be isolated from source mutation: %#v", clone)
	}

	created := NewContext("")
	if created.Mode != ModeDefault || created.AllowRules == nil || created.AskRules == nil || created.DenyRules == nil || created.WorkingDirectories == nil {
		t.Fatalf("NewContext should default mode and initialize maps: %#v", created)
	}
}
