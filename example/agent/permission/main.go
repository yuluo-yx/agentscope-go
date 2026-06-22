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

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agent permission example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	executed := false
	write, err := tool.NewFunctionTool(
		"WriteThing",
		"Write one value after user confirmation.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			executed = true
			return message.ContentBlockList{message.NewTextBlock("written")}, nil
		},
		tool.WithFunctionSuggestedRule("WriteThing"),
	)
	if err != nil {
		return fmt.Errorf("create write tool: %w", err)
	}
	kit, err := tool.NewToolkit(write)
	if err != nil {
		return fmt.Errorf("create toolkit: %w", err)
	}
	model, err := newDashScopeChatModel(true)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}
	agent, err := agentpkg.NewAgent("Friday", "Ask before writes. Use WriteThing only after confirmation.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	user, err := message.NewUserMessage("user", "Call WriteThing to write the thing.")
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}

	var confirm *message.RequireUserConfirmEvent
	if err := agent.ReplyStream(ctx, user, func(event message.Event) error {
		if typed, ok := event.(*message.RequireUserConfirmEvent); ok {
			confirm = typed
		}
		return nil
	}); err != nil {
		return fmt.Errorf("reply stream before confirmation: %w", err)
	}
	if confirm == nil || len(confirm.ToolCalls) == 0 {
		return fmt.Errorf("expected a user confirmation event")
	}
	fmt.Printf("confirmation=required tool=%s suggestions=%d\n", confirm.ToolCalls[0].Name, len(confirm.ToolCalls[0].SuggestedRules))

	confirmEvent := message.NewUserConfirmResultEvent(confirm.ReplyID(), []message.ConfirmResult{{
		Confirmed: true,
		ToolCall:  confirm.ToolCalls[0],
		Rules:     confirm.ToolCalls[0].SuggestedRules,
	}})
	reply, err := agent.Reply(ctx, confirmEvent)
	if err != nil {
		return fmt.Errorf("reply after confirmation: %w", err)
	}
	replyText := ""
	if text := reply.GetTextContent(); text != nil {
		replyText = *text
	}
	fmt.Printf("confirmed_reply=%s executed=%t\n", replyText, executed)
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
