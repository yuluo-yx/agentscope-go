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

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/middleware"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

type scriptedTracingModel struct {
	responses []*asmodel.ChatResponse
	requests  []asmodel.CallRequest
}

func (m *scriptedTracingModel) Name() string {
	return "scripted-tracing"
}

func (m *scriptedTracingModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return nil, fmt.Errorf("streaming model path is expected")
}

func (m *scriptedTracingModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted tracing model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		delta := response.Clone()
		delta.IsLast = false
		delta.Usage = nil
		select {
		case <-ctx.Done():
			return
		case out <- *delta:
		}
		select {
		case <-ctx.Done():
		case out <- *response.Clone():
		}
	}()
	return out, nil
}

func (m *scriptedTracingModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

type memoryTracer struct {
	mu    sync.Mutex
	spans []*memorySpan
}

func (t *memoryTracer) StartSpan(ctx context.Context, name string, attributes map[string]any) (context.Context, middleware.TraceSpan) {
	span := &memorySpan{name: name, attributes: cloneMap(attributes)}
	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()
	return ctx, span
}

func (t *memoryTracer) Names() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	names := make([]string, 0, len(t.spans))
	for _, span := range t.spans {
		names = append(names, span.name)
	}
	return names
}

type memorySpan struct {
	name       string
	attributes map[string]any
	err        error
	ended      bool
}

func (s *memorySpan) SetAttributes(attributes map[string]any) {
	for key, value := range attributes {
		s.attributes[key] = value
	}
}

func (s *memorySpan) RecordError(err error) {
	s.err = err
}

func (s *memorySpan) End() {
	s.ended = true
}

func main() {
	echo := mustTool(tool.NewFunctionTool(
		"Echo",
		"Echo one text value.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			text, _ := input["text"].(string)
			return message.ContentBlockList{message.NewTextBlock("echo " + text)}, nil
		},
		tool.WithFunctionReadOnly(true),
	))
	kit := mustToolkit(tool.NewToolkit(echo))
	model := &scriptedTracingModel{responses: []*asmodel.ChatResponse{
		asmodel.NewChatResponse(message.ContentBlockList{
			message.NewToolCallBlock("echo-call", "Echo", `{"text":"Ada"}`),
		}, true),
		asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("trace complete")}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeExplore)
	tracer := &memoryTracer{}
	agent := mustAgent(agentpkg.NewAgent(
		"Friday",
		"Use the Echo tool once.",
		model,
		agentpkg.WithToolkit(kit),
		agentpkg.WithAgentState(state),
		agentpkg.WithMiddlewares(middleware.NewTracingMiddleware(tracer)),
	))
	reply := mustReply(agent.Reply(context.Background(), mustMessage(message.NewUserMessage("user", "Echo Ada"))))

	fmt.Printf(
		"reply=%q spans=%s tool_result=%q\n",
		textContent(reply),
		strings.Join(tracer.Names(), ","),
		lastToolResultText(model),
	)
}

func lastToolResultText(model *scriptedTracingModel) string {
	if len(model.requests) == 0 {
		return ""
	}
	last := model.requests[len(model.requests)-1]
	for _, msg := range last.Messages {
		if msg == nil {
			continue
		}
		for _, block := range msg.GetContentBlocks("tool_result") {
			result, ok := block.(*message.ToolResultBlock)
			if !ok {
				continue
			}
			if text := result.Output.Blocks.GetTextContent(""); text != nil {
				return *text
			}
		}
	}
	return ""
}

func textContent(msg *message.Message) string {
	if text := msg.GetTextContent(""); text != nil {
		return *text
	}
	return ""
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mustTool(tool tool.Tool, err error) tool.Tool {
	if err != nil {
		panic(err)
	}
	return tool
}

func mustToolkit(toolkit *tool.Toolkit, err error) *tool.Toolkit {
	if err != nil {
		panic(err)
	}
	return toolkit
}

func mustAgent(agent *agentpkg.Agent, err error) *agentpkg.Agent {
	if err != nil {
		panic(err)
	}
	return agent
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}

func mustReply(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}
