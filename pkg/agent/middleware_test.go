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

package agent_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	agenterrors "github.com/yuluo-yx/agentscope-go/pkg/errors"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
)

type fakeAgentAccessor struct {
	name  string
	state *statepkg.AgentState
}

func (f fakeAgentAccessor) AgentName() string {
	return f.name
}

func (f fakeAgentAccessor) AgentState() *statepkg.AgentState {
	return f.state
}

func TestMiddlewareHookTypesWrapNextHandlers(t *testing.T) {
	t.Parallel()

	var hook agentpkg.ReasoningHook = func(
		ctx context.Context,
		agent agentpkg.AgentAccessor,
		input agentpkg.HookInput,
		next agentpkg.EventHandler,
	) (<-chan message.Event, error) {
		input["seen_by"] = agent.AgentName()
		return next(ctx)
	}

	next := func(context.Context) (<-chan message.Event, error) {
		ch := make(chan message.Event, 1)
		ch <- message.NewModelCallStartEvent("reply-1", "fake")
		close(ch)
		return ch, nil
	}

	input := agentpkg.HookInput{}
	events, err := hook(context.Background(), fakeAgentAccessor{name: "Friday", state: statepkg.NewAgentState()}, input, next)
	if err != nil {
		t.Fatalf("hook returned error: %v", err)
	}
	if input["seen_by"] != "Friday" {
		t.Fatalf("hook did not mutate input: %#v", input)
	}
	if evt := <-events; evt.GetType() != message.ModelCallStartType {
		t.Fatalf("unexpected event: %#v", evt)
	}
}

func TestSystemPromptHookTransformsSequentially(t *testing.T) {
	t.Parallel()

	hooks := []agentpkg.SystemPromptHook{
		func(context.Context, agentpkg.AgentAccessor, string) (string, error) { return "A", nil },
		func(context.Context, agentpkg.AgentAccessor, string) (string, error) { return "AB", nil },
	}

	got, err := agentpkg.ApplySystemPromptHooks(context.Background(), fakeAgentAccessor{name: "Friday"}, "start", hooks...)
	if err != nil {
		t.Fatalf("ApplySystemPromptHooks returned error: %v", err)
	}
	if got != "AB" {
		t.Fatalf("unexpected transformed prompt: %q", got)
	}
}

func TestSystemPromptHookStopsOnError(t *testing.T) {
	t.Parallel()

	hookErr := agenterrors.NewDeveloperError("bad prompt")
	_, err := agentpkg.ApplySystemPromptHooks(
		context.Background(),
		fakeAgentAccessor{name: "Friday"},
		"start",
		func(context.Context, agentpkg.AgentAccessor, string) (string, error) { return "", hookErr },
	)
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected hook error, got %v", err)
	}
}

func TestCompressContextHooksWrapStrategiesInOrder(t *testing.T) {
	t.Parallel()

	var calls []string
	agent, err := agentpkg.NewAgent(
		"Friday",
		"compress context",
		&scriptedChatModel{},
		agentpkg.WithContextStrategies(recordingContextStrategy{calls: &calls}),
		agentpkg.WithMiddlewares(
			compressHookMiddleware{name: "first", calls: &calls},
			compressHookMiddleware{name: "second", calls: &calls},
		),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}

	if err := agent.CompressContext(context.Background()); err != nil {
		t.Fatalf("CompressContext returned error: %v", err)
	}

	want := []string{"before:first", "before:second", "strategy", "after:second", "after:first"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("hook order mismatch:\nwant %#v\ngot  %#v", want, calls)
	}
}

func TestCompressContextHookPropagatesErrors(t *testing.T) {
	t.Parallel()

	hookErr := errors.New("compress hook failed")
	var calls []string
	agent, err := agentpkg.NewAgent(
		"Friday",
		"compress context",
		&scriptedChatModel{},
		agentpkg.WithContextStrategies(recordingContextStrategy{calls: &calls}),
		agentpkg.WithMiddlewares(
			compressHookMiddleware{name: "first", calls: &calls},
			compressHookMiddleware{name: "second", calls: &calls, err: hookErr},
		),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}

	err = agent.CompressContext(context.Background())
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected hook error, got %v", err)
	}
	want := []string{"before:first", "before:second", "after:first"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("hook error order mismatch:\nwant %#v\ngot  %#v", want, calls)
	}
}

func TestCompressContextHookRespectsCancellation(t *testing.T) {
	t.Parallel()

	var calls []string
	agent, err := agentpkg.NewAgent(
		"Friday",
		"compress context",
		&scriptedChatModel{},
		agentpkg.WithContextStrategies(recordingContextStrategy{calls: &calls}),
		agentpkg.WithMiddlewares(compressHookMiddleware{name: "only", calls: &calls}),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = agent.CompressContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	want := []string{"before:only", "after:only"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("cancel order mismatch:\nwant %#v\ngot  %#v", want, calls)
	}
}

func TestMiddlewareToolsJoinAgentReActToolProvider(t *testing.T) {
	t.Parallel()

	toolFromMiddleware, err := tool.NewFunctionTool(
		"MiddlewareEcho",
		"Echo a value supplied by middleware.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required":   []string{"value"},
		},
		func(_ context.Context, input map[string]any, _ *statepkg.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("middleware:" + input["value"].(string))}, nil
		},
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{Behavior: permission.BehaviorAllow, Message: "allowed in test"}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("call-mw", "MiddlewareEcho", `{"value":"hi"}`)},
			true,
		),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("done")}, true),
	}}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Use middleware tools.",
		model,
		agentpkg.WithMiddlewares(toolListMiddleware{tools: []agentpkg.Tool{toolFromMiddleware}}),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Echo hi from middleware")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	reply, err := agent.Reply(context.Background(), userMsg)
	if err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	if len(model.requests) != 2 {
		t.Fatalf("model should be called twice, got %d", len(model.requests))
	}
	if len(model.requests[0].Tools) != 1 || model.requests[0].Tools[0].Function.Name != "MiddlewareEcho" {
		t.Fatalf("middleware tool schema was not exposed to model: %#v", model.requests[0].Tools)
	}
	last := model.requests[1].Messages[len(model.requests[1].Messages)-1]
	results := last.GetContentBlocks("tool_result")
	if len(results) != 1 {
		t.Fatalf("second model call should include middleware tool result, got %#v", last.Content)
	}
	result := results[0].(*message.ToolResultBlock)
	if text := result.Output.Blocks.GetTextContent(""); result.State != message.ToolResultSuccess || text == nil || *text != "middleware:hi" {
		t.Fatalf("middleware tool result mismatch: %#v text=%#v", result, text)
	}
	if text := reply.GetTextContent(""); text == nil || *text != "done" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
}

type compressHookMiddleware struct {
	name  string
	calls *[]string
	err   error
}

func (m compressHookMiddleware) MiddlewareName() string {
	return "compress-" + m.name
}

func (m compressHookMiddleware) OnCompressContext(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.CompressContextHandler,
) error {
	_ = agent
	input["compress_hook"] = m.name
	if m.calls != nil {
		*m.calls = append(*m.calls, "before:"+m.name)
	}
	if m.err != nil {
		return m.err
	}
	err := next(ctx)
	if m.calls != nil {
		*m.calls = append(*m.calls, "after:"+m.name)
	}
	return err
}

type recordingContextStrategy struct {
	calls *[]string
}

func (s recordingContextStrategy) ContextStrategyName() string {
	return "recording"
}

func (s recordingContextStrategy) ApplyContextStrategy(ctx context.Context, _ *agentpkg.ContextStrategyInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.calls != nil {
		*s.calls = append(*s.calls, "strategy")
	}
	return nil
}

type toolListMiddleware struct {
	tools []agentpkg.Tool
}

func (m toolListMiddleware) MiddlewareName() string {
	return "tool-list"
}

func (m toolListMiddleware) ListTools(context.Context, agentpkg.AgentAccessor) ([]agentpkg.Tool, error) {
	return append([]agentpkg.Tool(nil), m.tools...), nil
}
