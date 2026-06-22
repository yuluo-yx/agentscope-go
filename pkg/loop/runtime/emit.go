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
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
)

func (m *Runtime) emit(ctx context.Context, out chan<- message.Event, agent agentpkg.AgentAccessor, eventType, reason, replyID string, loopCtx ...*statepkg.LoopContext) {
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

func (m *Runtime) loopEvent(
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
