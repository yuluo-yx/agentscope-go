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

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/tool"
	tasktool "github.com/yuluo-yx/agentscope-go/tool/task"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agent basic example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
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
		"Track work with task tools. Use TaskCreate when the user asks to track a task.",
		model,
		agentpkg.WithToolkit(kit),
	)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	user, err := message.NewUserMessage("user", "Use TaskCreate to track: write standalone examples for AgentScope Go.")
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}

	var replyText strings.Builder
	var seenEvents []string
	err = agent.ReplyStream(ctx, user, func(event message.Event) error {
		switch e := event.(type) {
		case *message.ToolCallStartEvent:
			seenEvents = append(seenEvents, "tool_call:"+e.ToolCallName)
		case *message.ToolResultEndEvent:
			seenEvents = append(seenEvents, "tool_result:"+string(e.State))
		case *message.TextBlockDeltaEvent:
			replyText.WriteString(e.Delta)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reply stream: %w", err)
	}

	fmt.Printf("agent_stream=%s tasks=%d events=%s\n", replyText.String(), len(agent.AgentState().TaskContext.Tasks), strings.Join(seenEvents, ","))
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
