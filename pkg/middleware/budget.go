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
	"fmt"
	"sync"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

const defaultReplyBudgetHint = "<system-reminder>You have reached the maximum token budget set by the user. Now you MUST wrap up immediately and provide a final concluding response without invoking any tools.</system-reminder>"

// ReplyBudgetOption configures ReplyBudgetControlMiddleware.
type ReplyBudgetOption func(*ReplyBudgetControlMiddleware)

// ReplyBudgetControlMiddleware enforces a weighted token budget per reply.
type ReplyBudgetControlMiddleware struct {
	tokenBudget       float64
	inputTokenWeight  float64
	outputTokenWeight float64
	hintMessage       string

	mu     sync.Mutex
	used   map[string]float64
	hinted map[string]bool
}

// NewReplyBudgetControlMiddleware creates reply-level budget middleware.
func NewReplyBudgetControlMiddleware(tokenBudget float64, opts ...ReplyBudgetOption) *ReplyBudgetControlMiddleware {
	m := &ReplyBudgetControlMiddleware{
		tokenBudget:       tokenBudget,
		inputTokenWeight:  1,
		outputTokenWeight: 1,
		hintMessage:       defaultReplyBudgetHint,
		used:              map[string]float64{},
		hinted:            map[string]bool{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m
}

// WithReplyBudgetWeights sets input/output token weights.
func WithReplyBudgetWeights(inputWeight, outputWeight float64) ReplyBudgetOption {
	return func(m *ReplyBudgetControlMiddleware) {
		m.inputTokenWeight = inputWeight
		m.outputTokenWeight = outputWeight
	}
}

// WithReplyBudgetHint sets the hint injected when the budget is exhausted.
func WithReplyBudgetHint(hint string) ReplyBudgetOption {
	return func(m *ReplyBudgetControlMiddleware) {
		m.hintMessage = hint
	}
}

// MiddlewareName returns the middleware name.
func (*ReplyBudgetControlMiddleware) MiddlewareName() string {
	return "reply-budget-control"
}

// OnReply tracks weighted token usage and clears budget state at reply end.
func (m *ReplyBudgetControlMiddleware) OnReply(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	_ = input
	if m == nil || m.tokenBudget <= 0 {
		return next(ctx)
	}
	events, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return nil, fmt.Errorf("agentscope/middleware: nil event stream")
	}
	out := make(chan message.Event)
	go func() {
		defer close(out)
		for event := range events {
			switch e := event.(type) {
			case *message.ReplyStartEvent:
				m.initReply(replyBudgetKey(agent, e.ReplyID()))
			case *message.ModelCallEndEvent:
				m.addUsage(replyBudgetKey(agent, e.ReplyID()), e.InputTokens, e.OutputTokens)
			case *message.ReplyEndEvent:
				m.clearReply(replyBudgetKey(agent, e.ReplyID()))
			}
			out <- event
		}
	}()
	return out, nil
}

// OnReasoning injects a wrap-up hint before the next reasoning step once the budget is exhausted.
func (m *ReplyBudgetControlMiddleware) OnReasoning(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	if m == nil || !m.exceeded(agent) {
		return next(ctx)
	}
	choice := &types.ToolChoice{Mode: string(types.ToolChoiceNone)}
	input["tool_choice"] = choice
	if m.markHinted(agent) {
		appendBudgetHint(agent, m.hintMessage)
	}
	return next(ctx)
}

// OnModelCall forces tool_choice=none when the reply budget is exhausted.
func (m *ReplyBudgetControlMiddleware) OnModelCall(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ModelCallHandler,
) (<-chan modelpkg.ChatResponse, error) {
	if m != nil && m.exceeded(agent) {
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

func (m *ReplyBudgetControlMiddleware) initReply(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.used[key] = 0
	m.hinted[key] = false
}

func (m *ReplyBudgetControlMiddleware) addUsage(key string, inputTokens, outputTokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.used[key] += m.inputTokenWeight*float64(inputTokens) + m.outputTokenWeight*float64(outputTokens)
}

func (m *ReplyBudgetControlMiddleware) clearReply(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.used, key)
	delete(m.hinted, key)
}

func (m *ReplyBudgetControlMiddleware) exceeded(agent agentpkg.AgentAccessor) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.used[replyBudgetKey(agent, "")] >= m.tokenBudget
}

func (m *ReplyBudgetControlMiddleware) markHinted(agent agentpkg.AgentAccessor) bool {
	key := replyBudgetKey(agent, "")
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hinted[key] {
		return false
	}
	m.hinted[key] = true
	return true
}

func replyBudgetKey(agent agentpkg.AgentAccessor, replyID string) string {
	sessionID := ""
	if agent != nil && agent.AgentState() != nil {
		state := agent.AgentState()
		sessionID = state.SessionID
		if replyID == "" {
			replyID = state.ReplyID
		}
	}
	return sessionID + ":" + replyID
}

func appendBudgetHint(agent agentpkg.AgentAccessor, hint string) {
	if hint == "" {
		return
	}
	_ = appendInboxBlocks(agent, message.ContentBlockList{
		message.NewHintBlock(hint, message.WithHintSource("reply-budget-control")),
	})
}

var (
	_ agentpkg.ReplyMiddleware     = (*ReplyBudgetControlMiddleware)(nil)
	_ agentpkg.ReasoningMiddleware = (*ReplyBudgetControlMiddleware)(nil)
	_ agentpkg.ModelCallMiddleware = (*ReplyBudgetControlMiddleware)(nil)
)
