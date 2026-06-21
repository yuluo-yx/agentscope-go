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
	"errors"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	runnerpkg "github.com/yuluo-yx/agentscope-go/loop/automation/runner"
	automationstore "github.com/yuluo-yx/agentscope-go/loop/automation/store"
	"github.com/yuluo-yx/agentscope-go/message"
)

func TestInputMapperAndAgentResolverBoundaryErrors(t *testing.T) {
	t.Parallel()

	if _, err := (runnerpkg.InputMapperFunc)(nil).MapInput(context.Background(), automationevent.Event{}, automationevent.RouteDecision{}); err == nil {
		t.Fatalf("nil InputMapperFunc should fail")
	}
	if _, err := (runnerpkg.AgentResolverFunc)(nil).ResolveAgent(context.Background(), automationevent.RouteDecision{}); err == nil {
		t.Fatalf("nil AgentResolverFunc should fail")
	}

	agent, err := agentpkg.NewAgent("Friday", "You are helpful.", &scriptedChatModel{})
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	resolver := runnerpkg.StaticAgentResolver{Agent: agent}
	if got, err := resolver.ResolveAgent(context.Background(), automationevent.RouteDecision{}); err != nil || got != agent {
		t.Fatalf("ResolveAgent returned (%v, %v), want configured agent", got, err)
	}
	var nilCtx context.Context
	if _, err := resolver.ResolveAgent(nilCtx, automationevent.RouteDecision{}); err == nil {
		t.Fatalf("ResolveAgent should reject a nil context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.ResolveAgent(ctx, automationevent.RouteDecision{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveAgent canceled error = %v, want %v", err, context.Canceled)
	}
	if _, err := (runnerpkg.StaticAgentResolver{}).ResolveAgent(context.Background(), automationevent.RouteDecision{}); err == nil {
		t.Fatalf("ResolveAgent should reject a nil configured agent")
	}
}

func TestTemplateMapperRejectsInvalidConfigurationAndExecution(t *testing.T) {
	t.Parallel()

	if _, err := runnerpkg.NewTemplateMapper("   "); err == nil {
		t.Fatalf("NewTemplateMapper should reject an empty template")
	}
	if _, err := runnerpkg.NewTemplateMapper("{{", nil); err == nil {
		t.Fatalf("NewTemplateMapper should reject an invalid template")
	}
	if _, err := runnerpkg.NewTemplateMapper("hello", runnerpkg.WithTemplateMapperUserName("   ")); err == nil {
		t.Fatalf("NewTemplateMapper should reject an empty user name")
	}
	if _, err := ((*runnerpkg.TemplateMapper)(nil)).MapInput(context.Background(), automationevent.Event{}, automationevent.RouteDecision{}); err == nil {
		t.Fatalf("nil TemplateMapper should fail")
	}

	mapper, err := runnerpkg.NewTemplateMapper("Handle {{.ID}}", nil)
	if err != nil {
		t.Fatalf("NewTemplateMapper returned error: %v", err)
	}
	var nilCtx context.Context
	if _, err := mapper.MapInput(nilCtx, automationevent.Event{}, automationevent.RouteDecision{}); err == nil {
		t.Fatalf("MapInput should reject a nil context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mapper.MapInput(ctx, automationevent.Event{}, automationevent.RouteDecision{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("MapInput canceled error = %v, want %v", err, context.Canceled)
	}

	execMapper, err := runnerpkg.NewTemplateMapper("{{call .ID}}")
	if err != nil {
		t.Fatalf("NewTemplateMapper execution-error fixture returned error: %v", err)
	}
	if _, err := execMapper.MapInput(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "manual://local", Type: "manual.requested"},
		automationevent.RouteDecision{},
	); err == nil {
		t.Fatalf("MapInput should surface template execution errors")
	}
}

func TestRunnerHandleEventRejectsInvalidSetupBeforeRunningAgent(t *testing.T) {
	t.Parallel()

	validEvent := automationevent.Event{ID: "evt-1", Source: "manual://local", Type: "manual.requested"}
	var nilCtx context.Context
	if err := (runnerpkg.Runner{}).HandleEvent(nilCtx, validEvent); err == nil {
		t.Fatalf("HandleEvent should reject a nil context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (runnerpkg.Runner{}).HandleEvent(ctx, validEvent); !errors.Is(err, context.Canceled) {
		t.Fatalf("HandleEvent canceled error = %v, want %v", err, context.Canceled)
	}
	if err := (runnerpkg.Runner{}).HandleEvent(context.Background(), automationevent.Event{}); err == nil {
		t.Fatalf("HandleEvent should reject invalid events")
	}
	if err := (runnerpkg.Runner{Store: automationstore.NewMemoryRunStore()}).HandleEvent(context.Background(), validEvent); err == nil {
		t.Fatalf("HandleEvent should reject a nil router")
	}
	if err := (runnerpkg.Runner{Router: automationevent.StaticRouter{Decision: automationevent.RouteDecision{}}}).HandleEvent(context.Background(), validEvent); err == nil {
		t.Fatalf("HandleEvent should reject a nil store")
	}

	mapperErr := errors.New("mapper failed")
	store := automationstore.NewMemoryRunStore()
	runner := runnerpkg.Runner{
		Router: automationevent.StaticRouter{Decision: automationevent.RouteDecision{LoopName: "manual-loop"}},
		Store:  store,
		Mapper: runnerpkg.InputMapperFunc(func(context.Context, automationevent.Event, automationevent.RouteDecision) (*message.Message, error) {
			return nil, mapperErr
		}),
	}
	if err := runner.HandleEvent(context.Background(), validEvent); !errors.Is(err, mapperErr) {
		t.Fatalf("HandleEvent mapper error = %v, want %v", err, mapperErr)
	}
	runs := store.Runs()
	if len(runs) != 1 || runs[0].Error == "" {
		t.Fatalf("mapper failure should still record a failed run: %#v", runs)
	}
}
