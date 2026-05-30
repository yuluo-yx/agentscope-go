// Copyright 20\d\d AgentScope Go
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

package permission_test

import (
	"context"
	"testing"

	"github.com/yuluo-yx/agentscope-go/permission"
)

type fakeTool struct {
	name     string
	readOnly bool
	decision *permission.Decision
}

func (f fakeTool) Name() string {
	return f.name
}

func (f fakeTool) IsReadOnly() bool {
	return f.readOnly
}

func (f fakeTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	if f.decision != nil {
		return f.decision, nil
	}
	return &permission.Decision{Behavior: permission.BehaviorPassthrough, Message: "continue"}, nil
}

func (f fakeTool) MatchRule(ruleContent string, input map[string]any) bool {
	value, _ := input["command"].(string)
	if value == "" {
		value, _ = input["file_path"].(string)
	}
	return permission.MatchPattern(ruleContent, value)
}

func (f fakeTool) GenerateSuggestions(map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName:    f.name,
		RuleContent: "*",
		Behavior:    permission.BehaviorAllow,
		Source:      "test",
	}}
}

func TestEngineRulePriorityDenyAskAllow(t *testing.T) {
	t.Parallel()

	ctx := permission.NewContext(permission.ModeDefault)
	engine := permission.NewEngine(ctx)

	engine.AddRule(permission.Rule{ToolName: "Bash", RuleContent: "git:*", Behavior: permission.BehaviorAllow, Source: "test"})
	engine.AddRule(permission.Rule{ToolName: "Bash", RuleContent: "git:*", Behavior: permission.BehaviorAsk, Source: "test"})
	engine.AddRule(permission.Rule{ToolName: "Bash", RuleContent: "git:*", Behavior: permission.BehaviorDeny, Source: "test"})

	decision, err := engine.CheckPermission(context.Background(), fakeTool{name: "Bash"}, map[string]any{"command": "git status"})
	if err != nil {
		t.Fatalf("CheckPermission returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorDeny {
		t.Fatalf("deny rule should win, got %q", decision.Behavior)
	}
}

func TestEngineAllowAndAskRules(t *testing.T) {
	t.Parallel()

	t.Run("allow rule returns updated input", func(t *testing.T) {
		t.Parallel()

		engine := permission.NewEngine(permission.NewContext(permission.ModeDefault))
		engine.AddRule(permission.Rule{ToolName: "Bash", RuleContent: "go test:*", Behavior: permission.BehaviorAllow, Source: "test"})

		input := map[string]any{"command": "go test ./..."}
		decision, err := engine.CheckPermission(context.Background(), fakeTool{name: "Bash"}, input)
		if err != nil {
			t.Fatalf("CheckPermission returned error: %v", err)
		}
		if decision.Behavior != permission.BehaviorAllow {
			t.Fatalf("expected allow decision, got %q", decision.Behavior)
		}
		if decision.UpdatedInput["command"] != "go test ./..." {
			t.Fatalf("updated input not preserved: %#v", decision.UpdatedInput)
		}
	})

	t.Run("ask rule attaches suggestions", func(t *testing.T) {
		t.Parallel()

		engine := permission.NewEngine(permission.NewContext(permission.ModeDefault))
		engine.AddRule(permission.Rule{ToolName: "Bash", RuleContent: "npm:*", Behavior: permission.BehaviorAsk, Source: "test"})

		decision, err := engine.CheckPermission(context.Background(), fakeTool{name: "Bash"}, map[string]any{"command": "npm install"})
		if err != nil {
			t.Fatalf("CheckPermission returned error: %v", err)
		}
		if decision.Behavior != permission.BehaviorAsk || len(decision.SuggestedRules) != 1 {
			t.Fatalf("unexpected ask decision: %#v", decision)
		}
	})
}

func TestEngineModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  *permission.Context
		tool fakeTool
		want permission.Behavior
	}{
		{
			name: "bypass allows default passthrough",
			ctx:  permission.NewContext(permission.ModeBypass),
			tool: fakeTool{name: "Bash"},
			want: permission.BehaviorAllow,
		},
		{
			name: "dont ask converts default ask to deny",
			ctx:  permission.NewContext(permission.ModeDontAsk),
			tool: fakeTool{name: "Bash"},
			want: permission.BehaviorDeny,
		},
		{
			name: "explore allows read-only tools",
			ctx:  permission.NewContext(permission.ModeExplore),
			tool: fakeTool{name: "Read", readOnly: true},
			want: permission.BehaviorAllow,
		},
		{
			name: "explore denies write tools",
			ctx:  permission.NewContext(permission.ModeExplore),
			tool: fakeTool{name: "Write"},
			want: permission.BehaviorDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decision, err := permission.NewEngine(tt.ctx).CheckPermission(context.Background(), tt.tool, map[string]any{"command": "npm install"})
			if err != nil {
				t.Fatalf("CheckPermission returned error: %v", err)
			}
			if decision.Behavior != tt.want {
				t.Fatalf("unexpected behavior: want %q, got %q", tt.want, decision.Behavior)
			}
		})
	}
}

func TestEngineAskDecisionIncludesSuggestedRules(t *testing.T) {
	t.Parallel()

	decision, err := permission.NewEngine(permission.NewContext(permission.ModeDefault)).
		CheckPermission(context.Background(), fakeTool{name: "Bash"}, map[string]any{"command": "npm install"})
	if err != nil {
		t.Fatalf("CheckPermission returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAsk {
		t.Fatalf("expected ask decision, got %q", decision.Behavior)
	}
	if len(decision.SuggestedRules) != 1 {
		t.Fatalf("expected one suggested rule, got %d", len(decision.SuggestedRules))
	}
}

func TestEngineToolDecisionsAndInputDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tool     fakeTool
		input    map[string]any
		want     permission.Behavior
		wantHint string
	}{
		{
			name: "tool deny is returned before allow rules",
			tool: fakeTool{name: "Write", decision: &permission.Decision{
				Behavior:       permission.BehaviorDeny,
				Message:        "dangerous path",
				DecisionReason: "safety",
			}},
			want: permission.BehaviorDeny,
		},
		{
			name: "tool ask receives suggestions",
			tool: fakeTool{name: "Bash", decision: &permission.Decision{
				Behavior: permission.BehaviorAsk,
				Message:  "needs confirmation",
			}},
			want:     permission.BehaviorAsk,
			wantHint: "*",
		},
		{
			name: "nil input becomes an empty map",
			tool: fakeTool{name: "Bash"},
			want: permission.BehaviorAsk,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decision, err := permission.NewEngine(&permission.Context{}).CheckPermission(context.Background(), tt.tool, tt.input)
			if err != nil {
				t.Fatalf("CheckPermission returned error: %v", err)
			}
			if decision.Behavior != tt.want {
				t.Fatalf("unexpected behavior: want %q, got %q", tt.want, decision.Behavior)
			}
			if tt.wantHint != "" && decision.SuggestedRules[0].RuleContent != tt.wantHint {
				t.Fatalf("unexpected suggestions: %#v", decision.SuggestedRules)
			}
		})
	}

	if _, err := permission.NewEngine(nil).CheckPermission(context.Background(), nil, nil); err == nil {
		t.Fatal("nil tool should return an error")
	}
}

func TestMatchPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{pattern: "", value: "anything", want: true},
		{pattern: "git:*", value: "git", want: true},
		{pattern: "git:*", value: "git status", want: true},
		{pattern: "git:*", value: "go test", want: false},
		{pattern: "*.go", value: "main.go", want: true},
		{pattern: "/tmp/**", value: "/tmp/sub/file.txt", want: true},
		{pattern: "install", value: "npm install package", want: true},
		{pattern: "*.go", value: "README.md", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+" "+tt.value, func(t *testing.T) {
			t.Parallel()

			if got := permission.MatchPattern(tt.pattern, tt.value); got != tt.want {
				t.Fatalf("MatchPattern(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}
