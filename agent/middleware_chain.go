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

func (a *Agent) applyReplyHooks(ctx context.Context, input HookInput, final EventHandler) (<-chan message.Event, error) {

	handler := final
	for i := len(a.replyHooks) - 1; i >= 0; i-- {
		hook := a.replyHooks[i]
		next := handler
		handler = func(ctx context.Context) (<-chan message.Event, error) {
			return hook(ctx, a, input, next)
		}
	}

	return handler(ctx)
}

func (a *Agent) applyReasoningHooks(ctx context.Context, input HookInput, final EventHandler) (<-chan message.Event, error) {
	handler := final
	for i := len(a.reasoningHooks) - 1; i >= 0; i-- {
		hook := a.reasoningHooks[i]
		next := handler
		handler = func(ctx context.Context) (<-chan message.Event, error) {
			return hook(ctx, a, input, next)
		}
	}
	return handler(ctx)
}

func (a *Agent) applyActingHooks(ctx context.Context, input HookInput, final ToolHandler) (<-chan ToolChunk, error) {
	handler := final
	for i := len(a.actingHooks) - 1; i >= 0; i-- {
		hook := a.actingHooks[i]
		next := handler
		handler = func(ctx context.Context) (<-chan ToolChunk, error) {
			return hook(ctx, a, input, next)
		}
	}
	return handler(ctx)
}

func (a *Agent) applyModelCallHooks(ctx context.Context, input HookInput, final ModelCallHandler) (<-chan ChatResponse, error) {
	handler := final
	for i := len(a.modelCallHooks) - 1; i >= 0; i-- {
		hook := a.modelCallHooks[i]
		next := handler
		handler = func(ctx context.Context) (<-chan ChatResponse, error) {
			return hook(ctx, a, input, next)
		}
	}
	return handler(ctx)
}

func (a *Agent) applyCompressContextHooks(ctx context.Context, input HookInput, final CompressContextHandler) error {
	handler := final
	for i := len(a.compressContextHooks) - 1; i >= 0; i-- {
		hook := a.compressContextHooks[i]
		next := handler
		handler = func(ctx context.Context) error {
			return hook(ctx, a, input, next)
		}
	}
	return handler(ctx)
}
