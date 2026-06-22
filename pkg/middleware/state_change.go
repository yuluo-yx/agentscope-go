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

package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

const (
	// StateUpdatedEvent is published when task, permission, or tool state changes during tool execution.
	StateUpdatedEvent = "state_updated"
	// TeamUpdatedEvent is published when a configured team tool finishes.
	TeamUpdatedEvent = "team_updated"
)

// ChangeEvent describes a state or team update detected after acting.
type ChangeEvent struct {
	Type       string
	AgentName  string
	SessionID  string
	ToolName   string
	ToolCallID string
	BeforeHash string
	AfterHash  string
}

// ChangeSink receives state/team change notifications.
type ChangeSink interface {
	PublishChange(context.Context, ChangeEvent) error
}

// StateChangeOption configures StateChangeMiddleware.
type StateChangeOption func(*StateChangeMiddleware)

// WithTeamToolNames marks tool names that should produce team_updated events.
func WithTeamToolNames(names ...string) StateChangeOption {
	return func(m *StateChangeMiddleware) {
		if m.teamTools == nil {
			m.teamTools = map[string]bool{}
		}
		for _, name := range names {
			if name != "" {
				m.teamTools[name] = true
			}
		}
	}
}

// StateChangeMiddleware publishes state/team change notifications after tool execution.
type StateChangeMiddleware struct {
	sink      ChangeSink
	teamTools map[string]bool
}

// NewStateChangeMiddleware creates state/team change middleware.
func NewStateChangeMiddleware(sink ChangeSink, opts ...StateChangeOption) *StateChangeMiddleware {
	m := &StateChangeMiddleware{sink: sink, teamTools: map[string]bool{}}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// MiddlewareName returns the middleware name.
func (*StateChangeMiddleware) MiddlewareName() string {
	return "state-change"
}

// OnActing snapshots Agent state before and after one tool execution and publishes changes.
func (m *StateChangeMiddleware) OnActing(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ToolHandler,
) (<-chan agentpkg.ToolChunk, error) {
	if m == nil || m.sink == nil {
		return next(ctx)
	}
	before := stateDigest(agent.AgentState())
	toolCall, _ := input["tool_call"].(*message.ToolCallBlock)
	chunks, err := next(ctx)
	if err != nil {
		m.publishChanges(ctx, agent, toolCall, before, stateDigest(agent.AgentState()))
		return nil, err
	}
	if chunks == nil {
		m.publishChanges(ctx, agent, toolCall, before, stateDigest(agent.AgentState()))
		return chunks, nil
	}
	out := make(chan agentpkg.ToolChunk)
	go func() {
		defer close(out)
		defer m.publishChanges(context.WithoutCancel(ctx), agent, toolCall, before, stateDigest(agent.AgentState()))
		for chunk := range chunks {
			clone := chunk.Clone()
			if clone == nil {
				continue
			}
			out <- *clone
		}
	}()
	return out, nil
}

func (m *StateChangeMiddleware) publishChanges(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	toolCall *message.ToolCallBlock,
	before string,
	after string,
) {
	toolName, toolCallID := toolCallInfo(toolCall)
	base := ChangeEvent{
		AgentName:  agent.AgentName(),
		SessionID:  sessionID(agent),
		ToolName:   toolName,
		ToolCallID: toolCallID,
		BeforeHash: before,
		AfterHash:  after,
	}
	if before != after {
		event := base
		event.Type = StateUpdatedEvent
		_ = m.sink.PublishChange(ctx, event)
	}
	if m.isTeamTool(toolCall) {
		event := base
		event.Type = TeamUpdatedEvent
		_ = m.sink.PublishChange(ctx, event)
	}
}

func (m *StateChangeMiddleware) isTeamTool(toolCall *message.ToolCallBlock) bool {
	if toolCall == nil {
		return false
	}
	if m.teamTools[toolCall.Name] {
		return true
	}
	if value, ok := toolCall.Extra["team_tool"].(bool); ok && value {
		return true
	}
	if value, ok := toolCall.Extra["agentscope.team_tool"].(bool); ok && value {
		return true
	}
	return false
}

func stateDigest(state *agentpkg.AgentState) string {
	if state == nil {
		return ""
	}
	payload := map[string]any{
		"permission_context": state.PermissionContext,
		"tool_context":       state.ToolContext,
		"task_context":       state.TaskContext,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func toolCallInfo(toolCall *message.ToolCallBlock) (string, string) {
	if toolCall == nil {
		return "", ""
	}
	return toolCall.Name, toolCall.ID
}
