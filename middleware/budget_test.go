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

package middleware_test

import (
	"context"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/middleware"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestReplyBudgetControlMiddlewareInjectsHintAndDisablesTools(t *testing.T) {
	t.Parallel()

	mw := middleware.NewReplyBudgetControlMiddleware(
		5,
		middleware.WithReplyBudgetHint("wrap up now"),
	)
	state := statepkg.NewAgentState()
	state.SessionID = "session-1"
	state.ReplyID = "reply-1"
	assistant, err := message.NewAssistantMessage("Friday", nil, message.WithMessageID("reply-1"))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	agent := fakeAgent{name: "Friday", state: state}

	replyEvents, err := mw.OnReply(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 2)
		out <- message.NewReplyStartEvent("session-1", "reply-1", "Friday")
		out <- message.NewModelCallEndEvent("reply-1", 3, 4)
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range replyEvents {
	}

	reasoningInput := agentpkg.HookInput{}
	reasoningEvents, err := mw.OnReasoning(context.Background(), agent, reasoningInput, func(context.Context) (<-chan message.Event, error) {
		choice, ok := reasoningInput["tool_choice"].(*types.ToolChoice)
		if !ok || choice.Mode != string(types.ToolChoiceNone) {
			t.Fatalf("reasoning hook should mark tool_choice none, got %#v", reasoningInput["tool_choice"])
		}
		out := make(chan message.Event)
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReasoning returned error: %v", err)
	}
	for range reasoningEvents {
	}
	hints := assistant.Content.GetContentBlocks("hint")
	if len(hints) != 1 || hints[0].(*message.HintBlock).Hint != "wrap up now" {
		t.Fatalf("budget hint was not injected: %#v", assistant.Content)
	}

	modelInput := agentpkg.HookInput{"request": modelpkg.CallRequest{}}
	responses, err := mw.OnModelCall(context.Background(), agent, modelInput, func(context.Context) (<-chan modelpkg.ChatResponse, error) {
		request := modelInput["request"].(modelpkg.CallRequest)
		if request.ToolChoice == nil || request.ToolChoice.Mode != string(types.ToolChoiceNone) {
			t.Fatalf("model request should force tool_choice none, got %#v", request.ToolChoice)
		}
		out := make(chan modelpkg.ChatResponse)
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnModelCall returned error: %v", err)
	}
	for range responses {
	}
}

func TestReplyBudgetControlMiddlewareCleansReplyState(t *testing.T) {
	t.Parallel()

	mw := middleware.NewReplyBudgetControlMiddleware(5)
	state := statepkg.NewAgentState()
	state.SessionID = "session-1"
	state.ReplyID = "reply-1"
	assistant, err := message.NewAssistantMessage("Friday", nil, message.WithMessageID("reply-1"))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	agent := fakeAgent{name: "Friday", state: state}

	events, err := mw.OnReply(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 3)
		out <- message.NewReplyStartEvent("session-1", "reply-1", "Friday")
		out <- message.NewModelCallEndEvent("reply-1", 10, 0)
		out <- message.NewReplyEndEvent("session-1", "reply-1")
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}

	modelInput := agentpkg.HookInput{"request": modelpkg.CallRequest{}}
	responses, err := mw.OnModelCall(context.Background(), agent, modelInput, func(context.Context) (<-chan modelpkg.ChatResponse, error) {
		request := modelInput["request"].(modelpkg.CallRequest)
		if request.ToolChoice != nil {
			t.Fatalf("budget state should be cleaned after reply end, got %#v", request.ToolChoice)
		}
		out := make(chan modelpkg.ChatResponse)
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnModelCall returned error: %v", err)
	}
	for range responses {
	}
}

func TestReplyBudgetControlMiddlewareWeightsAndFallbackBranches(t *testing.T) {
	t.Parallel()

	mw := middleware.NewReplyBudgetControlMiddleware(
		10,
		middleware.WithReplyBudgetWeights(2, 3),
		middleware.WithReplyBudgetHint(""),
	)
	if mw.MiddlewareName() != "reply-budget-control" {
		t.Fatalf("MiddlewareName mismatch: %q", mw.MiddlewareName())
	}
	state := statepkg.NewAgentState()
	state.SessionID = "session-weights"
	state.ReplyID = "reply-weights"
	agent := fakeAgent{name: "Friday", state: state}

	events, err := mw.OnReply(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 2)
		out <- message.NewReplyStartEvent("session-weights", "reply-weights", "Friday")
		out <- message.NewModelCallEndEvent("reply-weights", 2, 3)
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}

	reasoningInput := agentpkg.HookInput{}
	reasoningEvents, err := mw.OnReasoning(context.Background(), agent, reasoningInput, func(context.Context) (<-chan message.Event, error) {
		if reasoningInput["tool_choice"] == nil {
			t.Fatal("weighted budget should be exhausted")
		}
		out := make(chan message.Event)
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReasoning returned error: %v", err)
	}
	for range reasoningEvents {
	}
	if len(state.Context) != 0 {
		t.Fatalf("empty budget hint should not append context, got %#v", state.Context)
	}

	request := &modelpkg.CallRequest{}
	responses, err := mw.OnModelCall(context.Background(), agent, agentpkg.HookInput{"request": request}, func(context.Context) (<-chan modelpkg.ChatResponse, error) {
		if request.ToolChoice == nil || request.ToolChoice.Mode != string(types.ToolChoiceNone) {
			t.Fatalf("pointer request should be updated, got %#v", request.ToolChoice)
		}
		out := make(chan modelpkg.ChatResponse)
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnModelCall returned error: %v", err)
	}
	for range responses {
	}

	underBudget := middleware.NewReplyBudgetControlMiddleware(100)
	events, err = underBudget.OnReasoning(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event)
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("under-budget OnReasoning returned error: %v", err)
	}
	for range events {
	}
}
