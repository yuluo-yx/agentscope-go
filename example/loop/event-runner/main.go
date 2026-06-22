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
	automationevent "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
	automationrunner "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/runner"
	automationstore "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/store"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/core"
	loopruntime "github.com/yuluo-yx/agentscope-go/pkg/loop/runtime"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "event runner example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	spec := core.Spec{
		Name: "daily-triage",
		Goal: "handle a generic automation event and produce a report-only summary",
		NonGoals: []string{
			"call external issue trackers",
			"modify repository files",
		},
		SuccessCriteria: []core.SuccessCriterion{
			{Name: "report", Description: "final reply contains findings and next action", Required: true},
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
	mapper, err := automationrunner.NewTemplateMapper(
		"Handle event {{.Type}} from {{.Source}} for {{.Subject}}. Run the configured loop and report evidence.",
	)
	if err != nil {
		return err
	}
	store := automationstore.NewMemoryRunStore()
	var loopEvents []string
	runner := automationrunner.Runner{
		Router: automationevent.StaticRouter{Decision: automationevent.RouteDecision{
			LoopName:  spec.Name,
			AgentName: agent.AgentName(),
		}},
		Mapper: mapper,
		Store:  store,
		Agents: automationrunner.StaticAgentResolver{Agent: agent},
		Yield: func(_ context.Context, _ automationevent.Event, event message.Event) error {
			if custom, ok := event.(*message.CustomEvent); ok && strings.HasPrefix(custom.Name, "loop.") {
				loopEvents = append(loopEvents, custom.Name)
			}
			return nil
		},
	}

	event := automationevent.Event{
		ID:       "evt-daily-triage-1",
		Source:   "schedule://daily-triage",
		Type:     automationevent.EventTypeScheduleTick,
		Subject:  "repo://current",
		DedupKey: "daily-triage:today",
		Labels:   []string{"daily", "triage"},
	}
	if err := runner.HandleEvent(ctx, event); err != nil {
		return err
	}

	runs := store.Runs()
	if len(runs) == 0 {
		return fmt.Errorf("event runner did not record a run")
	}
	fmt.Printf("runs=%d stop=%s events=%s\n",
		len(runs),
		runs[0].StopReason,
		strings.Join(loopEvents, ","),
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
