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

package runner_test

import (
	"context"
	"fmt"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	automationevent "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
	automationgate "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/gate"
	automationgoal "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/goal"
	runnerpkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/runner"
	automationstore "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/store"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/core"
	loopruntime "github.com/yuluo-yx/agentscope-go/pkg/loop/runtime"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
)

type scriptedChatModel struct {
	responses []*modelpkg.ChatResponse
	requests  []modelpkg.CallRequest
}

func (m *scriptedChatModel) Name() string {
	return "scripted"
}

func (m *scriptedChatModel) Call(_ context.Context, request modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func (m *scriptedChatModel) Stream(ctx context.Context, request modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	ch := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(ch)
		select {
		case ch <- *response.Clone():
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (m *scriptedChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func TestRunnerHandlesEventAndRecordsRun(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("triage complete")},
			true,
			modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{InputTokens: 5, OutputTokens: 3}),
		),
	}}
	spec := core.Spec{
		Name:   "daily-triage",
		Goal:   "scan repository signals and produce a report",
		Mode:   core.ModeReportOnly,
		Policy: core.DefaultPolicy(core.ModeReportOnly),
	}
	agent, err := agentpkg.NewAgent("Friday", "You are helpful.", model, loopruntime.WithSpec(spec))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	mapper, err := runnerpkg.NewTemplateMapper("Handle {{.Event.Type}} from {{.Event.Source}}.")
	if err != nil {
		t.Fatalf("NewTemplateMapper returned error: %v", err)
	}
	store := automationstore.NewMemoryRunStore()
	var customEvents []string
	runner := runnerpkg.Runner{
		Router: automationevent.StaticRouter{Decision: automationevent.RouteDecision{LoopName: spec.Name, AgentName: "Friday"}},
		Mapper: mapper,
		Store:  store,
		Agents: runnerpkg.StaticAgentResolver{Agent: agent},
		Yield: func(_ context.Context, _ automationevent.Event, event message.Event) error {
			if custom, ok := event.(*message.CustomEvent); ok {
				customEvents = append(customEvents, custom.Name)
			}
			return nil
		},
	}
	event := automationevent.Event{ID: "evt-1", Source: "schedule://daily-triage", Type: "schedule.tick", DedupKey: "daily-triage"}

	if err := runner.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent returned error: %v", err)
	}
	if err := runner.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("duplicate HandleEvent returned error: %v", err)
	}

	runs := store.Runs()
	if len(runs) != 1 {
		t.Fatalf("recorded runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if run.EventID != event.ID || run.DedupKey != event.DedupKey || run.LoopName != spec.Name ||
		run.EventSource != event.Source || run.EventType != event.Type ||
		run.AgentName != "Friday" || run.ReplyID == "" || run.SessionID == "" || run.StopReason != "completed" ||
		run.ModelCalls != 1 || run.InputTokens != 5 || run.OutputTokens != 3 ||
		!run.FinishedAt.After(run.StartedAt) && !run.FinishedAt.Equal(run.StartedAt) {
		t.Fatalf("RunRecord mismatch: %#v", run)
	}
	if len(customEvents) == 0 {
		t.Fatalf("runner should yield loop custom events")
	}
}

func TestRunnerAllocatesWorkspaceForRun(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("triage complete")}, true),
	}}
	spec := core.Spec{Name: "isolated-triage", Goal: "scan in isolated workspace", Mode: core.ModeReportOnly}
	agent, err := agentpkg.NewAgent("Friday", "You are helpful.", model, loopruntime.WithSpec(spec))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	store := automationstore.NewMemoryRunStore()
	allocator := &recordingWorkspaceAllocator{
		lease: &recordingWorkspaceLease{
			root:     "/tmp/agentscope/workspaces/evt-1",
			metadata: map[string]string{"kind": "noop"},
		},
	}
	mapperSawWorkspace := false
	resolverSawWorkspace := false
	runner := runnerpkg.Runner{
		Router:    automationevent.StaticRouter{Decision: automationevent.RouteDecision{LoopName: spec.Name, AgentName: "Friday"}},
		Workspace: allocator,
		Mapper: runnerpkg.InputMapperFunc(func(_ context.Context, _ automationevent.Event, decision automationevent.RouteDecision) (*message.Message, error) {
			if decision.Metadata["workspace_root"] == allocator.lease.root {
				mapperSawWorkspace = true
			}
			return message.NewUserMessage("user", "run")
		}),
		Store: store,
		Agents: runnerpkg.AgentResolverFunc(func(_ context.Context, decision automationevent.RouteDecision) (*agentpkg.Agent, error) {
			if decision.Metadata["workspace_root"] == allocator.lease.root {
				resolverSawWorkspace = true
			}
			return agent, nil
		}),
	}
	event := automationevent.Event{ID: "evt-1", Source: "manual://local", Type: "manual.requested"}

	if err := runner.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent returned error: %v", err)
	}
	if allocator.calls != 1 {
		t.Fatalf("workspace allocations = %d, want 1", allocator.calls)
	}
	if !allocator.lease.closed {
		t.Fatalf("workspace lease should be closed")
	}
	if !mapperSawWorkspace || !resolverSawWorkspace {
		t.Fatalf("workspace metadata should reach mapper and resolver: mapper=%v resolver=%v", mapperSawWorkspace, resolverSawWorkspace)
	}
	runs := store.Runs()
	if len(runs) != 1 {
		t.Fatalf("recorded runs = %d, want 1", len(runs))
	}
	if runs[0].WorkspaceRoot != allocator.lease.root || runs[0].WorkspaceMetadata["kind"] != "noop" {
		t.Fatalf("workspace run record mismatch: %#v", runs[0])
	}
}

func TestRunnerStopsBeforeAgentWhenGateRequiresHuman(t *testing.T) {
	t.Parallel()

	store := automationstore.NewMemoryRunStore()
	allocator := &recordingWorkspaceAllocator{
		lease: &recordingWorkspaceLease{root: "/tmp/agentscope/workspaces/evt-1"},
	}
	mapperCalled := false
	resolverCalled := false
	runner := runnerpkg.Runner{
		Router: automationevent.StaticRouter{Decision: automationevent.RouteDecision{
			LoopName:  "release-loop",
			AgentName: "Friday",
			Metadata:  map[string]any{"risk": "release"},
		}},
		Gate: automationgate.GateFunc(func(_ context.Context, event automationevent.Event, decision automationevent.RouteDecision) (automationgate.GateDecision, error) {
			event.Labels[0] = "mutated"
			decision.Metadata["risk"] = "mutated"
			return automationgate.GateDecision{
				StopReason: automationgoal.GoalStopWaitingUser,
				Reason:     "release window requires approval",
				Metadata:   map[string]string{"gate": "release-window"},
			}, nil
		}),
		Workspace: allocator,
		Mapper: runnerpkg.InputMapperFunc(func(context.Context, automationevent.Event, automationevent.RouteDecision) (*message.Message, error) {
			mapperCalled = true
			return message.NewUserMessage("user", "run")
		}),
		Store: store,
		Agents: runnerpkg.AgentResolverFunc(func(context.Context, automationevent.RouteDecision) (*agentpkg.Agent, error) {
			resolverCalled = true
			return nil, fmt.Errorf("agent should not be resolved")
		}),
	}
	event := automationevent.Event{
		ID:     "evt-1",
		Source: "manual://release",
		Type:   "manual.requested",
		Labels: []string{"approval"},
	}

	if err := runner.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent returned error: %v", err)
	}
	if allocator.calls != 0 || mapperCalled || resolverCalled {
		t.Fatalf("gate should stop before workspace/mapper/agent: allocations=%d mapper=%v resolver=%v",
			allocator.calls, mapperCalled, resolverCalled)
	}
	runs := store.Runs()
	if len(runs) != 1 {
		t.Fatalf("recorded runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if run.LoopName != "release-loop" || run.AgentName != "Friday" ||
		run.StopReason != automationgoal.GoalStopWaitingUser ||
		run.GateReason != "release window requires approval" ||
		run.GateMetadata["gate"] != "release-window" {
		t.Fatalf("gated run mismatch: %#v", run)
	}
	if event.Labels[0] != "approval" {
		t.Fatalf("gate should receive a cloned event")
	}
}

func TestRunnerGateStopDoesNotRequireMapperOrAgent(t *testing.T) {
	t.Parallel()

	store := automationstore.NewMemoryRunStore()
	runner := runnerpkg.Runner{
		Router: automationevent.StaticRouter{Decision: automationevent.RouteDecision{LoopName: "approval-loop"}},
		Store:  store,
		Gate: automationgate.GateFunc(func(context.Context, automationevent.Event, automationevent.RouteDecision) (automationgate.GateDecision, error) {
			return automationgate.GateDecision{StopReason: automationgoal.GoalStopWaitingExternal, Reason: "external approval pending"}, nil
		}),
	}
	event := automationevent.Event{ID: "evt-1", Source: "manual://approval", Type: "manual.requested"}

	if err := runner.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent returned error: %v", err)
	}
	runs := store.Runs()
	if len(runs) != 1 || runs[0].StopReason != automationgoal.GoalStopWaitingExternal {
		t.Fatalf("gated run mismatch: %#v", runs)
	}
}

type recordingWorkspaceAllocator struct {
	calls int
	lease *recordingWorkspaceLease
}

func (a *recordingWorkspaceAllocator) Allocate(context.Context, automationevent.Event, automationevent.RouteDecision) (runnerpkg.WorkspaceLease, error) {
	a.calls++
	return a.lease, nil
}

type recordingWorkspaceLease struct {
	root     string
	metadata map[string]string
	closed   bool
}

func (l *recordingWorkspaceLease) Root() string {
	return l.root
}

func (l *recordingWorkspaceLease) Metadata() map[string]string {
	return l.metadata
}

func (l *recordingWorkspaceLease) Close(context.Context) error {
	l.closed = true
	return nil
}
