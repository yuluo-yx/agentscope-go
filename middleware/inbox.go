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

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
)

// InboxItem is one asynchronous hint delivered to an Agent before reasoning.
type InboxItem struct {
	// Hint is converted to a HintBlock when Blocks is empty.
	Hint string
	// Source identifies the sender or subsystem that produced Hint.
	Source string
	// Blocks are appended to the current assistant message. Non-hint blocks are allowed so
	// applications can carry structured reminders while still preserving ReAct content order.
	Blocks message.ContentBlockList
}

// InboxSource drains pending inbox items for an Agent.
type InboxSource interface {
	DrainInbox(context.Context, agentpkg.AgentAccessor) ([]InboxItem, error)
}

// InboxMiddleware injects drained inbox items into the current assistant turn before reasoning.
type InboxMiddleware struct {
	source InboxSource
}

// NewInboxMiddleware creates middleware backed by an inbox source.
func NewInboxMiddleware(source InboxSource) *InboxMiddleware {
	return &InboxMiddleware{source: source}
}

// MiddlewareName returns the middleware name.
func (*InboxMiddleware) MiddlewareName() string {
	return "inbox"
}

// OnReasoning drains inbox items and appends them to the current assistant message before model input is prepared.
func (m *InboxMiddleware) OnReasoning(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	if m == nil || m.source == nil {
		return next(ctx)
	}
	items, err := m.source.DrainInbox(ctx, agent)
	if err != nil {
		return nil, err
	}
	blocks := inboxBlocks(items)
	if len(blocks) > 0 {
		if err := appendInboxBlocks(agent, blocks); err != nil {
			return nil, err
		}
		input["inbox_blocks"] = blocks.Clone()
	}
	return next(ctx)
}

func inboxBlocks(items []InboxItem) message.ContentBlockList {
	blocks := message.ContentBlockList{}
	for _, item := range items {
		if len(item.Blocks) > 0 {
			blocks = append(blocks, item.Blocks.Clone()...)
			continue
		}
		if item.Hint != "" {
			opts := []message.HintBlockOption{}
			if item.Source != "" {
				opts = append(opts, message.WithHintSource(item.Source))
			}
			blocks = append(blocks, message.NewHintBlock(item.Hint, opts...))
		}
	}
	return blocks
}

func appendInboxBlocks(agent agentpkg.AgentAccessor, blocks message.ContentBlockList) error {
	state := agent.AgentState()
	if state == nil {
		return nil
	}
	if len(state.Context) > 0 {
		last := state.Context[len(state.Context)-1]
		if last != nil && last.Role == message.RoleAssistant {
			last.Content = append(last.Content, blocks.Clone()...)
			return nil
		}
	}
	msg, err := message.NewAssistantMessage(agent.AgentName(), blocks.Clone())
	if err != nil {
		return err
	}
	state.Context = append(state.Context, msg)
	return nil
}
