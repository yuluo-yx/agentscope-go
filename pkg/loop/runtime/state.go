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
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/core"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
)

func (m *Runtime) startRun(agent agentpkg.AgentAccessor, replyID string) {
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

func (m *Runtime) stopRun(agent agentpkg.AgentAccessor, reason statepkg.LoopStopReason) {
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

func (m *Runtime) beginReasoning(agent agentpkg.AgentAccessor) bool {
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

func (m *Runtime) exceededAgent(agent agentpkg.AgentAccessor) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.exceededLocked(ensureLoopContextLocked(agent, m.spec))
}

func (m *Runtime) exceededLocked(loopCtx *statepkg.LoopContext) bool {
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

func (m *Runtime) updateLoopContext(agent agentpkg.AgentAccessor, update func(*statepkg.LoopContext)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	update(ensureLoopContextLocked(agent, m.spec))
}

func (m *Runtime) recordCustomEvent(agent agentpkg.AgentAccessor, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	recordCustomEventLocked(ensureLoopContextLocked(agent, m.spec), name)
}

func (m *Runtime) stopReasonOrFallback(agent agentpkg.AgentAccessor, fallback statepkg.LoopStopReason) statepkg.LoopStopReason {
	m.mu.Lock()
	defer m.mu.Unlock()

	if stopReason := ensureLoopContextLocked(agent, m.spec).StopReason; stopReason != "" {
		return stopReason
	}
	return fallback
}

func ensureLoopContextLocked(agent agentpkg.AgentAccessor, spec core.Spec) *statepkg.LoopContext {
	state := agentState(agent)
	if state == nil {
		return &statepkg.LoopContext{Name: spec.Name, Goal: spec.Goal, Mode: string(spec.Mode)}
	}
	if state.LoopContext == nil {
		state.LoopContext = &statepkg.LoopContext{Name: spec.Name, Goal: spec.Goal, Mode: string(spec.Mode)}
	}
	return state.LoopContext
}

func stateHumanGates(gates []core.HumanGate) []statepkg.LoopHumanGate {
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
