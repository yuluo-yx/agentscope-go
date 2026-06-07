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

// Package middleware contains optional Agent middleware implementations.
package middleware

import (
	"context"
	"fmt"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
)

// TraceSpan is the minimal span contract used by TracingMiddleware.
type TraceSpan interface {
	// SetAttributes records additional span attributes.
	SetAttributes(map[string]any)
	// RecordError records an error observed by the middleware.
	RecordError(error)
	// End closes the span.
	End()
}

// Tracer starts tracing spans without binding core Agent code to a tracing SDK.
type Tracer interface {
	// StartSpan starts a span and may return a context carrying tracing state.
	StartSpan(context.Context, string, map[string]any) (context.Context, TraceSpan)
}

// TracingMiddleware records reply, model-call, and tool-execution spans.
type TracingMiddleware struct {
	tracer Tracer
}

// NewTracingMiddleware creates an Agent middleware backed by the provided tracer.
func NewTracingMiddleware(tracer Tracer) *TracingMiddleware {
	return &TracingMiddleware{tracer: tracer}
}

// MiddlewareName returns the middleware name.
func (*TracingMiddleware) MiddlewareName() string {
	return "tracing"
}

// OnReply records one span around a full Agent reply stream.
func (m *TracingMiddleware) OnReply(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	if m == nil || m.tracer == nil {
		return next(ctx)
	}
	name := "invoke_agent " + agent.AgentName()
	ctx, span := m.tracer.StartSpan(ctx, name, map[string]any{
		"gen_ai.operation.name":  "invoke_agent",
		"gen_ai.agent.name":      agent.AgentName(),
		"gen_ai.conversation.id": sessionID(agent),
		"agentscope.input.type":  inputType(input["input"]),
	})
	events, err := next(ctx)
	if err != nil {
		recordAndEnd(span, err)
		return nil, err
	}
	return wrapEvents(events, span), nil
}

// OnModelCall records one span around a streaming model call.
func (m *TracingMiddleware) OnModelCall(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ModelCallHandler,
) (<-chan modelpkg.ChatResponse, error) {
	if m == nil || m.tracer == nil {
		return next(ctx)
	}
	modelName := "unknown"
	if model, ok := input["model"].(modelpkg.ChatModel); ok && model != nil {
		modelName = model.Name()
	}
	request, _ := input["request"].(modelpkg.CallRequest)
	ctx, span := m.tracer.StartSpan(ctx, "chat "+modelName, map[string]any{
		"gen_ai.operation.name":    "chat",
		"gen_ai.request.model":     modelName,
		"gen_ai.conversation.id":   sessionID(agent),
		"agentscope.message.count": len(request.Messages),
		"agentscope.tool.count":    len(request.Tools),
	})
	responses, err := next(ctx)
	if err != nil {
		recordAndEnd(span, err)
		return nil, err
	}
	return wrapResponses(responses, span), nil
}

// OnActing records one span around local tool execution.
func (m *TracingMiddleware) OnActing(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ToolHandler,
) (<-chan agentpkg.ToolChunk, error) {
	if m == nil || m.tracer == nil {
		return next(ctx)
	}
	toolCall, _ := input["tool_call"].(*message.ToolCallBlock)
	toolName := "unknown"
	toolID := ""
	toolInput := ""
	if toolCall != nil {
		toolName = toolCall.Name
		toolID = toolCall.ID
		toolInput = toolCall.Input
	}
	ctx, span := m.tracer.StartSpan(ctx, "execute_tool "+toolName, map[string]any{
		"gen_ai.operation.name":      "execute_tool",
		"gen_ai.tool.name":           toolName,
		"gen_ai.tool.call.id":        toolID,
		"gen_ai.tool.call.arguments": toolInput,
		"gen_ai.conversation.id":     sessionID(agent),
	})
	chunks, err := next(ctx)
	if err != nil {
		recordAndEnd(span, err)
		return nil, err
	}
	return wrapToolChunks(chunks, span), nil
}

// OnCompressContext records one span around context compression.
func (m *TracingMiddleware) OnCompressContext(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.CompressContextHandler,
) error {
	if m == nil || m.tracer == nil {
		return next(ctx)
	}
	_ = input
	ctx, span := m.tracer.StartSpan(ctx, "compress_context "+agent.AgentName(), map[string]any{
		"gen_ai.operation.name":  "compress_context",
		"gen_ai.agent.name":      agent.AgentName(),
		"gen_ai.conversation.id": sessionID(agent),
	})
	err := next(ctx)
	if err != nil {
		recordAndEnd(span, err)
		return err
	}
	span.End()
	return nil
}

func wrapEvents(events <-chan message.Event, span TraceSpan) <-chan message.Event {
	if events == nil {
		recordAndEnd(span, fmt.Errorf("agentscope/middleware: nil event stream"))
		return nil
	}
	out := make(chan message.Event)
	go func() {
		defer close(out)
		defer span.End()
		for event := range events {
			if event != nil && event.GetType() == message.ReplyEndType {
				span.SetAttributes(map[string]any{"agentscope.reply.ended": true})
			}
			out <- event
		}
	}()
	return out
}

func wrapResponses(responses <-chan modelpkg.ChatResponse, span TraceSpan) <-chan modelpkg.ChatResponse {
	if responses == nil {
		recordAndEnd(span, fmt.Errorf("agentscope/middleware: nil model response stream"))
		return nil
	}
	out := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(out)
		defer span.End()
		var last *modelpkg.ChatResponse
		for response := range responses {
			clone := response.Clone()
			if clone != nil {
				last = clone
				if clone.Error != nil {
					span.RecordError(clone.Error)
				}
				out <- *clone
			}
		}
		if last != nil {
			attrs := map[string]any{"gen_ai.response.id": last.ID}
			if last.Usage != nil {
				attrs["gen_ai.usage.input_tokens"] = last.Usage.InputTokens
				attrs["gen_ai.usage.output_tokens"] = last.Usage.OutputTokens
			}
			span.SetAttributes(attrs)
		}
	}()
	return out
}

func wrapToolChunks(chunks <-chan agentpkg.ToolChunk, span TraceSpan) <-chan agentpkg.ToolChunk {
	if chunks == nil {
		recordAndEnd(span, fmt.Errorf("agentscope/middleware: nil tool chunk stream"))
		return nil
	}
	out := make(chan agentpkg.ToolChunk)
	go func() {
		defer close(out)
		defer span.End()
		var builder strings.Builder
		finalState := message.ToolResultRunning
		for chunk := range chunks {
			clone := chunk.Clone()
			if clone == nil {
				continue
			}
			if clone.State != "" {
				finalState = clone.State
			}
			if text := clone.Content.GetTextContent(""); text != nil {
				builder.WriteString(*text)
			}
			out <- *clone
		}
		span.SetAttributes(map[string]any{
			"gen_ai.tool.call.result": builder.String(),
			"agentscope.tool.state":   string(finalState),
		})
	}()
	return out
}

func recordAndEnd(span TraceSpan, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
	}
	span.End()
}

func sessionID(agent agentpkg.AgentAccessor) string {
	if agent == nil || agent.AgentState() == nil {
		return ""
	}
	return agent.AgentState().SessionID
}

func inputType(input any) string {
	if input == nil {
		return "nil"
	}
	return fmt.Sprintf("%T", input)
}
