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
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	teampkg "github.com/yuluo-yx/agentscope-go/team"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

func main() {
	ctx := context.Background()
	model := scriptedChatModel{}
	manager := teampkg.NewManager(teampkg.WithTeamWorkerModel(model))
	leader := mustAgent(agentpkg.NewAgent("leader", "Coordinate the team.", model, teampkg.WithTeam(manager, teampkg.TeamRoleLeader)))
	leaderTools := mustToolkit(manager.Toolkit(teampkg.TeamRoleLeader))

	fmt.Println(runTool(leaderTools, "TeamCreate", `{"name":"Launch","description":"Prepare a release summary"}`, leader.AgentState()))
	fmt.Println(runTool(leaderTools, "AgentCreate", `{"name":"researcher","description":"Collects release facts","prompt":"Find the three most important release facts and report back."}`, leader.AgentState()))

	worker := onlyPendingAgent(manager)
	must(manager.DrainInbox(ctx, worker))
	workerInput := worker.AgentState().Context[0].GetTextContent("")

	workerTools := mustToolkit(manager.Toolkit(teampkg.TeamRoleWorker))
	fmt.Println(runTool(workerTools, "TeamSay", `{"content":"Release facts collected: API, examples, and tests are ready."}`, worker.AgentState()))
	must(manager.DrainInbox(ctx, leader))
	leaderInput := leader.AgentState().Context[len(leader.AgentState().Context)-1].GetTextContent("")

	team, _ := manager.TeamForSession(leader.AgentState().SessionID)
	fmt.Printf("team=%s members=%d worker_received=%q leader_received=%q\n", team.Name, len(team.Members), shorten(value(workerInput), 72), shorten(value(leaderInput), 72))
}

type scriptedChatModel struct{}

func (scriptedChatModel) Name() string { return "scripted-team-example" }

func (scriptedChatModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("ok")}, true), nil
}

func (m scriptedChatModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	response, err := m.Call(ctx, request)
	if err != nil {
		return nil, err
	}
	out := make(chan asmodel.ChatResponse, 1)
	out <- *response
	close(out)
	return out, nil
}

func (scriptedChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func runTool(kit *astool.Toolkit, name, input string, state *agentpkg.AgentState) string {
	response, err := kit.RunTool(context.Background(), message.NewToolCallBlock("call-"+name, name, input), state)
	must(err)
	return value(response.GetTextContent(""))
}

func onlyPendingAgent(manager *teampkg.Manager) *agentpkg.Agent {
	agents := manager.PendingAgents()
	if len(agents) != 1 {
		panic(fmt.Sprintf("expected one pending agent, got %d", len(agents)))
	}
	return agents[0]
}

func mustAgent(agent *agentpkg.Agent, err error) *agentpkg.Agent {
	must(err)
	return agent
}

func mustToolkit(kit *astool.Toolkit, err error) *astool.Toolkit {
	must(err)
	return kit
}

func must(err error) {
	if err != nil {
		panic(err)
	}
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
