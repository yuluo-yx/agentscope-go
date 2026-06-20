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

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/types"
)

// Option configures Loop Engineering middleware.
type Option func(*options) error

type options struct {
	verifier   Verifier
	observer   Observer
	emitEvents bool
}

// WithVerifier sets the verifier used after a reply run finishes.
func WithVerifier(verifier Verifier) Option {
	return func(opts *options) error {
		if verifier == nil {
			return fmt.Errorf("loop: verifier is nil")
		}
		opts.verifier = verifier
		return nil
	}
}

// WithObserver sets an observer for loop run events.
func WithObserver(observer Observer) Option {
	return func(opts *options) error {
		if observer == nil {
			return fmt.Errorf("loop: observer is nil")
		}
		opts.observer = observer
		return nil
	}
}

// WithEventEmission controls whether loop custom events are emitted into the Agent stream.
func WithEventEmission(enabled bool) Option {
	return func(opts *options) error {
		opts.emitEvents = enabled
		return nil
	}
}

// Middleware attaches Loop Engineering behavior to an Agent.
type Middleware struct {
	spec       Spec
	verifier   Verifier
	observer   Observer
	emitEvents bool

	mu     sync.Mutex
	hinted map[string]bool
}

// NewMiddleware creates Loop Engineering middleware.
func NewMiddleware(spec Spec, opts ...Option) (*Middleware, error) {
	spec = normalizeSpec(spec)
	if err := Validate(spec); err != nil {
		return nil, err
	}

	options := options{emitEvents: true}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&options); err != nil {
			return nil, err
		}
	}
	if spec.Mode == ModeUnattended && options.verifier == nil {
		return nil, fmt.Errorf("loop: unattended mode requires verifier")
	}

	return &Middleware{
		spec:       spec,
		verifier:   options.verifier,
		observer:   options.observer,
		emitEvents: options.emitEvents,
		hinted:     map[string]bool{},
	}, nil
}

// WithSpec installs Loop Engineering middleware on an Agent.
func WithSpec(spec Spec, opts ...Option) agentpkg.AgentOption {
	return func(agent *agentpkg.Agent) error {
		middleware, err := NewMiddleware(spec, opts...)
		if err != nil {
			return err
		}
		return agentpkg.WithMiddlewares(middleware)(agent)
	}
}

// MiddlewareName returns the middleware name.
func (m *Middleware) MiddlewareName() string {
	if m == nil || m.spec.Name == "" {
		return "loop"
	}
	return "loop:" + m.spec.Name
}

// OnSystemPrompt appends loop goals, success criteria, scope, and handoff rules.
func (m *Middleware) OnSystemPrompt(ctx context.Context, agent agentpkg.AgentAccessor, prompt string) (string, error) {
	_ = ctx
	_ = agent
	if m == nil {
		return prompt, nil
	}
	guidance := m.systemPrompt()
	if strings.TrimSpace(prompt) == "" {
		return guidance, nil
	}
	return strings.TrimSpace(prompt) + "\n\n" + guidance, nil
}

// OnReply tracks lifecycle events, metrics, verifier results, and final stop reason.
func (m *Middleware) OnReply(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	_ = input
	if m == nil {
		return next(ctx)
	}
	events, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return nil, fmt.Errorf("loop: nil event stream")
	}

	out := make(chan message.Event)
	go func() {
		defer close(out)
		replyID := ""
		stopReason := statepkg.LoopStopCompleted
		started := false
		for event := range events {
			replyID = replyIDFromEventOrState(event, agent)
			switch typed := event.(type) {
			case *message.ReplyStartEvent:
				replyID = typed.ReplyID()
				m.startRun(agent, replyID)
				started = true
				out <- event
				m.emit(ctx, out, agent, EventStart, "", replyID)
				continue
			case *message.ModelCallEndEvent:
				m.updateLoopContext(agent, func(loopCtx *statepkg.LoopContext) {
					loopCtx.ModelCalls++
					loopCtx.InputTokens += typed.InputTokens
					loopCtx.OutputTokens += typed.OutputTokens
					loopCtx.UpdatedAt = time.Now()
				})
			case *message.ToolResultStartEvent:
				m.updateLoopContext(agent, func(loopCtx *statepkg.LoopContext) {
					loopCtx.ToolCalls++
					loopCtx.UpdatedAt = time.Now()
				})
			case *message.RequireUserConfirmEvent:
				stopReason = statepkg.LoopStopWaitingUser
				m.updateLoopContext(agent, func(loopCtx *statepkg.LoopContext) {
					loopCtx.StopReason = stopReason
				})
			case *message.RequireExternalExecutionEvent:
				stopReason = statepkg.LoopStopWaitingExternal
				m.updateLoopContext(agent, func(loopCtx *statepkg.LoopContext) {
					loopCtx.StopReason = stopReason
				})
			case *message.ExceedMaxItersEvent:
				stopReason = statepkg.LoopStopMaxIterations
				m.updateLoopContext(agent, func(loopCtx *statepkg.LoopContext) {
					loopCtx.StopReason = stopReason
				})
			case *message.CustomEvent:
				m.recordCustomEvent(agent, typed.Name)
			}
			out <- event
			if _, ok := event.(*message.ReplyEndEvent); ok {
				stopReason = m.verifyAfterReply(ctx, out, agent, replyID, stopReason)
				m.stopRun(agent, stopReason)
				m.emit(ctx, out, agent, EventStop, string(stopReason), replyID)
				started = false
			}
		}
		if started {
			m.stopRun(agent, stopReason)
			m.emit(ctx, out, agent, EventStop, string(stopReason), replyID)
		}
	}()
	return out, nil
}

// OnReasoning emits iteration boundary events and injects wrap-up hints after budget exhaustion.
func (m *Middleware) OnReasoning(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	if m == nil {
		return next(ctx)
	}
	wrappedUp := false
	exceeded := m.beginReasoning(agent)
	if exceeded {
		input["tool_choice"] = &types.ToolChoice{Mode: string(types.ToolChoiceNone)}
		if m.markHinted(agent) {
			if err := appendHint(agent, m.spec.Policy.WrapUpHint); err != nil {
				return nil, err
			}
			wrappedUp = true
		}
	}
	events, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return nil, fmt.Errorf("loop: nil reasoning event stream")
	}
	out := make(chan message.Event)
	go func() {
		defer close(out)
		if wrappedUp {
			m.emit(ctx, out, agent, EventWrapUp, string(statepkg.LoopStopBudgetExceeded), "")
		}
		m.emit(ctx, out, agent, EventIterationStart, "", "")
		for event := range events {
			out <- event
		}
		m.emit(ctx, out, agent, EventIterationEnd, "", "")
	}()
	return out, nil
}

// OnModelCall forces no-tool wrap-up requests when a loop budget has been exhausted.
func (m *Middleware) OnModelCall(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ModelCallHandler,
) (<-chan modelpkg.ChatResponse, error) {
	if m != nil && m.exceededAgent(agent) {
		choice := &types.ToolChoice{Mode: string(types.ToolChoiceNone)}
		switch request := input["request"].(type) {
		case modelpkg.CallRequest:
			request.ToolChoice = choice
			input["request"] = request
		case *modelpkg.CallRequest:
			if request != nil {
				request.ToolChoice = choice
			}
		}
	}
	return next(ctx)
}

// OnActing participates in the Acting hook chain without replacing tool execution.
func (m *Middleware) OnActing(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ToolHandler,
) (<-chan agentpkg.ToolChunk, error) {
	_ = m
	_ = agent
	_ = input
	return next(ctx)
}

func (m *Middleware) systemPrompt() string {
	var builder strings.Builder
	builder.WriteString("<loop_engineering>\n")
	builder.WriteString("Loop Engineering controls for this agent run:\n")
	builder.WriteString("- Loop name: ")
	builder.WriteString(m.spec.Name)
	builder.WriteString("\n- Mode: ")
	builder.WriteString(string(m.spec.Mode))
	builder.WriteString("\n- Goal: ")
	builder.WriteString(m.spec.Goal)
	builder.WriteString("\n")
	appendStringList(&builder, "Non-goals", m.spec.NonGoals)
	criteria := make([]string, 0, len(m.spec.SuccessCriteria))
	for _, criterion := range m.spec.SuccessCriteria {
		if criterion.Name == "" && criterion.Description == "" {
			continue
		}
		item := criterion.Name
		if criterion.Description != "" {
			if item != "" {
				item += ": "
			}
			item += criterion.Description
		}
		if criterion.Required {
			item += " (required)"
		}
		criteria = append(criteria, item)
	}
	appendStringList(&builder, "Success criteria", criteria)
	appendStringList(&builder, "Scope paths", m.spec.Scope.Paths)
	appendStringList(&builder, "Allowed/expected tools", m.spec.Scope.ToolNames)
	gates := make([]string, 0, len(m.spec.HumanGates))
	for _, gate := range m.spec.HumanGates {
		if gate.Name == "" && gate.Description == "" && gate.Reason == "" {
			continue
		}
		item := gate.Name
		if gate.Description != "" {
			if item != "" {
				item += ": "
			}
			item += gate.Description
		}
		if gate.Reason != "" {
			item += " Reason: " + gate.Reason
		}
		gates = append(gates, item)
	}
	appendStringList(&builder, "Human gates", gates)
	builder.WriteString("If the goal is blocked, risky, or outside scope, stop and hand off with evidence.\n")
	builder.WriteString("</loop_engineering>")
	return builder.String()
}

func appendStringList(builder *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	builder.WriteString("- ")
	builder.WriteString(label)
	builder.WriteString(":\n")
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		builder.WriteString("  - ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
}

func (m *Middleware) emit(ctx context.Context, out chan<- message.Event, agent agentpkg.AgentAccessor, eventType, reason, replyID string, loopCtx ...*statepkg.LoopContext) {
	if m == nil || !m.emitEvents {
		return
	}
	agentName, sessionID := agentInfo(agent)
	event, snapshot, replyID := m.loopEvent(agent, agentName, sessionID, replyID, eventType, reason, firstLoopContext(loopCtx))
	m.observe(ctx, eventType, reason, agentName, sessionID, replyID, snapshot)
	out <- event
}

func firstLoopContext(contexts []*statepkg.LoopContext) *statepkg.LoopContext {
	if len(contexts) == 0 {
		return nil
	}
	return contexts[0]
}

func (m *Middleware) verifyAfterReply(ctx context.Context, out chan<- message.Event, agent agentpkg.AgentAccessor, replyID string, fallback statepkg.LoopStopReason) statepkg.LoopStopReason {
	if m == nil || m.verifier == nil {
		return m.stopReasonOrFallback(agent, fallback)
	}
	m.emit(ctx, out, agent, EventVerifyStart, "", replyID)
	result, err := m.verifier.Verify(ctx, VerificationInput{
		AgentName: agentName(agent),
		SessionID: sessionID(agent),
		ReplyID:   replyID,
		Spec:      m.spec.clone(),
		State:     agentState(agent),
	})
	if err != nil {
		result = VerificationResult{Passed: false, Reason: err.Error(), NextAction: "escalate"}
	}
	verifyEvent, snapshot, stopReason := m.recordVerification(agent, replyID, result)
	verifyEvent.Value["verification_passed"] = result.Passed
	verifyEvent.Value["verification_reason"] = result.Reason
	verifyEvent.Value["verification_evidence"] = append([]string(nil), result.Evidence...)
	verifyEvent.Value["verification_next_action"] = result.NextAction
	m.observe(ctx, EventVerifyEnd, result.Reason, agentName(agent), sessionID(agent), replyID, snapshot)
	out <- verifyEvent
	if stopReason != "" {
		return stopReason
	}
	return fallback
}

func (m *Middleware) startRun(agent agentpkg.AgentAccessor, replyID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	loopCtx := ensureLoopContextLocked(agent, m.spec)
	now := time.Now()
	loopCtx.Name = m.spec.Name
	loopCtx.Goal = m.spec.Goal
	loopCtx.Mode = string(m.spec.Mode)
	loopCtx.NonGoals = append([]string(nil), m.spec.NonGoals...)
	loopCtx.SuccessCriteria = successCriterionDescriptions(m.spec.SuccessCriteria)
	loopCtx.ScopePaths = append([]string(nil), m.spec.Scope.Paths...)
	loopCtx.ScopeTools = append([]string(nil), m.spec.Scope.ToolNames...)
	loopCtx.ScopeLabels = append([]string(nil), m.spec.Scope.TaskLabels...)
	loopCtx.HumanGates = stateHumanGates(m.spec.HumanGates)
	loopCtx.Metadata = cloneMap(m.spec.Metadata)
	loopCtx.Iteration = 0
	loopCtx.ModelCalls = 0
	loopCtx.ToolCalls = 0
	loopCtx.InputTokens = 0
	loopCtx.OutputTokens = 0
	loopCtx.StopReason = ""
	loopCtx.LastVerification = nil
	loopCtx.StartedAt = now
	loopCtx.UpdatedAt = now
	loopCtx.Runs = append(loopCtx.Runs, statepkg.LoopRun{ReplyID: replyID, StartedAt: now})
	m.clearHintLocked(agent)
}

func (m *Middleware) stopRun(agent agentpkg.AgentAccessor, reason statepkg.LoopStopReason) {
	m.mu.Lock()
	defer m.mu.Unlock()

	loopCtx := ensureLoopContextLocked(agent, m.spec)
	if reason == "" {
		reason = statepkg.LoopStopCompleted
	}
	loopCtx.StopReason = reason
	loopCtx.UpdatedAt = time.Now()
	if len(loopCtx.Runs) == 0 {
		return
	}
	run := &loopCtx.Runs[len(loopCtx.Runs)-1]
	run.FinishedAt = loopCtx.UpdatedAt
	run.Iterations = loopCtx.Iteration
	run.ModelCalls = loopCtx.ModelCalls
	run.ToolCalls = loopCtx.ToolCalls
	run.InputTokens = loopCtx.InputTokens
	run.OutputTokens = loopCtx.OutputTokens
	run.StopReason = reason
}

func (m *Middleware) beginReasoning(agent agentpkg.AgentAccessor) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	loopCtx := ensureLoopContextLocked(agent, m.spec)
	loopCtx.Iteration++
	loopCtx.UpdatedAt = time.Now()
	if !m.exceededLocked(loopCtx) {
		return false
	}
	loopCtx.StopReason = statepkg.LoopStopBudgetExceeded
	return true
}

func (m *Middleware) exceededAgent(agent agentpkg.AgentAccessor) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.exceededLocked(ensureLoopContextLocked(agent, m.spec))
}

func (m *Middleware) exceededLocked(loopCtx *statepkg.LoopContext) bool {
	if m == nil || loopCtx == nil {
		return false
	}
	policy := m.spec.Policy
	return (policy.MaxIterations > 0 && loopCtx.Iteration >= policy.MaxIterations) ||
		(policy.MaxModelCalls > 0 && loopCtx.ModelCalls >= policy.MaxModelCalls) ||
		(policy.MaxToolCalls > 0 && loopCtx.ToolCalls >= policy.MaxToolCalls) ||
		(policy.MaxInputTokens > 0 && loopCtx.InputTokens >= policy.MaxInputTokens) ||
		(policy.MaxOutputTokens > 0 && loopCtx.OutputTokens >= policy.MaxOutputTokens)
}

func (m *Middleware) markHinted(agent agentpkg.AgentAccessor) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := loopKey(agent)
	if m.hinted[key] {
		return false
	}
	m.hinted[key] = true
	return true
}

func (m *Middleware) clearHintLocked(agent agentpkg.AgentAccessor) {
	key := loopKey(agent)
	delete(m.hinted, key)
}

func loopKey(agent agentpkg.AgentAccessor) string {
	if agent == nil || agent.AgentState() == nil {
		return ":"
	}
	state := agent.AgentState()
	return state.SessionID + ":" + state.ReplyID
}

func (m *Middleware) updateLoopContext(agent agentpkg.AgentAccessor, update func(*statepkg.LoopContext)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	update(ensureLoopContextLocked(agent, m.spec))
}

func (m *Middleware) recordCustomEvent(agent agentpkg.AgentAccessor, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	recordCustomEventLocked(ensureLoopContextLocked(agent, m.spec), name)
}

func (m *Middleware) stopReasonOrFallback(agent agentpkg.AgentAccessor, fallback statepkg.LoopStopReason) statepkg.LoopStopReason {
	m.mu.Lock()
	defer m.mu.Unlock()

	if stopReason := ensureLoopContextLocked(agent, m.spec).StopReason; stopReason != "" {
		return stopReason
	}
	return fallback
}

func (m *Middleware) loopEvent(
	agent agentpkg.AgentAccessor,
	agentName string,
	sessionID string,
	replyID string,
	eventType string,
	reason string,
	loopCtx *statepkg.LoopContext,
) (*message.CustomEvent, *statepkg.LoopContext, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := loopCtx
	if current == nil {
		current = ensureLoopContextLocked(agent, m.spec)
	}
	if replyID == "" && agent != nil && agent.AgentState() != nil {
		replyID = agent.AgentState().ReplyID
	}
	event := m.customEvent(agentName, sessionID, replyID, eventType, reason, current)
	recordCustomEventLocked(current, event.Name)
	return event, current.Clone(), replyID
}

func (m *Middleware) recordVerification(
	agent agentpkg.AgentAccessor,
	replyID string,
	result VerificationResult,
) (*message.CustomEvent, *statepkg.LoopContext, statepkg.LoopStopReason) {
	m.mu.Lock()
	defer m.mu.Unlock()

	loopCtx := ensureLoopContextLocked(agent, m.spec)
	loopCtx.LastVerification = &statepkg.LoopVerification{
		Passed:     result.Passed,
		Reason:     result.Reason,
		Evidence:   append([]string(nil), result.Evidence...),
		NextAction: result.NextAction,
		UpdatedAt:  time.Now(),
	}
	if !result.Passed {
		loopCtx.StopReason = statepkg.LoopStopVerifierFailed
	}
	verifyEvent := m.customEvent(agentName(agent), sessionID(agent), replyID, EventVerifyEnd, result.Reason, loopCtx)
	recordCustomEventLocked(loopCtx, verifyEvent.Name)
	return verifyEvent, loopCtx.Clone(), loopCtx.StopReason
}

func ensureLoopContextLocked(agent agentpkg.AgentAccessor, spec Spec) *statepkg.LoopContext {
	state := agentState(agent)
	if state == nil {
		return &statepkg.LoopContext{Name: spec.Name, Goal: spec.Goal, Mode: string(spec.Mode)}
	}
	if state.LoopContext == nil {
		state.LoopContext = &statepkg.LoopContext{Name: spec.Name, Goal: spec.Goal, Mode: string(spec.Mode)}
	}
	return state.LoopContext
}

func appendHint(agent agentpkg.AgentAccessor, hint string) error {
	if strings.TrimSpace(hint) == "" || agent == nil || agent.AgentState() == nil {
		return nil
	}
	block := message.NewHintBlock(hint, message.WithHintSource("loop"))
	state := agent.AgentState()
	if len(state.Context) > 0 {
		last := state.Context[len(state.Context)-1]
		if last != nil && last.Role == message.RoleAssistant {
			last.Content = append(last.Content, block)
			return nil
		}
	}
	msg, err := message.NewAssistantMessage(agent.AgentName(), message.ContentBlockList{block})
	if err != nil {
		return err
	}
	state.Context = append(state.Context, msg)
	return nil
}

func recordCustomEventLocked(loopCtx *statepkg.LoopContext, name string) {
	if loopCtx == nil || name == "" || !strings.HasPrefix(name, "loop.") || len(loopCtx.Runs) == 0 {
		return
	}
	run := &loopCtx.Runs[len(loopCtx.Runs)-1]
	run.CustomEvents = append(run.CustomEvents, name)
}

func replyIDFromEventOrState(event message.Event, agent agentpkg.AgentAccessor) string {
	if event != nil && event.ReplyID() != "" {
		return event.ReplyID()
	}
	if agent != nil && agent.AgentState() != nil {
		return agent.AgentState().ReplyID
	}
	return ""
}

func agentInfo(agent agentpkg.AgentAccessor) (string, string) {
	return agentName(agent), sessionID(agent)
}

func agentName(agent agentpkg.AgentAccessor) string {
	if agent == nil {
		return ""
	}
	return agent.AgentName()
}

func sessionID(agent agentpkg.AgentAccessor) string {
	if agent == nil || agent.AgentState() == nil {
		return ""
	}
	return agent.AgentState().SessionID
}

func agentState(agent agentpkg.AgentAccessor) *statepkg.AgentState {
	if agent == nil {
		return nil
	}
	return agent.AgentState()
}

func successCriterionDescriptions(criteria []SuccessCriterion) []string {
	out := make([]string, 0, len(criteria))
	for _, criterion := range criteria {
		value := criterion.Name
		if criterion.Description != "" {
			if value != "" {
				value += ": "
			}
			value += criterion.Description
		}
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func stateHumanGates(gates []HumanGate) []statepkg.LoopHumanGate {
	out := make([]statepkg.LoopHumanGate, 0, len(gates))
	for _, gate := range gates {
		out = append(out, statepkg.LoopHumanGate{
			Name:        gate.Name,
			Description: gate.Description,
			MatchPaths:  append([]string(nil), gate.MatchPaths...),
			Reason:      gate.Reason,
		})
	}
	return out
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

var (
	_ agentpkg.ReplyMiddleware        = (*Middleware)(nil)
	_ agentpkg.ReasoningMiddleware    = (*Middleware)(nil)
	_ agentpkg.ActingMiddleware       = (*Middleware)(nil)
	_ agentpkg.ModelCallMiddleware    = (*Middleware)(nil)
	_ agentpkg.SystemPromptMiddleware = (*Middleware)(nil)
)
