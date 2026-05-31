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
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/tool"
	tasktool "github.com/yuluo-yx/agentscope-go/tool/task"
)

type scriptedChatModel struct {
	responses []*asmodel.ChatResponse
}

func (m *scriptedChatModel) Name() string {
	return "scripted-hooks"
}

func (m *scriptedChatModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return m.nextResponse()
}

func (m *scriptedChatModel) Stream(ctx context.Context, _ asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	response, err := m.nextResponse()
	if err != nil {
		return nil, err
	}
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
		case out <- *response:
		}
	}()
	return out, nil
}

func (m *scriptedChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *scriptedChatModel) nextResponse() (*asmodel.ChatResponse, error) {
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

type auditMiddleware struct {
	mu    sync.Mutex
	trace []string
}

func (m *auditMiddleware) MiddlewareName() string {
	return "audit-hooks"
}

func (m *auditMiddleware) OnReply(ctx context.Context, _ agentpkg.AgentAccessor, _ agentpkg.HookInput, next agentpkg.EventHandler) (<-chan message.Event, error) {
	m.record("reply:before")
	events, err := next(ctx)
	if err != nil {
		return nil, err
	}
	return m.wrapEvents(ctx, events, "reply:after"), nil
}

func (m *auditMiddleware) OnReasoning(ctx context.Context, _ agentpkg.AgentAccessor, _ agentpkg.HookInput, next agentpkg.EventHandler) (<-chan message.Event, error) {
	m.record("reasoning:before")
	events, err := next(ctx)
	if err != nil {
		return nil, err
	}
	return m.wrapEvents(ctx, events, "reasoning:after"), nil
}

func (m *auditMiddleware) OnActing(ctx context.Context, _ agentpkg.AgentAccessor, input agentpkg.HookInput, next agentpkg.ToolHandler) (<-chan agentpkg.ToolChunk, error) {
	if toolCall, ok := input["tool_call"].(*message.ToolCallBlock); ok && toolCall != nil {
		m.record("acting:" + toolCall.Name)
	}
	chunks, err := next(ctx)
	if err != nil {
		return nil, err
	}
	return m.wrapToolChunks(ctx, chunks, "acting:after"), nil
}

func (m *auditMiddleware) OnModelCall(ctx context.Context, _ agentpkg.AgentAccessor, input agentpkg.HookInput, next agentpkg.ModelCallHandler) (<-chan agentpkg.ChatResponse, error) {
	if request, ok := input["request"].(asmodel.CallRequest); ok {
		request.Metadata = map[string]any{"audit": "enabled"}
		input["request"] = request
	}
	m.record("model_call:before")
	responses, err := next(ctx)
	if err != nil {
		return nil, err
	}
	return m.wrapModelResponses(ctx, responses, "model_call:after"), nil
}

func (m *auditMiddleware) OnSystemPrompt(_ context.Context, agent agentpkg.AgentAccessor, prompt string) (string, error) {
	m.record("system_prompt:" + agent.AgentName())
	return prompt + "\nRecord hook activity in the audit middleware.", nil
}

func (m *auditMiddleware) Snapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.trace...)
}

func (m *auditMiddleware) record(entry string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trace = append(m.trace, entry)
}

func (m *auditMiddleware) wrapEvents(ctx context.Context, events <-chan message.Event, after string) <-chan message.Event {
	out := make(chan message.Event)
	go func() {
		defer close(out)
		defer m.record(after)
		for event := range events {
			select {
			case <-ctx.Done():
				return
			case out <- event:
			}
		}
	}()
	return out
}

func (m *auditMiddleware) wrapToolChunks(ctx context.Context, chunks <-chan agentpkg.ToolChunk, after string) <-chan agentpkg.ToolChunk {
	out := make(chan agentpkg.ToolChunk)
	go func() {
		defer close(out)
		defer m.record(after)
		for chunk := range chunks {
			select {
			case <-ctx.Done():
				return
			case out <- chunk:
			}
		}
	}()
	return out
}

func (m *auditMiddleware) wrapModelResponses(ctx context.Context, responses <-chan agentpkg.ChatResponse, after string) <-chan agentpkg.ChatResponse {
	out := make(chan agentpkg.ChatResponse)
	go func() {
		defer close(out)
		defer m.record(after)
		for response := range responses {
			select {
			case <-ctx.Done():
				return
			case out <- response:
			}
		}
	}()
	return out
}

func main() {
	recorder := &auditMiddleware{}
	kit := mustToolkit(tool.NewToolkit(tasktool.NewTaskCreate()))
	model := &scriptedChatModel{responses: []*asmodel.ChatResponse{
		asmodel.NewChatResponse(
			message.ContentBlockList{
				message.NewToolCallBlock("task-call", "TaskCreate", `{"subject":"Document hooks","description":"Show every Agent middleware hook."}`),
			},
			true,
		),
		asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("hooks observed")}, true),
	}}
	agent := mustAgent(agentpkg.NewAgent(
		"Friday",
		"Track one task and reply with a short status.",
		model,
		agentpkg.WithToolkit(kit),
		agentpkg.WithMiddlewares(recorder),
	))
	user := mustMessage(message.NewUserMessage("user", "Track the hook example task."))

	var reply strings.Builder
	if err := agent.ReplyStream(context.Background(), user, func(event message.Event) error {
		if delta, ok := event.(*message.TextBlockDeltaEvent); ok {
			reply.WriteString(delta.Delta)
		}
		return nil
	}); err != nil {
		panic(err)
	}

	fmt.Printf("reply=%s tasks=%d trace=%s\n",
		reply.String(),
		len(agent.AgentState().TaskContext.Tasks),
		strings.Join(recorder.Snapshot(), ","),
	)
}

func mustToolkit(kit *tool.Toolkit, err error) *tool.Toolkit {
	if err != nil {
		panic(err)
	}
	return kit
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
