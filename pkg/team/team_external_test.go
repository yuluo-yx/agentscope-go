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

package team_test

import (
	"context"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
	teampkg "github.com/yuluo-yx/agentscope-go/pkg/team"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

func TestTeamToolsCreateWorkerAndRouteMessages(t *testing.T) {
	model := teamScriptedModel{}
	manager := teampkg.NewManager(teampkg.WithTeamWorkerModel(model))
	leader, err := agentpkg.NewAgent("leader", "Lead the team.", model, teampkg.WithTeam(manager, teampkg.TeamRoleLeader))
	if err != nil {
		t.Fatalf("NewAgent leader returned error: %v", err)
	}
	leaderTools := mustTeamToolkit(t, manager, teampkg.TeamRoleLeader)

	createTeam := runTeamTool(t, leaderTools, "TeamCreate", `{"name":"Launch","description":"Ship the release"}`, leader.AgentState())
	if !strings.Contains(createTeam, "created") {
		t.Fatalf("TeamCreate response mismatch: %q", createTeam)
	}

	createWorker := runTeamTool(t, leaderTools, "AgentCreate", `{"name":"researcher","description":"Find facts","prompt":"Collect launch facts","permission_mode":"explore"}`, leader.AgentState())
	if !strings.Contains(createWorker, "researcher") {
		t.Fatalf("AgentCreate response mismatch: %q", createWorker)
	}

	team, ok := manager.TeamForSession(leader.AgentState().SessionID)
	if !ok || team.Name != "Launch" || len(team.Members) != 1 {
		t.Fatalf("team snapshot mismatch: %#v ok=%v", team, ok)
	}
	worker := onlyPendingAgent(t, manager)
	workerSessionID := team.Members[0].SessionID
	pending := manager.PendingMessages(workerSessionID)
	if len(pending) != 1 || !teamMessageContains(pending[0], "Collect launch facts") {
		t.Fatalf("worker initial inbox mismatch: %#v", pending)
	}

	runTeamTool(t, leaderTools, "TeamSay", `{"to":"researcher","content":"Use official notes only"}`, leader.AgentState())
	pending = manager.PendingMessages(workerSessionID)
	if len(pending) != 1 || !teamMessageContains(pending[0], "Use official notes only") {
		t.Fatalf("worker direct inbox mismatch: %#v", pending)
	}

	workerTools := mustTeamToolkit(t, manager, teampkg.TeamRoleWorker)
	runTeamTool(t, workerTools, "TeamSay", `{"content":"Research complete"}`, worker.AgentState())
	leaderInbox := manager.PendingMessages(leader.AgentState().SessionID)
	if len(leaderInbox) != 1 || !teamMessageContains(leaderInbox[0], "Research complete") {
		t.Fatalf("leader inbox mismatch: %#v", leaderInbox)
	}

	deleteTeam := runTeamTool(t, leaderTools, "TeamDelete", `{}`, leader.AgentState())
	if !strings.Contains(deleteTeam, "dissolved") {
		t.Fatalf("TeamDelete response mismatch: %q", deleteTeam)
	}
	if _, ok := manager.TeamForSession(leader.AgentState().SessionID); ok {
		t.Fatal("leader should no longer be in a team after TeamDelete")
	}
}

func TestTeamManagerDrainInboxObservesMessages(t *testing.T) {
	ctx := context.Background()
	model := teamScriptedModel{}
	manager := teampkg.NewManager(teampkg.WithTeamWorkerModel(model))
	leader, err := agentpkg.NewAgent("leader", "Lead.", model, teampkg.WithTeam(manager, teampkg.TeamRoleLeader))
	if err != nil {
		t.Fatalf("NewAgent leader returned error: %v", err)
	}
	leaderTools := mustTeamToolkit(t, manager, teampkg.TeamRoleLeader)
	runTeamTool(t, leaderTools, "TeamCreate", `{"name":"Team","description":"Work"}`, leader.AgentState())
	runTeamTool(t, leaderTools, "AgentCreate", `{"name":"worker","description":"Work","prompt":"Initial task"}`, leader.AgentState())

	worker := onlyPendingAgent(t, manager)
	if err := manager.DrainInbox(ctx, worker); err != nil {
		t.Fatalf("DrainInbox returned error: %v", err)
	}
	if len(worker.AgentState().Context) != 1 || !teamMessageContains(worker.AgentState().Context[0], "Initial task") {
		t.Fatalf("worker context mismatch after drain: %#v", worker.AgentState().Context)
	}
}

func mustTeamToolkit(t *testing.T, manager *teampkg.Manager, role teampkg.TeamRole) *astool.Toolkit {
	t.Helper()
	kit, err := manager.Toolkit(role)
	if err != nil {
		t.Fatalf("Toolkit returned error: %v", err)
	}
	return kit
}

func runTeamTool(t *testing.T, kit *astool.Toolkit, name, input string, state *agentpkg.AgentState) string {
	t.Helper()
	response, err := kit.RunTool(context.Background(), message.NewToolCallBlock("call-"+name, name, input), state)
	if err != nil {
		t.Fatalf("%s RunTool returned error: %v", name, err)
	}
	text := response.GetTextContent("")
	if text == nil {
		t.Fatalf("%s returned no text: %#v", name, response)
	}
	return *text
}

func onlyPendingAgent(t *testing.T, manager *teampkg.Manager) *agentpkg.Agent {
	t.Helper()
	agents := manager.PendingAgents()
	if len(agents) != 1 {
		t.Fatalf("expected one pending agent, got %d", len(agents))
	}
	return agents[0]
}

func teamMessageContains(msg *message.Message, text string) bool {
	if msg == nil {
		return false
	}
	got := msg.GetTextContent("")
	return got != nil && strings.Contains(*got, text)
}

type teamScriptedModel struct{}

func (teamScriptedModel) Name() string { return "team-scripted" }

func (teamScriptedModel) Call(context.Context, modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	return modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("ok")}, true), nil
}

func (m teamScriptedModel) Stream(ctx context.Context, request modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	out := make(chan modelpkg.ChatResponse, 1)
	response, err := m.Call(ctx, request)
	if err != nil {
		return nil, err
	}
	out <- *response
	close(out)
	return out, nil
}

func (teamScriptedModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}
