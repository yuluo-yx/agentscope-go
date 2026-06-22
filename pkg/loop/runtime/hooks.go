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

package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

// OnSystemPrompt appends loop goals, success criteria, scope, and handoff rules.
func (m *Runtime) OnSystemPrompt(ctx context.Context, agent agentpkg.AgentAccessor, prompt string) (string, error) {
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
func (m *Runtime) OnReply(
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
func (m *Runtime) OnReasoning(
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
func (m *Runtime) OnModelCall(
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
func (m *Runtime) OnActing(
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
