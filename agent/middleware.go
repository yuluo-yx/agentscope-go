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
	// AgentName returns the agent name associated with the current hook invocation.
	AgentName() string
	// AgentState returns the mutable state for advanced middleware that needs session, context, or task data.
	// Middleware should mutate state deliberately because hooks run inside the Agent reply lifecycle.
	AgentState() *AgentState
}

// EventHandler represents the next reply or reasoning handler.
type EventHandler func(context.Context) (<-chan message.Event, error)

// ToolHandler represents the next tool execution handler.
type ToolHandler func(context.Context) (<-chan ToolChunk, error)

// ModelCallHandler represents the next streaming model call handler.
type ModelCallHandler func(context.Context) (<-chan ChatResponse, error)

// CompressContextHandler represents the next context compression handler.
type CompressContextHandler func(context.Context) error

// ReplyHook intercepts the full reply flow.
type ReplyHook func(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)

// ReasoningHook intercepts the reasoning and model-call event stream.
type ReasoningHook func(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)

// ActingHook intercepts one tool execution.
type ActingHook func(context.Context, AgentAccessor, HookInput, ToolHandler) (<-chan ToolChunk, error)

// ModelCallHook intercepts the raw streaming model call.
type ModelCallHook func(context.Context, AgentAccessor, HookInput, ModelCallHandler) (<-chan ChatResponse, error)

// CompressContextHook intercepts one context compression pass.
type CompressContextHook func(context.Context, AgentAccessor, HookInput, CompressContextHandler) error

// SystemPromptHook transforms the system prompt in registration order.
type SystemPromptHook func(context.Context, AgentAccessor, string) (string, error)

// Middleware is the marker interface for all middleware implementations.
type Middleware interface {
	// MiddlewareName returns a stable middleware name for logs and diagnostics.
	MiddlewareName() string
}

// ReplyMiddleware is implemented by middleware that intercepts full replies.
type ReplyMiddleware interface {
	// OnReply wraps a full Agent reply, including input handling, reasoning, acting, and reply-end events.
	// Implementations may inspect input["input"], wrap the returned event stream, or call next with a derived context.
	OnReply(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)
}

// ReasoningMiddleware is implemented by middleware that intercepts reasoning.
type ReasoningMiddleware interface {
	// OnReasoning wraps one reasoning pass that prepares model input and emits model-derived events.
	// Implementations may observe or transform the event stream but should preserve event order.
	OnReasoning(context.Context, AgentAccessor, HookInput, EventHandler) (<-chan message.Event, error)
}

// ActingMiddleware is implemented by middleware that intercepts tool execution.
type ActingMiddleware interface {
	// OnActing wraps one local tool execution. input["tool_call"] contains the ToolCallBlock being executed.
	// Implementations can time, audit, or replace tool chunks before they become ToolResult events.
	OnActing(context.Context, AgentAccessor, HookInput, ToolHandler) (<-chan ToolChunk, error)
}

// ModelCallMiddleware is implemented by middleware that intercepts model calls.
type ModelCallMiddleware interface {
	// OnModelCall wraps the raw ChatModel.Stream call. input["model"] contains the selected ChatModel and
	// input["request"] contains a CallRequest clone; middleware may replace either before calling next.
	OnModelCall(context.Context, AgentAccessor, HookInput, ModelCallHandler) (<-chan ChatResponse, error)
}

// CompressContextMiddleware is implemented by middleware that intercepts context compression.
type CompressContextMiddleware interface {
	// OnCompressContext wraps one Agent context compression pass.
	// Implementations should call next(ctx) unless they intentionally replace compression behavior.
	OnCompressContext(context.Context, AgentAccessor, HookInput, CompressContextHandler) error
}

// SystemPromptMiddleware is implemented by middleware that transforms system prompts.
type SystemPromptMiddleware interface {
	// OnSystemPrompt receives the current system prompt and returns the prompt passed to the next hook or model request.
	OnSystemPrompt(context.Context, AgentAccessor, string) (string, error)
}

// ToolMiddleware is implemented by middleware that contributes static tools to the Agent.
// Tools are collected while WithMiddlewares is applied and then participate in the same
// schema, permission, and execution path as the Agent toolkit.
type ToolMiddleware interface {
	// ListTools returns tools exposed by this middleware.
	ListTools(context.Context, AgentAccessor) ([]Tool, error)
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
