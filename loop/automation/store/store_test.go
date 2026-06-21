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
	"sync"
	"testing"
	"time"

	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	storepkg "github.com/yuluo-yx/agentscope-go/loop/automation/store"
)

func TestMemoryRunStoreRecordsEventsAndRunsConcurrently(t *testing.T) {
	t.Parallel()

	store := storepkg.NewMemoryRunStore()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.RecordEvent(ctx, automationevent.Event{ID: "evt-1", Source: "manual://user", Type: "manual.requested", DedupKey: "work-1"}); err != nil {
				t.Errorf("RecordEvent returned error: %v", err)
			}
			if err := store.RecordRun(ctx, storepkg.RunRecord{ID: "run-1", EventID: "evt-1", DedupKey: "work-1"}); err != nil {
				t.Errorf("RecordRun returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	seen, err := store.SeenEvent(ctx, "work-1")
	if err != nil {
		t.Fatalf("SeenEvent returned error: %v", err)
	}
	if !seen {
		t.Fatalf("SeenEvent should return true for recorded dedup key")
	}
	if len(store.Events()) != 1 {
		t.Fatalf("recorded events = %d, want one de-duplicated snapshot", len(store.Events()))
	}
	if len(store.Runs()) == 0 {
		t.Fatalf("store should retain event and run snapshots")
	}
}

func TestMemoryRunStoreSummarizesBudgetUsage(t *testing.T) {
	t.Parallel()

	store := storepkg.NewMemoryRunStore()
	ctx := context.Background()
	start := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	runs := []storepkg.RunRecord{
		{ID: "run-1", EventType: "release.requested", LoopName: "release-loop", StartedAt: start.Add(time.Hour), InputTokens: 10, OutputTokens: 4, EstimatedCost: 1.25},
		{ID: "run-2", EventType: "review.requested", LoopName: "review-loop", StartedAt: start.Add(2 * time.Hour), InputTokens: 5, OutputTokens: 1, EstimatedCost: 0.75},
		{ID: "run-3", EventType: "release.requested", LoopName: "release-loop", StartedAt: start.Add(25 * time.Hour), InputTokens: 100, OutputTokens: 100, EstimatedCost: 100},
	}
	for _, run := range runs {
		if err := store.RecordRun(ctx, run); err != nil {
			t.Fatalf("RecordRun returned error: %v", err)
		}
	}

	usage, err := store.BudgetUsage(ctx, storepkg.BudgetWindow{
		Start: start,
		End:   start.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("BudgetUsage returned error: %v", err)
	}
	if usage.Runs != 2 || usage.InputTokens != 15 || usage.OutputTokens != 5 ||
		usage.TotalTokens != 20 || usage.EstimatedCost != 2 {
		t.Fatalf("BudgetUsage mismatch: %#v", usage)
	}

	releaseUsage, err := store.BudgetUsage(ctx, storepkg.BudgetWindow{
		Start:     start,
		End:       start.Add(24 * time.Hour),
		EventType: "release.requested",
	})
	if err != nil {
		t.Fatalf("release BudgetUsage returned error: %v", err)
	}
	if releaseUsage.Runs != 1 || releaseUsage.TotalTokens != 14 || releaseUsage.EstimatedCost != 1.25 {
		t.Fatalf("release BudgetUsage mismatch: %#v", releaseUsage)
	}

	reviewUsage, err := store.BudgetUsage(ctx, storepkg.BudgetWindow{
		Start:    start,
		End:      start.Add(24 * time.Hour),
		LoopName: "review-loop",
	})
	if err != nil {
		t.Fatalf("review BudgetUsage returned error: %v", err)
	}
	if reviewUsage.Runs != 1 || reviewUsage.TotalTokens != 6 || reviewUsage.EstimatedCost != 0.75 {
		t.Fatalf("review BudgetUsage mismatch: %#v", reviewUsage)
	}
}
