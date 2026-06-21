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

package gate_test

import (
	"context"
	"testing"
	"time"

	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	gatepkg "github.com/yuluo-yx/agentscope-go/loop/automation/gate"
	automationgoal "github.com/yuluo-yx/agentscope-go/loop/automation/goal"
	automationstore "github.com/yuluo-yx/agentscope-go/loop/automation/store"
)

func TestBudgetGateStopsWhenDailyRunBudgetIsExceeded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	store := automationstore.NewMemoryRunStore()
	if err := store.RecordRun(context.Background(), automationstore.RunRecord{
		ID:        "run-1",
		StartedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	gate := gatepkg.BudgetGate{
		Store:  store,
		Budget: gatepkg.AutomationBudget{MaxRunsPerDay: 1},
		Now:    func() time.Time { return now },
	}

	decision, err := gate.Evaluate(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "manual://budget", Type: "manual.requested"},
		automationevent.RouteDecision{LoopName: "budget-loop"},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.StopReason != automationgoal.GoalStopWaitingUser ||
		decision.Metadata["budget"] != "runs_per_day" ||
		decision.Metadata["used"] != "1" ||
		decision.Metadata["limit"] != "1" {
		t.Fatalf("budget decision mismatch: %#v", decision)
	}
}

func TestBudgetGateStopsWhenDailyTokenBudgetIsExceeded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	store := automationstore.NewMemoryRunStore()
	if err := store.RecordRun(context.Background(), automationstore.RunRecord{
		ID:           "run-1",
		StartedAt:    now.Add(-time.Hour),
		InputTokens:  12,
		OutputTokens: 8,
	}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	gate := gatepkg.BudgetGate{
		Store:  store,
		Budget: gatepkg.AutomationBudget{MaxTokensPerDay: 20},
		Now:    func() time.Time { return now },
	}

	decision, err := gate.Evaluate(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "manual://budget", Type: "manual.requested"},
		automationevent.RouteDecision{},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.StopReason != automationgoal.GoalStopWaitingUser ||
		decision.Metadata["budget"] != "tokens_per_day" ||
		decision.Metadata["used"] != "20" ||
		decision.Metadata["limit"] != "20" {
		t.Fatalf("budget decision mismatch: %#v", decision)
	}
}

func TestBudgetGateStopsWhenDailyCostBudgetIsExceeded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	store := automationstore.NewMemoryRunStore()
	if err := store.RecordRun(context.Background(), automationstore.RunRecord{
		ID:            "run-1",
		StartedAt:     now.Add(-time.Hour),
		EstimatedCost: 12.5,
	}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	gate := gatepkg.BudgetGate{
		Store:  store,
		Budget: gatepkg.AutomationBudget{MaxCostPerDay: 12.5},
		Now:    func() time.Time { return now },
	}

	decision, err := gate.Evaluate(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "manual://budget", Type: "manual.requested"},
		automationevent.RouteDecision{},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.StopReason != automationgoal.GoalStopWaitingUser ||
		decision.Metadata["budget"] != "cost_per_day" ||
		decision.Metadata["used"] != "12.5" ||
		decision.Metadata["limit"] != "12.5" {
		t.Fatalf("budget decision mismatch: %#v", decision)
	}
}

func TestBudgetGateAllowsWhenUsageIsUnderBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	store := automationstore.NewMemoryRunStore()
	gate := gatepkg.BudgetGate{
		Store: store,
		Budget: gatepkg.AutomationBudget{
			MaxRunsPerDay:   1,
			MaxTokensPerDay: 20,
		},
		Now: func() time.Time { return now },
	}

	decision, err := gate.Evaluate(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "manual://budget", Type: "manual.requested"},
		automationevent.RouteDecision{},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.RequiresStop() {
		t.Fatalf("budget gate should allow under-budget event: %#v", decision)
	}
}

func TestBudgetGateStopsWhenEventTypeBudgetIsExceeded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	store := automationstore.NewMemoryRunStore()
	if err := store.RecordRun(context.Background(), automationstore.RunRecord{
		ID:           "run-1",
		EventType:    "release.requested",
		StartedAt:    now.Add(-time.Hour),
		InputTokens:  8,
		OutputTokens: 2,
	}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	gate := gatepkg.BudgetGate{
		Store: store,
		Budget: gatepkg.AutomationBudget{
			PerEventType: map[string]gatepkg.BudgetLimit{
				"release.requested": {MaxTokensPerDay: 10},
			},
		},
		Now: func() time.Time { return now },
	}

	decision, err := gate.Evaluate(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "manual://release", Type: "release.requested"},
		automationevent.RouteDecision{},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.StopReason != automationgoal.GoalStopWaitingUser ||
		decision.Metadata["scope"] != "event_type" ||
		decision.Metadata["scope_value"] != "release.requested" ||
		decision.Metadata["budget"] != "tokens_per_day" {
		t.Fatalf("event type budget decision mismatch: %#v", decision)
	}
}

func TestBudgetGateStopsWhenLoopBudgetIsExceeded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	store := automationstore.NewMemoryRunStore()
	if err := store.RecordRun(context.Background(), automationstore.RunRecord{
		ID:        "run-1",
		LoopName:  "release-loop",
		StartedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	gate := gatepkg.BudgetGate{
		Store: store,
		Budget: gatepkg.AutomationBudget{
			PerLoop: map[string]gatepkg.BudgetLimit{
				"release-loop": {MaxRunsPerDay: 1},
			},
		},
		Now: func() time.Time { return now },
	}

	decision, err := gate.Evaluate(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "manual://release", Type: "release.requested"},
		automationevent.RouteDecision{LoopName: "release-loop"},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.StopReason != automationgoal.GoalStopWaitingUser ||
		decision.Metadata["scope"] != "loop" ||
		decision.Metadata["scope_value"] != "release-loop" ||
		decision.Metadata["budget"] != "runs_per_day" {
		t.Fatalf("loop budget decision mismatch: %#v", decision)
	}
}

func TestBudgetGateStopsWhenLoopCostBudgetIsExceeded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	store := automationstore.NewMemoryRunStore()
	if err := store.RecordRun(context.Background(), automationstore.RunRecord{
		ID:            "run-1",
		LoopName:      "release-loop",
		StartedAt:     now.Add(-time.Hour),
		EstimatedCost: 4.2,
	}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	gate := gatepkg.BudgetGate{
		Store: store,
		Budget: gatepkg.AutomationBudget{
			PerLoop: map[string]gatepkg.BudgetLimit{
				"release-loop": {MaxCostPerDay: 4.2},
			},
		},
		Now: func() time.Time { return now },
	}

	decision, err := gate.Evaluate(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "manual://release", Type: "release.requested"},
		automationevent.RouteDecision{LoopName: "release-loop"},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.StopReason != automationgoal.GoalStopWaitingUser ||
		decision.Metadata["scope"] != "loop" ||
		decision.Metadata["scope_value"] != "release-loop" ||
		decision.Metadata["budget"] != "cost_per_day" {
		t.Fatalf("loop budget decision mismatch: %#v", decision)
	}
}
