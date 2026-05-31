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

package agent

import (
	"context"

	"github.com/yuluo-yx/agentscope-go/message"
)

// HookInput is the mutable input map passed to middleware hooks.
type HookInput map[string]any

// AgentAccessor is the minimal Agent surface visible to middleware.
type AgentAccessor interface {
	AgentName() string
	AgentState() *AgentState
}

// EventHandler represents the next reply or reasoning handler.
type EventHandler func(context.Context) (<-chan message.Event, error)

// ToolHandler represents the next tool execution handler.
type ToolHandler func(context.Context) (<-chan ToolChunk, error)

// ModelCallHandler represents the next model call handler.
type ModelCallHandler func(context.Context) (*ChatResponse, error)

// ReplyHook intercepts the full reply flow.
type ReplyHook func(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)

// ReasoningHook intercepts the reasoning and model-call event stream.
type ReasoningHook func(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)

// ActingHook intercepts one tool execution.
type ActingHook func(context.Context, AgentAccessor, HookInput, ToolHandler) (<-chan ToolChunk, error)

// ModelCallHook intercepts the raw model call.
type ModelCallHook func(context.Context, AgentAccessor, HookInput, ModelCallHandler) (*ChatResponse, error)

// SystemPromptHook transforms the system prompt in registration order.
type SystemPromptHook func(context.Context, AgentAccessor, string) (string, error)

// Middleware is the marker interface for all middleware implementations.
type Middleware interface {
	MiddlewareName() string
}

// ReplyMiddleware is implemented by middleware that intercepts full replies.
type ReplyMiddleware interface {
	OnReply(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)
}

// ReasoningMiddleware is implemented by middleware that intercepts reasoning.
type ReasoningMiddleware interface {
	OnReasoning(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)
}

// ActingMiddleware is implemented by middleware that intercepts tool execution.
type ActingMiddleware interface {
	OnActing(context.Context, AgentAccessor, HookInput, ToolHandler) (<-chan ToolChunk, error)
}

// ModelCallMiddleware is implemented by middleware that intercepts model calls.
type ModelCallMiddleware interface {
	OnModelCall(context.Context, AgentAccessor, HookInput, ModelCallHandler) (*ChatResponse, error)
}

// SystemPromptMiddleware is implemented by middleware that transforms system prompts.
type SystemPromptMiddleware interface {
	OnSystemPrompt(context.Context, AgentAccessor, string) (string, error)
}

// ApplySystemPromptHooks applies system-prompt hooks in registration order.
func ApplySystemPromptHooks(ctx context.Context, agent AgentAccessor, prompt string, hooks ...SystemPromptHook) (string, error) {
	current := prompt
	for _, hook := range hooks {
		next, err := hook(ctx, agent, current)
		if err != nil {
			return "", err
		}
		current = next
	}
	return current, nil
}
