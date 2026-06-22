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
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/core"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
)

func (m *Runtime) verifyAfterReply(ctx context.Context, out chan<- message.Event, agent agentpkg.AgentAccessor, replyID string, fallback statepkg.LoopStopReason) statepkg.LoopStopReason {
	if m == nil || m.verifier == nil {
		return m.stopReasonOrFallback(agent, fallback)
	}
	m.emit(ctx, out, agent, EventVerifyStart, "", replyID)
	result, err := m.verifier.Verify(ctx, core.VerificationInput{
		AgentName: agentName(agent),
		SessionID: sessionID(agent),
		ReplyID:   replyID,
		Spec:      core.CloneSpec(m.spec),
		State:     agentState(agent),
	})
	if err != nil {
		result = core.VerificationResult{Passed: false, Reason: err.Error(), NextAction: "escalate"}
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

func (m *Runtime) recordVerification(
	agent agentpkg.AgentAccessor,
	replyID string,
	result core.VerificationResult,
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
