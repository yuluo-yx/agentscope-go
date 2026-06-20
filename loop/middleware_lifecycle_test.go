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
	"errors"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/types"
)

type hookAgentAccessor struct {
	name  string
	state *agentpkg.AgentState
}

func (a hookAgentAccessor) AgentName() string {
	return a.name
}

func (a hookAgentAccessor) AgentState() *agentpkg.AgentState {
	return a.state
}

func TestMiddlewareOptionsObserverAndHelpers(t *testing.T) {
	t.Parallel()

	if err := WithObserver(nil)(&options{}); err == nil || !strings.Contains(err.Error(), "observer is nil") {
		t.Fatalf("WithObserver nil error = %v", err)
	}
	opts := options{emitEvents: true}
	if err := WithEventEmission(false)(&opts); err != nil || opts.emitEvents {
		t.Fatalf("WithEventEmission(false) = %#v, %v", opts, err)
	}
	if err := (ObserverFunc(nil)).ObserveLoop(context.Background(), RunEvent{}); err != nil {
		t.Fatalf("nil ObserverFunc returned error: %v", err)
	}
	observed := []RunEvent{}
	observer := ObserverFunc(func(_ context.Context, event RunEvent) error {
		observed = append(observed, event)
		return errors.New("observer errors are ignored")
	})
	if err := observer.ObserveLoop(context.Background(), RunEvent{Type: EventStart}); err == nil {
		t.Fatalf("direct ObserverFunc should return callback error")
	}
	if len(observed) != 1 || observed[0].Type != EventStart {
		t.Fatalf("direct observer event mismatch: %#v", observed)
	}
	if result, err := (VerifierFunc(nil)).Verify(context.Background(), VerificationInput{}); err != nil || result.Passed {
		t.Fatalf("nil VerifierFunc = %#v, %v", result, err)
	}

	spec := Spec{
		Name:     "coverage-loop",
		Goal:     "cover internal helpers",
		NonGoals: []string{"do not run e2e"},
		SuccessCriteria: []SuccessCriterion{
			{Name: "tests", Description: "unit tests pass", Required: true},
			{Description: "coverage threshold"},
			{},
		},
		Scope: Scope{
			Paths:      []string{"loop"},
			ToolNames:  []string{"Bash"},
			TaskLabels: []string{"ci"},
			Metadata:   map[string]any{"component": "loop"},
		},
		Mode:   ModeAssisted,
		Policy: Policy{MaxIterations: 1, MaxModelCalls: 1, MaxToolCalls: 1, MaxInputTokens: 1, MaxOutputTokens: 1, MaxAttempts: 1, WrapUpHint: "summarize now"},
		HumanGates: []HumanGate{
			{Name: "security", Description: "security files", MatchPaths: []string{"secrets/**"}, Reason: "risk"},
			{},
		},
		Metadata: map[string]any{"risk": "low"},
	}
	var middlewareEvents []RunEvent
	middleware, err := NewMiddleware(spec, WithObserver(ObserverFunc(func(_ context.Context, event RunEvent) error {
		middlewareEvents = append(middlewareEvents, event)
		return errors.New("ignored")
	})))
	if err != nil {
		t.Fatalf("NewMiddleware returned error: %v", err)
	}
	if middleware.MiddlewareName() != "loop:coverage-loop" || (*Middleware)(nil).MiddlewareName() != "loop" {
		t.Fatalf("MiddlewareName mismatch")
	}
	state := agentpkg.NewAgentState()
	state.SessionID = "session-1"
	state.ReplyID = "reply-state"
	agent := hookAgentAccessor{name: "Friday", state: state}

	prompt, err := middleware.OnSystemPrompt(context.Background(), agent, "")
	if err != nil || !strings.Contains(prompt, "Loop Engineering") || !strings.Contains(prompt, "coverage-loop") {
		t.Fatalf("empty system prompt = %q, %v", prompt, err)
	}
	prompt, err = middleware.OnSystemPrompt(context.Background(), agent, "base")
	if err != nil || !strings.HasPrefix(prompt, "base\n\n<loop_engineering>") {
		t.Fatalf("combined system prompt = %q, %v", prompt, err)
	}
	nilPrompt, err := (*Middleware)(nil).OnSystemPrompt(context.Background(), agent, "base")
	if err != nil || nilPrompt != "base" {
		t.Fatalf("nil middleware prompt = %q, %v", nilPrompt, err)
	}

	middleware.startRun(agent, "reply-1")
	loopCtx := state.LoopContext
	if loopCtx == nil || loopCtx.Name != spec.Name || loopCtx.Metadata["risk"] != "low" ||
		len(loopCtx.SuccessCriteria) != 2 || len(loopCtx.HumanGates) != 2 {
		t.Fatalf("startRun loop context mismatch: %#v", loopCtx)
	}
	middleware.updateLoopContext(agent, func(ctx *statepkg.LoopContext) {
		ctx.ModelCalls = 1
		ctx.ToolCalls = 1
		ctx.InputTokens = 1
		ctx.OutputTokens = 1
	})
	if !middleware.exceededAgent(agent) {
		t.Fatalf("budget should be exceeded")
	}
	if !middleware.beginReasoning(agent) || state.LoopContext.StopReason != statepkg.LoopStopBudgetExceeded {
		t.Fatalf("beginReasoning should mark budget exhaustion: %#v", state.LoopContext)
	}
	if !middleware.markHinted(agent) || middleware.markHinted(agent) {
		t.Fatalf("markHinted should only return true once per reply")
	}
	if err := appendHint(agent, "wrap up"); err != nil {
		t.Fatalf("appendHint returned error: %v", err)
	}
	if len(state.Context) != 1 || len(state.Context[0].GetContentBlocks("hint")) != 1 {
		t.Fatalf("appendHint should add assistant hint message: %#v", state.Context)
	}
	if err := appendHint(hookAgentAccessor{name: "Friday"}, "ignored"); err != nil {
		t.Fatalf("appendHint without state returned error: %v", err)
	}

	out := make(chan message.Event, 1)
	middleware.emit(context.Background(), out, agent, EventStart, "started", "")
	event := (<-out).(*message.CustomEvent)
	if event.Name != EventStart || event.Value["reply_id"] != "reply-state" {
		t.Fatalf("custom event mismatch: %#v", event)
	}
	if len(middlewareEvents) == 0 || middlewareEvents[len(middlewareEvents)-1].Metrics.ModelCalls != 1 {
		t.Fatalf("observer should receive loop metrics: %#v", middlewareEvents)
	}
	middleware.recordCustomEvent(agent, EventWrapUp)
	middleware.stopRun(agent, "")
	lastRun := state.LoopContext.Runs[len(state.LoopContext.Runs)-1]
	if lastRun.StopReason != statepkg.LoopStopCompleted || len(lastRun.CustomEvents) == 0 {
		t.Fatalf("stopRun/custom events mismatch: %#v", lastRun)
	}

	if firstLoopContext(nil) != nil {
		t.Fatalf("firstLoopContext nil should return nil")
	}
	recordCustomEventLocked(nil, EventStart)
	recordCustomEventLocked(&statepkg.LoopContext{}, EventStart)
	if replyIDFromEventOrState(message.NewReplyEndEvent("session-1", "reply-event"), agent) != "reply-event" ||
		replyIDFromEventOrState(nil, agent) != "reply-state" ||
		replyIDFromEventOrState(nil, hookAgentAccessor{}) != "" {
		t.Fatalf("replyIDFromEventOrState mismatch")
	}
	if name, sessionID := agentInfo(nil); name != "" || sessionID != "" {
		t.Fatalf("nil agentInfo mismatch: %q %q", name, sessionID)
	}
	if agentState(nil) != nil || loopKey(nil) != ":" || cloneMap(nil) != nil {
		t.Fatalf("nil helpers mismatch")
	}
	if !middleware.exceededLocked(&statepkg.LoopContext{OutputTokens: 1}) || middleware.exceededLocked(nil) || (*Middleware)(nil).exceededLocked(&statepkg.LoopContext{OutputTokens: 1}) {
		t.Fatalf("exceededLocked nil/output-token branches mismatch")
	}
}

func TestMiddlewareHookErrorAndRequestBranches(t *testing.T) {
	t.Parallel()

	middleware, err := NewMiddleware(Spec{
		Name:   "hooks",
		Goal:   "cover hook branches",
		Mode:   ModeAssisted,
		Policy: Policy{MaxIterations: 1, MaxModelCalls: 1, MaxToolCalls: 1, MaxAttempts: 1, WrapUpHint: "wrap"},
	})
	if err != nil {
		t.Fatalf("NewMiddleware returned error: %v", err)
	}
	agentState := agentpkg.NewAgentState()
	agentState.SessionID = "session-2"
	agentState.ReplyID = "reply-2"
	agent := hookAgentAccessor{name: "Friday", state: agentState}

	nextErr := errors.New("next failed")
	if _, err := middleware.OnReply(context.Background(), agent, nil, func(context.Context) (<-chan message.Event, error) {
		return nil, nextErr
	}); !errors.Is(err, nextErr) {
		t.Fatalf("OnReply next error = %v", err)
	}
	if _, err := middleware.OnReply(context.Background(), agent, nil, func(context.Context) (<-chan message.Event, error) {
		return nil, nil
	}); err == nil || !strings.Contains(err.Error(), "nil event stream") {
		t.Fatalf("OnReply nil stream error = %v", err)
	}
	if _, err := middleware.OnReasoning(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		return nil, nil
	}); err == nil || !strings.Contains(err.Error(), "nil reasoning event stream") {
		t.Fatalf("OnReasoning nil stream error = %v", err)
	}

	replyEvents := make(chan message.Event, 5)
	replyEvents <- message.NewReplyStartEvent("session-2", "reply-open", "Friday")
	replyEvents <- message.NewModelCallEndEvent("reply-open", 3, 4)
	replyEvents <- message.NewToolResultStartEvent("reply-open", "call-1", "Bash")
	replyEvents <- message.NewRequireUserConfirmEvent("reply-open", []*message.ToolCallBlock{message.NewToolCallBlock("call-1", "Bash", "{}")})
	replyEvents <- message.NewRequireExternalExecutionEvent("reply-open", []*message.ToolCallBlock{message.NewToolCallBlock("call-2", "Deploy", "{}")})
	close(replyEvents)
	out, err := middleware.OnReply(context.Background(), agent, nil, func(context.Context) (<-chan message.Event, error) {
		return replyEvents, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range out {
	}
	if agentState.LoopContext.StopReason != statepkg.LoopStopWaitingExternal ||
		agentState.LoopContext.ModelCalls != 1 || agentState.LoopContext.ToolCalls != 1 {
		t.Fatalf("OnReply open stream state mismatch: %#v", agentState.LoopContext)
	}

	middleware.startRun(agent, "reply-budget")
	agentState.LoopContext.ModelCalls = 1
	reasoningInput := agentpkg.HookInput{}
	reasoningEvents := make(chan message.Event)
	close(reasoningEvents)
	reasoningOut, err := middleware.OnReasoning(context.Background(), agent, reasoningInput, func(context.Context) (<-chan message.Event, error) {
		return reasoningEvents, nil
	})
	if err != nil {
		t.Fatalf("OnReasoning returned error: %v", err)
	}
	var customNames []string
	for event := range reasoningOut {
		if custom, ok := event.(*message.CustomEvent); ok {
			customNames = append(customNames, custom.Name)
		}
	}
	if _, ok := reasoningInput["tool_choice"].(*types.ToolChoice); !ok {
		t.Fatalf("OnReasoning should force no-tool tool choice: %#v", reasoningInput)
	}
	if strings.Join(customNames, ",") != EventWrapUp+","+EventIterationStart+","+EventIterationEnd {
		t.Fatalf("reasoning custom events mismatch: %#v", customNames)
	}

	valueRequest := modelpkg.CallRequest{}
	valueInput := agentpkg.HookInput{"request": valueRequest}
	if _, err := middleware.OnModelCall(context.Background(), agent, valueInput, func(context.Context) (<-chan modelpkg.ChatResponse, error) {
		ch := make(chan modelpkg.ChatResponse)
		close(ch)
		return ch, nil
	}); err != nil {
		t.Fatalf("OnModelCall value returned error: %v", err)
	}
	if valueInput["request"].(modelpkg.CallRequest).ToolChoice == nil {
		t.Fatalf("OnModelCall should update value request tool choice")
	}
	ptrRequest := &modelpkg.CallRequest{}
	ptrInput := agentpkg.HookInput{"request": ptrRequest}
	if _, err := middleware.OnModelCall(context.Background(), agent, ptrInput, func(context.Context) (<-chan modelpkg.ChatResponse, error) {
		ch := make(chan modelpkg.ChatResponse)
		close(ch)
		return ch, nil
	}); err != nil {
		t.Fatalf("OnModelCall pointer returned error: %v", err)
	}
	if ptrRequest.ToolChoice == nil {
		t.Fatalf("OnModelCall should update pointer request tool choice")
	}
	actingCh := make(chan agentpkg.ToolChunk)
	close(actingCh)
	gotActing, err := middleware.OnActing(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan agentpkg.ToolChunk, error) {
		return actingCh, nil
	})
	if err != nil || gotActing != actingCh {
		t.Fatalf("OnActing should delegate directly: %#v, %v", gotActing, err)
	}
}
