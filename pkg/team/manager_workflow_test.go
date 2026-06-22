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

package team

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

func TestTeamManagerRegistrationAndWorkerErrorBranches(t *testing.T) {
	t.Parallel()

	model := &coverageChatModel{name: "team"}
	if _, err := (*TeamManager)(nil).Toolkit(TeamRoleLeader); err == nil || !strings.Contains(err.Error(), "team manager is nil") {
		t.Fatalf("nil Toolkit error = %v", err)
	}
	if err := (*TeamManager)(nil).RegisterAgent(nil, TeamRoleLeader, ""); err == nil || !strings.Contains(err.Error(), "team manager is nil") {
		t.Fatalf("nil RegisterAgent error = %v", err)
	}
	if err := WithTeam(nil, TeamRoleLeader)(nil); err == nil || !strings.Contains(err.Error(), "team manager is nil") {
		t.Fatalf("WithTeam nil manager error = %v", err)
	}

	manager := NewManager(nil, WithTeamWorkerOptions(agentpkg.WithReActConfig(agentpkg.ReActConfig{MaxIters: 1})))
	if err := manager.RegisterAgent(nil, TeamRoleLeader, ""); err == nil || !strings.Contains(err.Error(), "team agent is nil") {
		t.Fatalf("RegisterAgent nil error = %v", err)
	}
	emptyStateAgent, err := agentpkg.NewAgent("empty", "lead", model, agentpkg.WithAgentState(&asstate.AgentState{}))
	if err != nil {
		t.Fatalf("NewAgent empty state returned error: %v", err)
	}
	if err := manager.RegisterAgent(emptyStateAgent, TeamRoleLeader, ""); err == nil || !strings.Contains(err.Error(), "session id is empty") {
		t.Fatalf("RegisterAgent empty session error = %v", err)
	}
	if _, err := manager.createTeam(nil, "team", "desc"); err == nil || !strings.Contains(err.Error(), "requires agent state") {
		t.Fatalf("createTeam nil state error = %v", err)
	}
	unregistered := asstate.NewAgentState()
	if _, err := manager.createTeam(unregistered, "team", "desc"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("createTeam unregistered error = %v", err)
	}

	leader, err := agentpkg.NewAgent("leader", "lead", model)
	if err != nil {
		t.Fatalf("NewAgent leader returned error: %v", err)
	}
	if err := manager.RegisterAgent(leader, "", "leader description"); err != nil {
		t.Fatalf("RegisterAgent leader returned error: %v", err)
	}
	leaderState := leader.AgentState()
	if _, err := manager.createTeam(leaderState, " ", "desc"); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("createTeam empty name error = %v", err)
	}
	if text, err := manager.createTeam(leaderState, "Launch", "Ship"); err != nil || !strings.Contains(text, "created") {
		t.Fatalf("createTeam returned %q, %v", text, err)
	}
	leaderState.PermissionContext.Mode = permission.ModeAcceptEdits
	leaderState.PermissionContext.WorkingDirectories["repo"] = permission.AdditionalWorkingDirectory{Path: "/tmp/repo", Source: "session"}
	leaderState.PermissionContext.AllowRules["Bash"] = []permission.Rule{{
		ToolName:    "Bash",
		RuleContent: "git status",
		Behavior:    permission.BehaviorAllow,
		Source:      "session",
	}}
	if _, err := manager.createTeam(leaderState, "Again", "Ship"); err == nil || !strings.Contains(err.Error(), "already part of a team") {
		t.Fatalf("createTeam duplicate error = %v", err)
	}
	if snapshot, ok := manager.Team("missing"); ok || snapshot != nil {
		t.Fatalf("missing team snapshot = %#v, %v", snapshot, ok)
	}
	if _, err := manager.createAgent(context.Background(), leaderState, "", "desc", "prompt", permission.ModeDefault); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("createAgent empty name error = %v", err)
	}
	if _, err := manager.createAgent(context.Background(), leaderState, "leader", "desc", "prompt", permission.ModeDefault); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("createAgent duplicate leader name error = %v", err)
	}

	factoryErr := errors.New("factory failed")
	manager.workerFactory = func(context.Context, TeamWorkerRequest) (*agentpkg.Agent, error) {
		return nil, factoryErr
	}
	if _, err := manager.createAgent(context.Background(), leaderState, "worker-error", "desc", "prompt", permission.ModeDefault); !errors.Is(err, factoryErr) {
		t.Fatalf("createAgent factory error = %v", err)
	}
	manager.workerFactory = func(context.Context, TeamWorkerRequest) (*agentpkg.Agent, error) {
		return nil, nil
	}
	if _, err := manager.createAgent(context.Background(), leaderState, "worker-nil", "desc", "prompt", permission.ModeDefault); err == nil || !strings.Contains(err.Error(), "nil agent") {
		t.Fatalf("createAgent nil worker error = %v", err)
	}

	var captured TeamWorkerRequest
	manager.workerFactory = func(ctx context.Context, request TeamWorkerRequest) (*agentpkg.Agent, error) {
		captured = request
		return agentpkg.NewAgent(request.Name, request.SystemPrompt, model)
	}
	if text, err := manager.createAgent(context.Background(), leaderState, "worker", "research", "start", permission.ModeExplore); err != nil || !strings.Contains(text, "worker") {
		t.Fatalf("createAgent success = %q, %v", text, err)
	}
	if captured.Leader != leader || captured.Team.Name != "Launch" || !strings.Contains(captured.SystemPrompt, "research") {
		t.Fatalf("worker request mismatch: %#v", captured)
	}
	if captured.PermissionContext == nil {
		t.Fatal("worker request should include inherited permission context")
	}
	if captured.PermissionContext.Mode != permission.ModeExplore {
		t.Fatalf("explicit worker mode should override inherited mode, got %q", captured.PermissionContext.Mode)
	}
	if captured.PermissionContext.WorkingDirectories["repo"].Path != "/tmp/repo" {
		t.Fatalf("worker should inherit leader working directories: %#v", captured.PermissionContext.WorkingDirectories)
	}
	if got := captured.PermissionContext.AllowRules["Bash"]; len(got) != 1 || got[0].RuleContent != "git status" {
		t.Fatalf("worker should inherit leader allow rules: %#v", got)
	}
	captured.PermissionContext.AllowRules["Bash"][0].RuleContent = "mutated"
	if leaderState.PermissionContext.AllowRules["Bash"][0].RuleContent != "git status" {
		t.Fatalf("worker permission context should not alias leader rules: %#v", leaderState.PermissionContext.AllowRules)
	}
	team, ok := manager.TeamForSession(leaderState.SessionID)
	if !ok || len(team.Members) != 1 {
		t.Fatalf("team snapshot after createAgent mismatch: %#v ok=%v", team, ok)
	}
	workerSession := team.Members[0].SessionID
	workerParticipant := manager.participants[workerSession]
	if workerParticipant == nil || workerParticipant.Agent == nil {
		t.Fatalf("worker participant missing: %#v", manager.participants)
	}
	workerState := workerParticipant.Agent.AgentState()
	if _, err := manager.createAgent(context.Background(), workerState, "other", "desc", "prompt", permission.ModeDefault); err == nil || !strings.Contains(err.Error(), "only the team leader") {
		t.Fatalf("worker createAgent error = %v", err)
	}
	if _, err := manager.say(leaderState, "hello", "missing"); err == nil || !strings.Contains(err.Error(), "no team member") {
		t.Fatalf("say missing target error = %v", err)
	}
	if _, err := manager.say(leaderState, "hello", "leader"); err == nil || !strings.Contains(err.Error(), "yourself") {
		t.Fatalf("say self error = %v", err)
	}
	if text, err := manager.say(workerState, "done", "leader"); err != nil || !strings.Contains(text, "Delivered") {
		t.Fatalf("worker say direct = %q, %v", text, err)
	}
	if _, err := manager.deleteTeam(workerState); err == nil || !strings.Contains(err.Error(), "only the team leader") {
		t.Fatalf("worker deleteTeam error = %v", err)
	}

	messages := manager.PendingMessages(leaderState.SessionID)
	if len(messages) != 1 || messages[0].GetTextContent("") == nil {
		t.Fatalf("PendingMessages leader mismatch: %#v", messages)
	}
	messages[0].Content = nil
	manager.inbox[leaderState.SessionID] = []*message.Message{message.MustAssistantMessage("team", "kept")}
	cloned := manager.PendingMessages(leaderState.SessionID)
	if len(cloned) != 1 || cloned[0].GetTextContent("") == nil {
		t.Fatalf("PendingMessages should clone and skip nil: %#v", cloned)
	}
	manager.inbox["missing"] = []*message.Message{message.MustAssistantMessage("team", "orphan")}
	manager.inbox[workerSession] = nil
	if agents := manager.PendingAgents(); len(agents) != 0 {
		t.Fatalf("PendingAgents should skip orphan and empty inbox: %#v", agents)
	}
	if err := manager.DrainInbox(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "drain agent is nil") {
		t.Fatalf("DrainInbox nil error = %v", err)
	}
	if err := manager.DrainInbox(context.Background(), workerParticipant.Agent); err != nil {
		t.Fatalf("DrainInbox empty returned error: %v", err)
	}

	if text, err := manager.deleteTeam(leaderState); err != nil || !strings.Contains(text, "dissolved") {
		t.Fatalf("deleteTeam leader = %q, %v", text, err)
	}
	if _, err := manager.say(leaderState, "after delete", ""); err == nil || !strings.Contains(err.Error(), "not in any team") {
		t.Fatalf("say after delete error = %v", err)
	}
}

func TestTeamManagerDefaultFactoryAndDeletedTeamBranches(t *testing.T) {
	t.Parallel()

	model := &coverageChatModel{name: "worker-model"}
	manager := NewManager()
	leader, err := agentpkg.NewAgent("leader", "lead", model)
	if err != nil {
		t.Fatalf("NewAgent leader returned error: %v", err)
	}
	if err := manager.RegisterAgent(leader, TeamRoleLeader, ""); err != nil {
		t.Fatalf("RegisterAgent returned error: %v", err)
	}
	leaderState := leader.AgentState()
	if _, err := manager.createTeam(leaderState, "Launch", ""); err != nil {
		t.Fatalf("createTeam returned error: %v", err)
	}
	if _, err := manager.defaultWorkerFactory(context.Background(), TeamWorkerRequest{Name: "worker"}); err == nil || !strings.Contains(err.Error(), "no worker factory") {
		t.Fatalf("defaultWorkerFactory missing model error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	manager.workerModel = model
	if _, err := manager.defaultWorkerFactory(canceled, TeamWorkerRequest{Name: "worker"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("defaultWorkerFactory canceled error = %v", err)
	}
	worker, err := manager.defaultWorkerFactory(context.Background(), TeamWorkerRequest{
		Name:           "worker",
		SystemPrompt:   "worker system",
		PermissionMode: permission.ModeBypass,
		PermissionContext: &permission.Context{
			Mode:               permission.ModeBypass,
			WorkingDirectories: map[string]permission.AdditionalWorkingDirectory{"repo": {Path: "/tmp/repo", Source: "session"}},
			AllowRules: map[string][]permission.Rule{
				"Bash": {{ToolName: "Bash", RuleContent: "git status", Behavior: permission.BehaviorAllow, Source: "session"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("defaultWorkerFactory returned error: %v", err)
	}
	if worker.AgentName() != "worker" || worker.AgentState().PermissionContext.Mode != permission.ModeBypass {
		t.Fatalf("default worker mismatch: %#v", worker)
	}
	if worker.AgentState().PermissionContext.WorkingDirectories["repo"].Path != "/tmp/repo" {
		t.Fatalf("default worker should apply inherited working directories: %#v", worker.AgentState().PermissionContext.WorkingDirectories)
	}
	if got := worker.AgentState().PermissionContext.AllowRules["Bash"]; len(got) != 1 || got[0].RuleContent != "git status" {
		t.Fatalf("default worker should apply inherited rules: %#v", got)
	}

	deletedManager := NewManager()
	deletedLeader, err := agentpkg.NewAgent("deleted-leader", "lead", model)
	if err != nil {
		t.Fatalf("NewAgent deleted leader returned error: %v", err)
	}
	if err := deletedManager.RegisterAgent(deletedLeader, TeamRoleLeader, ""); err != nil {
		t.Fatalf("RegisterAgent deleted leader returned error: %v", err)
	}
	deletedLeaderState := deletedLeader.AgentState()
	if _, err := deletedManager.createTeam(deletedLeaderState, "Deleted", ""); err != nil {
		t.Fatalf("createTeam deleted returned error: %v", err)
	}
	deletedManager.workerFactory = func(_ context.Context, request TeamWorkerRequest) (*agentpkg.Agent, error) {
		deletedManager.mu.Lock()
		delete(deletedManager.teams, request.Team.ID)
		deletedManager.mu.Unlock()
		return agentpkg.NewAgent("late-worker", "worker", model)
	}
	if _, err := deletedManager.createAgent(context.Background(), deletedLeaderState, "late-worker", "desc", "prompt", permission.ModeDefault); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("createAgent deleted team error = %v", err)
	}

	if got := buildWorkerSystemPrompt("Team", " purpose ", "worker", " role "); !strings.Contains(got, "purpose") || !strings.Contains(got, "role") {
		t.Fatalf("buildWorkerSystemPrompt = %q", got)
	}
	schema := objectSchema(nil, nil)
	if schema["properties"] == nil || schema["type"] != "object" {
		t.Fatalf("objectSchema nil mismatch: %#v", schema)
	}
	if got := stringInput(map[string]any{"name": " worker "}, "name"); got != "worker" {
		t.Fatalf("stringInput trim = %q", got)
	}
	if got := stringInput(nil, "name"); got != "" {
		t.Fatalf("stringInput nil = %q", got)
	}
	if text := toolText("ok", errors.New("bad")); text.GetTextContent("") == nil || *text.GetTextContent("") != "bad" {
		t.Fatalf("toolText error mismatch: %#v", text)
	}
	options := teamToolOptions(true)
	tool, err := astool.NewFunctionTool("TeamOption", "desc", objectSchema(nil, nil), func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
		return message.ContentBlockList{message.NewTextBlock("ok")}, nil
	}, options...)
	if err != nil {
		t.Fatalf("NewFunctionTool with team options returned error: %v", err)
	}
	if !tool.IsConcurrencySafe() || !tool.IsReadOnly() || !tool.IsStateInjected() {
		t.Fatalf("team tool options not applied")
	}
	decision, err := tool.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("team permission decision = %#v, %v", decision, err)
	}
}

type coverageChatModel struct {
	name        string
	countTokens int
}

func (m *coverageChatModel) Name() string {
	if m == nil || m.name == "" {
		return "coverage"
	}
	return m.name
}

func (m *coverageChatModel) Call(_ context.Context, request asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("ok")}, true), nil
}

func (m *coverageChatModel) Stream(_ context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	out := make(chan asmodel.ChatResponse, 1)
	out <- *asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("ok")}, true)
	close(out)
	return out, nil
}

func (m *coverageChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	if m != nil && m.countTokens != 0 {
		return m.countTokens, nil
	}
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}
