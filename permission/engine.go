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
	"encoding/json"
	"fmt"
)

// Tool is the minimal tool surface required by the permission engine.
type Tool interface {
	// Name returns the stable tool name used to match permission rules and diagnostic messages.
	Name() string
	// IsReadOnly reports whether this tool only reads state or external resources.
	// Explore mode allows read-only tools before consulting tool-specific checks.
	IsReadOnly() bool
	// CheckPermissions lets the tool make a tool-specific decision after global deny/ask rules
	// and before global allow/bypass defaults are applied. Implementations may return
	// BehaviorPassthrough to let the engine continue evaluating the remaining rule layers.
	CheckPermissions(context.Context, map[string]any, *Context) (*Decision, error)
	// MatchRule reports whether a stored rule value matches the current input map.
	// The rule value is supplied by permission.Rule.Input and should be interpreted by
	// the tool because each tool owns its input schema.
	MatchRule(string, map[string]any) bool
	// GenerateSuggestions returns candidate rules that would allow, deny, or ask for the
	// current input. The engine attaches these suggestions to ask decisions.
	GenerateSuggestions(map[string]any) []Rule
}

// InputReadOnlyTool is implemented by tools whose read-only status depends on
// the current input, such as Bash commands that may be read-only or mutating.
type InputReadOnlyTool interface {
	IsReadOnlyInput(map[string]any) bool
}

// Engine decides in deny, ask, tool check, allow, bypass, then default order.
type Engine struct {
	context        *Context
	autoClassifier AutoPermissionClassifier
	autoTranscript string
}

// EngineOption configures one permission engine instance.
type EngineOption func(*Engine)

// WithAutoPermissionClassifier enables AI classification for auto permission mode.
func WithAutoPermissionClassifier(classifier AutoPermissionClassifier) EngineOption {
	return func(e *Engine) {
		e.autoClassifier = classifier
	}
}

// WithAutoPermissionTranscript sets the sanitized transcript supplied to the auto classifier.
func WithAutoPermissionTranscript(transcript string) EngineOption {
	return func(e *Engine) {
		e.autoTranscript = transcript
	}
}

// NewEngine creates a permission engine.
func NewEngine(ctx *Context, opts ...EngineOption) *Engine {
	if ctx == nil {
		ctx = NewContext(ModeDefault)
	}
	ctx.ensureMaps()
	engine := &Engine{context: ctx}
	for _, opt := range opts {
		if opt != nil {
			opt(engine)
		}
	}
	return engine
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

	if decision := e.checkExploreModes(tool, input); decision != nil {
		return decision, nil
	}

	toolDecision, err := tool.CheckPermissions(ctx, input, e.context)
	if err != nil {
		return nil, err
	}
	if toolDecision != nil && toolDecision.Behavior != BehaviorPassthrough {
		if toolDecision.Behavior == BehaviorAsk {
			toolDecision.SuggestedRules = tool.GenerateSuggestions(input)
			return e.resolveAutoPermission(ctx, tool, input, toolDecision)
		}
		if toolDecision.Behavior == BehaviorAllow {
			e.resetAutoDenials()
		}
		return toolDecision, nil
	}

	if decision := e.checkRules(tool, input, e.context.AllowRules, BehaviorAllow); decision != nil {
		decision.UpdatedInput = input
		e.resetAutoDenials()
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
		return e.resolveAutoPermission(ctx, tool, input, decision)
	}
	return decision, nil
}

func (e *Engine) checkExploreModes(tool Tool, input map[string]any) *Decision {
	switch e.context.Mode {
	case ModeAuto:
		if isReadOnlyInvocation(tool, input) {
			return &Decision{
				Behavior:       BehaviorAllow,
				Message:        fmt.Sprintf("Permission granted for %s (auto mode - read-only invocation)", tool.Name()),
				DecisionReason: "Auto mode allows read-only operations",
			}
		}
	case ModeExplore:
		if isReadOnlyInvocation(tool, input) {
			return &Decision{
				Behavior:       BehaviorAllow,
				Message:        fmt.Sprintf("Permission granted for %s (explore mode - read-only invocation)", tool.Name()),
				DecisionReason: "Explore mode allows read-only operations",
			}
		}
		return &Decision{
			Behavior:       BehaviorDeny,
			Message:        fmt.Sprintf("Permission denied for %s (explore mode is read-only)", tool.Name()),
			DecisionReason: "Explore mode does not allow modifications",
		}
	case ModeAcceptEdits:
		if isReadOnlyInvocation(tool, input) {
			return &Decision{
				Behavior:       BehaviorAllow,
				Message:        fmt.Sprintf("Permission granted for %s (accept edits mode - read-only invocation)", tool.Name()),
				DecisionReason: "Accept edits mode allows read-only operations",
			}
		}
	}
	return nil
}

func isReadOnlyInvocation(tool Tool, input map[string]any) bool {
	if inputAware, ok := tool.(InputReadOnlyTool); ok && inputAware.IsReadOnlyInput(input) {
		return true
	}
	return tool.IsReadOnly()
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

func (e *Engine) resolveAutoPermission(ctx context.Context, tool Tool, input map[string]any, ask *Decision) (*Decision, error) {
	if e.context.Mode != ModeAuto || e.autoClassifier == nil {
		return ask, nil
	}
	if fastDecision, err := e.checkAcceptEditsFastPath(ctx, tool, input); err != nil {
		return nil, err
	} else if fastDecision != nil {
		return fastDecision, nil
	}

	request := e.classifierRequest(tool, input)
	decision, err := e.autoClassifier.Classify(ctx, request)
	if err != nil {
		return &Decision{
			Behavior:       BehaviorDeny,
			Message:        fmt.Sprintf("Permission denied for %s (auto classifier failed)", tool.Name()),
			DecisionReason: err.Error(),
		}, nil
	}
	if decision == nil {
		return &Decision{
			Behavior:       BehaviorDeny,
			Message:        fmt.Sprintf("Permission denied for %s (auto classifier returned no decision)", tool.Name()),
			DecisionReason: "Auto classifier returned nil decision",
		}, nil
	}
	return e.resolveAutoClassifierDecision(tool, input, ask, decision), nil
}

func (e *Engine) resolveAutoClassifierDecision(tool Tool, input map[string]any, ask, decision *Decision) *Decision {
	switch decision.Behavior {
	case BehaviorAllow:
		if decision.Message == "" {
			decision.Message = fmt.Sprintf("Permission granted for %s (auto classifier)", tool.Name())
		}
		if decision.DecisionReason == "" {
			decision.DecisionReason = "Auto classifier allowed the operation"
		}
		if decision.UpdatedInput == nil {
			decision.UpdatedInput = input
		}
		e.resetAutoDenials()
		return decision
	case BehaviorDeny:
		e.recordAutoDenial()
		if e.shouldFallbackToAsk() {
			ask.DecisionReason = "Auto classifier denial threshold reached"
			return ask
		}
		if decision.Message == "" {
			decision.Message = fmt.Sprintf("Permission denied for %s (auto classifier)", tool.Name())
		}
		if decision.DecisionReason == "" {
			decision.DecisionReason = "Auto classifier denied the operation"
		}
		return decision
	case BehaviorAsk:
		return ask
	default:
		return &Decision{
			Behavior:       BehaviorDeny,
			Message:        fmt.Sprintf("Permission denied for %s (auto classifier returned invalid behavior)", tool.Name()),
			DecisionReason: "Invalid auto classifier behavior: " + string(decision.Behavior),
		}
	}
}

func (e *Engine) checkAcceptEditsFastPath(ctx context.Context, tool Tool, input map[string]any) (*Decision, error) {
	acceptContext := e.context.Clone()
	acceptContext.Mode = ModeAcceptEdits
	decision, err := tool.CheckPermissions(ctx, input, acceptContext)
	if err != nil {
		return nil, err
	}
	if decision == nil || decision.Behavior == BehaviorPassthrough || decision.Behavior == BehaviorAsk {
		return nil, nil
	}
	if decision.Behavior == BehaviorAllow {
		if decision.Message == "" {
			decision.Message = fmt.Sprintf("Permission granted for %s (auto mode accept-edits fast path)", tool.Name())
		}
		if decision.DecisionReason == "" {
			decision.DecisionReason = "Accept edits mode allowed the operation before classifier escalation"
		}
		if decision.UpdatedInput == nil {
			decision.UpdatedInput = input
		}
		e.resetAutoDenials()
	}
	return decision, nil
}

func (e *Engine) classifierRequest(tool Tool, input map[string]any) ClassifierRequest {
	return ClassifierRequest{
		ToolName:           tool.Name(),
		ToolDescription:    toolDescription(tool),
		ToolReadOnly:       isReadOnlyInvocation(tool, input),
		Input:              cloneAnyMap(input),
		Action:             formatClassifierAction(tool.Name(), input),
		Transcript:         e.autoTranscript,
		WorkingDirectories: cloneWorkingDirectories(e.context.WorkingDirectories),
		AllowRules:         cloneRuleMap(e.context.AllowRules),
		DenyRules:          cloneRuleMap(e.context.DenyRules),
		AskRules:           cloneRuleMap(e.context.AskRules),
		DenialState:        e.context.AutoDenialState,
	}
}

func toolDescription(tool Tool) string {
	describer, ok := tool.(interface{ Description() string })
	if !ok {
		return ""
	}
	return describer.Description()
}

func formatClassifierAction(toolName string, input map[string]any) string {
	data, err := json.Marshal(map[string]any{
		"tool_name": toolName,
		"input":     input,
	})
	if err != nil {
		return fmt.Sprintf("%s(%v)", toolName, input)
	}
	return string(data)
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneWorkingDirectories(in map[string]AdditionalWorkingDirectory) map[string]AdditionalWorkingDirectory {
	if in == nil {
		return nil
	}
	out := make(map[string]AdditionalWorkingDirectory, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (e *Engine) recordAutoDenial() {
	e.context.AutoDenialState.ConsecutiveDenials++
	e.context.AutoDenialState.TotalDenials++
}

func (e *Engine) resetAutoDenials() {
	if e.context == nil || e.context.Mode != ModeAuto {
		return
	}
	e.context.AutoDenialState.ConsecutiveDenials = 0
}

func (e *Engine) shouldFallbackToAsk() bool {
	state := e.context.AutoDenialState
	return state.ConsecutiveDenials >= AutoMaxConsecutiveDenials || state.TotalDenials >= AutoMaxTotalDenials
}
