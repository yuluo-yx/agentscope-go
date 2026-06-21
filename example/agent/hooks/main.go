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
	"os"
	"strings"
	"sync"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/tool"
	tasktool "github.com/yuluo-yx/agentscope-go/tool/task"
)

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
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agent hooks example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	recorder := &auditMiddleware{}
	kit, err := tool.NewToolkit(tasktool.NewTaskCreate())
	if err != nil {
		return fmt.Errorf("create toolkit: %w", err)
	}
	model, err := newDashScopeChatModel(true)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Track one task with TaskCreate and reply with a short status.",
		model,
		agentpkg.WithToolkit(kit),
		agentpkg.WithMiddlewares(recorder),
	)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	user, err := message.NewUserMessage("user", "Use TaskCreate to track: document every Agent middleware hook.")
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}

	var reply strings.Builder
	if err := agent.ReplyStream(ctx, user, func(event message.Event) error {
		if delta, ok := event.(*message.TextBlockDeltaEvent); ok {
			reply.WriteString(delta.Delta)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("reply stream: %w", err)
	}

	fmt.Printf("reply=%s tasks=%d trace=%s\n",
		reply.String(),
		len(agent.AgentState().TaskContext.Tasks),
		strings.Join(recorder.Snapshot(), ","),
	)
	return nil
}

func newDashScopeChatModel(stream bool) (*dashscope.ChatModel, error) {
	apiKey := os.Getenv("AI_DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}
	return dashscope.NewChatModel(
		credential.NewDashScope(apiKey).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(stream),
	)
}
