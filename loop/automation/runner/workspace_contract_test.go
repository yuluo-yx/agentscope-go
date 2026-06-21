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
	"strings"
	"testing"

	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	runnerpkg "github.com/yuluo-yx/agentscope-go/loop/automation/runner"
)

func TestWorkspaceAllocatorFuncAndNoopAllocatorBoundaries(t *testing.T) {
	t.Parallel()

	_, err := (runnerpkg.WorkspaceAllocatorFunc(nil)).Allocate(context.Background(), automationevent.Event{}, automationevent.RouteDecision{})
	if err == nil || !strings.Contains(err.Error(), "workspace allocator is nil") {
		t.Fatalf("nil WorkspaceAllocatorFunc error = %v, want workspace allocator is nil", err)
	}

	allocated := false
	lease, err := (runnerpkg.WorkspaceAllocatorFunc(func(_ context.Context, event automationevent.Event, decision automationevent.RouteDecision) (runnerpkg.WorkspaceLease, error) {
		allocated = event.ID == "evt-1" && decision.LoopName == "triage"
		return runnerpkg.StaticWorkspaceLease{RootPath: "/tmp/triage", Values: map[string]string{"kind": "static"}}, nil
	})).Allocate(context.Background(), automationevent.Event{ID: "evt-1"}, automationevent.RouteDecision{LoopName: "triage"})
	if err != nil || !allocated || lease.Root() != "/tmp/triage" {
		t.Fatalf("WorkspaceAllocatorFunc result lease=%#v allocated=%v err=%v", lease, allocated, err)
	}

	noopLease, err := (runnerpkg.NoopWorkspaceAllocator{
		Root:     " ",
		Metadata: map[string]string{"profile": "local"},
	}).Allocate(context.Background(), automationevent.Event{}, automationevent.RouteDecision{})
	if err != nil {
		t.Fatalf("NoopWorkspaceAllocator returned error: %v", err)
	}
	if strings.TrimSpace(noopLease.Root()) == "" {
		t.Fatalf("NoopWorkspaceAllocator should fall back to working directory")
	}
	metadata := noopLease.Metadata()
	metadata["profile"] = "mutated"
	if noopLease.Metadata()["profile"] != "local" {
		t.Fatalf("StaticWorkspaceLease metadata should be cloned")
	}
	if err := noopLease.Close(context.Background()); err != nil {
		t.Fatalf("StaticWorkspaceLease Close returned error: %v", err)
	}
	var nilCtx context.Context
	if err := noopLease.Close(nilCtx); err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("StaticWorkspaceLease Close(nil) error = %v, want context is nil", err)
	}
}

func TestNoopWorkspaceAllocatorHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := (runnerpkg.NoopWorkspaceAllocator{Root: "/tmp/triage"}).Allocate(ctx, automationevent.Event{}, automationevent.RouteDecision{})
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("Allocate canceled error = %v, want context canceled", err)
	}
}
