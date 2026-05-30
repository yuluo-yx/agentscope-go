// Copyright 20\d\d AgentScope Go
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
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	agenterrors "github.com/yuluo-yx/agentscope-go/errors"
	"github.com/yuluo-yx/agentscope-go/message"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
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
