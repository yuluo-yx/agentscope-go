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
	"github.com/yuluo-yx/agentscope-go/loop/core"
	loopruntime "github.com/yuluo-yx/agentscope-go/loop/runtime"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

type scriptedChatModel struct {
	responses []*asmodel.ChatResponse
}

func (m *scriptedChatModel) Name() string { return "scripted-loop-basic" }

func (m *scriptedChatModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return m.nextResponse()
}

func (m *scriptedChatModel) Stream(ctx context.Context, _ asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	response, err := m.nextResponse()
	if err != nil {
		return nil, err
	}
	out := make(chan asmodel.ChatResponse, 1)
	go func() {
		defer close(out)
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
	model := &scriptedChatModel{responses: []*asmodel.ChatResponse{
		asmodel.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("findings=0 next=keep monitoring")},
			true,
			asmodel.WithChatResponseUsage(&asmodel.ChatUsage{InputTokens: 12, OutputTokens: 5}),
		),
	}}
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
