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
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	teampkg "github.com/yuluo-yx/agentscope-go/pkg/team"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agent team example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	model, err := newDashScopeChatModel(false)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}
	manager := teampkg.NewManager(teampkg.WithTeamWorkerModel(model))
	leader, err := agentpkg.NewAgent("leader", "Coordinate the team.", model, teampkg.WithTeam(manager, teampkg.TeamRoleLeader))
	if err != nil {
		return fmt.Errorf("create leader agent: %w", err)
	}
	leaderTools, err := manager.Toolkit(teampkg.TeamRoleLeader)
	if err != nil {
		return fmt.Errorf("create leader toolkit: %w", err)
	}

	output, err := runTool(leaderTools, "TeamCreate", `{"name":"Launch","description":"Prepare a release summary"}`, leader.AgentState())
	if err != nil {
		return err
	}
	fmt.Println(output)
	output, err = runTool(leaderTools, "AgentCreate", `{"name":"researcher","description":"Collects release facts","prompt":"Find the three most important release facts and report back."}`, leader.AgentState())
	if err != nil {
		return err
	}
	fmt.Println(output)

	worker, err := onlyPendingAgent(manager)
	if err != nil {
		return err
	}
	if err := manager.DrainInbox(ctx, worker); err != nil {
		return fmt.Errorf("drain worker inbox: %w", err)
	}
	workerInput := worker.AgentState().Context[0].GetTextContent("")

	workerTools, err := manager.Toolkit(teampkg.TeamRoleWorker)
	if err != nil {
		return fmt.Errorf("create worker toolkit: %w", err)
	}
	output, err = runTool(workerTools, "TeamSay", `{"content":"Release facts collected: API, examples, and tests are ready."}`, worker.AgentState())
	if err != nil {
		return err
	}
	fmt.Println(output)
	if err := manager.DrainInbox(ctx, leader); err != nil {
		return fmt.Errorf("drain leader inbox: %w", err)
	}
	leaderInput := leader.AgentState().Context[len(leader.AgentState().Context)-1].GetTextContent("")

	team, ok := manager.TeamForSession(leader.AgentState().SessionID)
	if !ok {
		return fmt.Errorf("team not found for leader session")
	}
	fmt.Printf("team=%s members=%d worker_received=%q leader_received=%q\n", team.Name, len(team.Members), shorten(value(workerInput), 72), shorten(value(leaderInput), 72))
	return nil
}

func runTool(kit *astool.Toolkit, name, input string, state *agentpkg.AgentState) (string, error) {
	response, err := kit.RunTool(context.Background(), message.NewToolCallBlock("call-"+name, name, input), state)
	if err != nil {
		return "", fmt.Errorf("run %s tool: %w", name, err)
	}
	return value(response.GetTextContent("")), nil
}

func onlyPendingAgent(manager *teampkg.Manager) (*agentpkg.Agent, error) {
	agents := manager.PendingAgents()
	if len(agents) != 1 {
		return nil, fmt.Errorf("expected one pending agent, got %d", len(agents))
	}
	return agents[0], nil
}

func value(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func shorten(text string, limit int) string {
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
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
