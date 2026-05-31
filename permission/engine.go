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

import (
	"context"
	"fmt"
)

// Tool is the minimal tool surface required by the permission engine.
type Tool interface {
	Name() string
	IsReadOnly() bool
	CheckPermissions(context.Context, map[string]any, *Context) (*Decision, error)
	MatchRule(string, map[string]any) bool
	GenerateSuggestions(map[string]any) []Rule
}

// Engine decides in deny, ask, tool check, allow, bypass, then default order.
type Engine struct {
	context *Context
}

// NewEngine creates a permission engine.
func NewEngine(ctx *Context) *Engine {
	if ctx == nil {
		ctx = NewContext(ModeDefault)
	}
	ctx.ensureMaps()
	return &Engine{context: ctx}
}

// AddRule writes a rule into the behavior-specific rule group.
func (e *Engine) AddRule(rule Rule) {
	e.context.ensureMaps()
	switch rule.Behavior {
	case BehaviorAllow:
		e.context.AllowRules[rule.ToolName] = append(e.context.AllowRules[rule.ToolName], rule)
	case BehaviorDeny:
		e.context.DenyRules[rule.ToolName] = append(e.context.DenyRules[rule.ToolName], rule)
	case BehaviorAsk:
		e.context.AskRules[rule.ToolName] = append(e.context.AskRules[rule.ToolName], rule)
	}
}

// CheckPermission checks permissions for one tool call.
func (e *Engine) CheckPermission(ctx context.Context, tool Tool, input map[string]any) (*Decision, error) {
	if tool == nil {
		return nil, fmt.Errorf("permission: nil tool")
	}
	if input == nil {
		input = map[string]any{}
	}

	if decision := e.checkRules(tool, input, e.context.DenyRules, BehaviorDeny); decision != nil {
		return decision, nil
	}
	if decision := e.checkRules(tool, input, e.context.AskRules, BehaviorAsk); decision != nil {
		decision.SuggestedRules = tool.GenerateSuggestions(input)
		return decision, nil
	}

	if decision := e.checkExploreModes(tool); decision != nil {
		return decision, nil
	}

	toolDecision, err := tool.CheckPermissions(ctx, input, e.context)
	if err != nil {
		return nil, err
	}
	if toolDecision != nil && toolDecision.Behavior != BehaviorPassthrough {
		if toolDecision.Behavior == BehaviorAsk {
			toolDecision.SuggestedRules = tool.GenerateSuggestions(input)
		}
		return toolDecision, nil
	}

	if decision := e.checkRules(tool, input, e.context.AllowRules, BehaviorAllow); decision != nil {
		decision.UpdatedInput = input
		return decision, nil
	}

	if e.context.Mode == ModeBypass {
		return &Decision{
			Behavior:       BehaviorAllow,
			Message:        fmt.Sprintf("Permission granted for %s (bypass mode)", tool.Name()),
			DecisionReason: "Bypass mode allows all operations",
		}, nil
	}

	decision := e.defaultDecisionAsk(tool.Name())
	if decision.Behavior == BehaviorAsk {
		decision.SuggestedRules = tool.GenerateSuggestions(input)
	}
	return decision, nil
}

func (e *Engine) checkExploreModes(tool Tool) *Decision {
	switch e.context.Mode {
	case ModeExplore:
		if tool.IsReadOnly() {
			return &Decision{
				Behavior:       BehaviorAllow,
				Message:        fmt.Sprintf("Permission granted for %s (explore mode - read-only tool)", tool.Name()),
				DecisionReason: "Explore mode allows read-only operations",
			}
		}
		return &Decision{
			Behavior:       BehaviorDeny,
			Message:        fmt.Sprintf("Permission denied for %s (explore mode is read-only)", tool.Name()),
			DecisionReason: "Explore mode does not allow modifications",
		}
	case ModeAcceptEdits:
		if tool.IsReadOnly() {
			return &Decision{
				Behavior:       BehaviorAllow,
				Message:        fmt.Sprintf("Permission granted for %s (accept edits mode - read-only tool)", tool.Name()),
				DecisionReason: "Accept edits mode allows read-only operations",
			}
		}
	}
	return nil
}

func (e *Engine) checkRules(tool Tool, input map[string]any, rulesByTool map[string][]Rule, behavior Behavior) *Decision {
	for _, rule := range rulesByTool[tool.Name()] {
		if rule.RuleContent == "" || tool.MatchRule(rule.RuleContent, input) {
			switch behavior {
			case BehaviorDeny:
				return &Decision{
					Behavior:       BehaviorDeny,
					Message:        fmt.Sprintf("Permission to use %s has been denied", tool.Name()),
					DecisionReason: "Rule: " + rule.RuleContent,
				}
			case BehaviorAsk:
				return &Decision{
					Behavior:       BehaviorAsk,
					Message:        fmt.Sprintf("Permission required for %s", tool.Name()),
					DecisionReason: "Rule: " + rule.RuleContent,
				}
			case BehaviorAllow:
				return &Decision{
					Behavior: BehaviorAllow,
					Message:  fmt.Sprintf("Permission granted for %s", tool.Name()),
				}
			}
		}
	}
	return nil
}

func (e *Engine) defaultDecisionAsk(toolName string) *Decision {
	if e.context.Mode == ModeDontAsk {
		return &Decision{
			Behavior:       BehaviorDeny,
			Message:        fmt.Sprintf("Permission denied for %s (dont_ask mode - user not available)", toolName),
			DecisionReason: "User is not available to answer permission prompts",
		}
	}
	return &Decision{
		Behavior:       BehaviorAsk,
		Message:        fmt.Sprintf("Permission required for %s", toolName),
		DecisionReason: "Mode: " + string(e.context.Mode),
	}
}
