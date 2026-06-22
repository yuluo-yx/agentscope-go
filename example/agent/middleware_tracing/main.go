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

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/middleware"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
)

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
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agent middleware tracing example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	echo, err := tool.NewFunctionTool(
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
	)
	if err != nil {
		return fmt.Errorf("create echo tool: %w", err)
	}
	kit, err := tool.NewToolkit(echo)
	if err != nil {
		return fmt.Errorf("create toolkit: %w", err)
	}
	model, err := newDashScopeChatModel(true)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeExplore)
	tracer := &memoryTracer{}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Use the Echo tool once.",
		model,
		agentpkg.WithToolkit(kit),
		agentpkg.WithAgentState(state),
		agentpkg.WithMiddlewares(middleware.NewTracingMiddleware(tracer)),
	)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	user, err := message.NewUserMessage("user", "Use Echo with text Ada, then reply with the echoed value.")
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}
	reply, err := agent.Reply(ctx, user)
	if err != nil {
		return fmt.Errorf("agent reply: %w", err)
	}

	fmt.Printf(
		"reply=%q spans=%s tool_result=%q\n",
		textContent(reply),
		strings.Join(tracer.Names(), ","),
		lastToolResultText(state),
	)
	return nil
}

func lastToolResultText(state *asstate.AgentState) string {
	if state == nil {
		return ""
	}
	for i := len(state.Context) - 1; i >= 0; i-- {
		msg := state.Context[i]
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
