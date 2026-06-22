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

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/core"
	loopruntime "github.com/yuluo-yx/agentscope-go/pkg/loop/runtime"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "loop basic example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	spec := core.Spec{
		Name: "daily-triage",
		Goal: "produce a report-only triage summary without modifying external systems",
		NonGoals: []string{
			"create pull requests",
			"merge code",
		},
		SuccessCriteria: []core.SuccessCriterion{
			{Name: "summary", Description: "final reply lists findings and next action", Required: true},
		},
		Mode:   core.ModeReportOnly,
		Policy: core.DefaultPolicy(core.ModeReportOnly),
	}
	model, err := newDashScopeChatModel(false)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}
	agent, err := agentpkg.NewAgent("Friday", "You are concise.", model, loopruntime.WithSpec(spec))
	if err != nil {
		return err
	}
	user, err := message.NewUserMessage("user", "Run the loop.")
	if err != nil {
		return err
	}

	var events []string
	err = agent.ReplyStream(ctx, user, func(event message.Event) error {
		if custom, ok := event.(*message.CustomEvent); ok && strings.HasPrefix(custom.Name, "loop.") {
			events = append(events, custom.Name)
		}
		return nil
	})
	if err != nil {
		return err
	}

	loopState := agent.AgentState().LoopContext
	fmt.Printf("loop=%s mode=%s model_calls=%d events=%s\n",
		loopState.Name,
		loopState.Mode,
		loopState.ModelCalls,
		strings.Join(events, ","),
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
