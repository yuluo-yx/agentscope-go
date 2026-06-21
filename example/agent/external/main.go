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

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agent external example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	deploy, err := tool.NewFunctionTool(
		"DeployJob",
		"Submit a deployment job to an external executor.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"service": map[string]any{"type": "string"}},
			"required":   []any{"service"},
		},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("this should run outside the process")}, nil
		},
		tool.WithFunctionExternalTool(true),
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{Behavior: permission.BehaviorAllow, Message: "external execution is allowed"}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("create deploy tool: %w", err)
	}
	kit, err := tool.NewToolkit(deploy)
	if err != nil {
		return fmt.Errorf("create toolkit: %w", err)
	}
	model, err := newDashScopeChatModel(true)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}
	agent, err := agentpkg.NewAgent("Friday", "Submit external work when needed. Use DeployJob for deployment requests.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	user, err := message.NewUserMessage("user", "Use DeployJob to deploy checkout.")
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}

	var required *message.RequireExternalExecutionEvent
	if err := agent.ReplyStream(ctx, user, func(event message.Event) error {
		if typed, ok := event.(*message.RequireExternalExecutionEvent); ok {
			required = typed
		}
		return nil
	}); err != nil {
		return fmt.Errorf("reply stream before external execution: %w", err)
	}
	if required == nil || len(required.ToolCalls) == 0 {
		return fmt.Errorf("expected an external execution event")
	}
	fmt.Printf("external=required tool=%s calls=%d\n", required.ToolCalls[0].Name, len(required.ToolCalls))

	result := message.NewToolResultBlock(
		required.ToolCalls[0].ID,
		required.ToolCalls[0].Name,
		message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("external executor finished checkout")}},
		message.ToolResultSuccess,
	)
	reply, err := agent.Reply(ctx, message.NewExternalExecutionResultEvent(required.ReplyID(), []*message.ToolResultBlock{result}))
	if err != nil {
		return fmt.Errorf("reply after external execution: %w", err)
	}
	replyText := ""
	if text := reply.GetTextContent(); text != nil {
		replyText = *text
	}
	fmt.Printf("external_reply=%s result_state=%s\n", replyText, result.State)
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
