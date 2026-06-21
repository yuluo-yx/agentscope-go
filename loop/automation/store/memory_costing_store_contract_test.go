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

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	storepkg "github.com/yuluo-yx/agentscope-go/loop/automation/store"
)

func TestMemoryRunStoreHandlesZeroValueSnapshotsAndBudgetEdges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := &storepkg.MemoryRunStore{}
	evt := automationevent.Event{
		ID:         "evt-1",
		Source:     "schedule://nightly",
		Type:       automationevent.EventTypeScheduleTick,
		DedupKey:   "nightly:2026-06-21",
		Extensions: map[string]string{"trace": "initial"},
	}
	if err := store.RecordEvent(ctx, evt); err != nil {
		t.Fatalf("RecordEvent returned error: %v", err)
	}
	if err := store.RecordEvent(ctx, evt); err != nil {
		t.Fatalf("duplicate RecordEvent returned error: %v", err)
	}
	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("duplicate events should be de-duplicated, got %#v", events)
	}
	events[0].Extensions["trace"] = "mutated"
	if store.Events()[0].Extensions["trace"] != "initial" {
		t.Fatalf("Events should return cloned event snapshots")
	}
	seenByID, err := store.SeenEvent(ctx, evt.ID)
	if err != nil {
		t.Fatalf("SeenEvent by id returned error: %v", err)
	}
	if !seenByID {
		t.Fatalf("event id should be tracked when a dedup key is present")
	}

	finishedAt := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	if err := store.RecordRun(ctx, storepkg.RunRecord{
		ID:                "run-finished",
		EventType:         evt.Type,
		LoopName:          "nightly-loop",
		WorkspaceMetadata: map[string]string{"root": "/tmp/work"},
		GateMetadata:      map[string]string{"rule": "nightly"},
		ModelCalls:        2,
		ToolCalls:         3,
		InputTokens:       11,
		OutputTokens:      7,
		EstimatedCost:     1.5,
		FinishedAt:        finishedAt,
	}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	runs := store.Runs()
	runs[0].WorkspaceMetadata["root"] = "mutated"
	if store.Runs()[0].WorkspaceMetadata["root"] != "/tmp/work" {
		t.Fatalf("Runs should return cloned metadata")
	}

	usage, err := store.BudgetUsage(ctx, storepkg.BudgetWindow{
		Start: finishedAt.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("BudgetUsage returned error: %v", err)
	}
	if usage.Runs != 1 || usage.TotalTokens != 18 || usage.EstimatedCost != 1.5 {
		t.Fatalf("BudgetUsage should include finished-at runs without an end time: %#v", usage)
	}
	if _, err := store.BudgetUsage(ctx, storepkg.BudgetWindow{
		Start: finishedAt,
		End:   finishedAt,
	}); err == nil {
		t.Fatalf("BudgetUsage should reject an end time that is not after start")
	}

	var nilStore *storepkg.MemoryRunStore
	if nilStore.Events() != nil || nilStore.Runs() != nil || nilStore.Reports() != nil {
		t.Fatalf("nil store snapshots should be nil")
	}
}

func TestCostingRunStoreDelegatesAndRejectsInvalidCost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := storepkg.NewMemoryRunStore()
	evt := automationevent.Event{
		ID:       "evt-1",
		Source:   "manual://operator",
		Type:     "manual.requested",
		DedupKey: "manual:1",
	}
	costing := &storepkg.CostingRunStore{
		Store: base,
		Estimator: storepkg.CostEstimatorFunc(func(_ context.Context, record storepkg.RunRecord) (float64, error) {
			if record.ID != "run-1" {
				t.Fatalf("estimator saw unexpected run: %#v", record)
			}
			return 4.25, nil
		}),
	}
	if err := costing.RecordEvent(ctx, evt); err != nil {
		t.Fatalf("RecordEvent delegated error: %v", err)
	}
	seen, err := costing.SeenEvent(ctx, evt.DedupKey)
	if err != nil {
		t.Fatalf("SeenEvent delegated error: %v", err)
	}
	if !seen {
		t.Fatalf("SeenEvent should delegate to the base store")
	}
	if err := costing.RecordRun(ctx, storepkg.RunRecord{ID: "run-1"}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	if got := base.Runs()[0].EstimatedCost; got != 4.25 {
		t.Fatalf("estimated cost = %v, want 4.25", got)
	}

	if _, err := (storepkg.CostEstimatorFunc)(nil).EstimateRunCost(ctx, storepkg.RunRecord{}); err == nil {
		t.Fatalf("nil CostEstimatorFunc should fail")
	}
	if err := (&storepkg.CostingRunStore{}).RecordRun(ctx, storepkg.RunRecord{ID: "run-2"}); err == nil {
		t.Fatalf("CostingRunStore without a base store should fail")
	}
	estimatorErr := errors.New("rate card unavailable")
	failing := &storepkg.CostingRunStore{
		Store: base,
		Estimator: storepkg.CostEstimatorFunc(func(context.Context, storepkg.RunRecord) (float64, error) {
			return 0, estimatorErr
		}),
	}
	if err := failing.RecordRun(ctx, storepkg.RunRecord{ID: "run-3"}); !errors.Is(err, estimatorErr) {
		t.Fatalf("RecordRun estimator error = %v, want %v", err, estimatorErr)
	}
	negative := &storepkg.CostingRunStore{
		Store: base,
		Estimator: storepkg.CostEstimatorFunc(func(context.Context, storepkg.RunRecord) (float64, error) {
			return -1, nil
		}),
	}
	if err := negative.RecordRun(ctx, storepkg.RunRecord{ID: "run-4"}); err == nil {
		t.Fatalf("RecordRun should reject a negative estimated cost")
	}
}
