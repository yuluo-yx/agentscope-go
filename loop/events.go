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
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/state"
)

const (
	EventStart          = "loop.start"
	EventIterationStart = "loop.iteration_start"
	EventIterationEnd   = "loop.iteration_end"
	EventVerifyStart    = "loop.verify_start"
	EventVerifyEnd      = "loop.verify_end"
	EventWrapUp         = "loop.wrap_up"
	EventStop           = "loop.stop"
)

func (m *Middleware) customEvent(agentName, sessionID, replyID, eventType, reason string, ctx *state.LoopContext) *message.CustomEvent {
	value := map[string]any{
		"agent_name": agentName,
		"session_id": sessionID,
		"reply_id":   replyID,
		"loop_name":  m.spec.Name,
		"mode":       string(m.spec.Mode),
		"reason":     reason,
	}
	if ctx != nil {
		value["iteration"] = ctx.Iteration
		value["model_calls"] = ctx.ModelCalls
		value["tool_calls"] = ctx.ToolCalls
		value["input_tokens"] = ctx.InputTokens
		value["output_tokens"] = ctx.OutputTokens
	}
	return message.NewCustomEvent(eventType, value)
}

func (m *Middleware) observe(ctx context.Context, eventType, reason, agentName, sessionID, replyID string, loopCtx *state.LoopContext) {
	if m == nil || m.observer == nil {
		return
	}
	metrics := Metrics{}
	if loopCtx != nil {
		metrics = Metrics{
			Iteration:    loopCtx.Iteration,
			ModelCalls:   loopCtx.ModelCalls,
			ToolCalls:    loopCtx.ToolCalls,
			InputTokens:  loopCtx.InputTokens,
			OutputTokens: loopCtx.OutputTokens,
		}
	}
	_ = m.observer.ObserveLoop(context.WithoutCancel(ctx), RunEvent{
		Type:      eventType,
		AgentName: agentName,
		SessionID: sessionID,
		ReplyID:   replyID,
		LoopName:  m.spec.Name,
		Mode:      m.spec.Mode,
		Reason:    reason,
		Metrics:   metrics,
		Time:      time.Now(),
	})
}
