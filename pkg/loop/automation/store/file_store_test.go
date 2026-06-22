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
	"os"
	"path/filepath"
	"testing"
	"time"

	automationevent "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
	automationgoal "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/goal"
	storepkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/store"
)

func TestFileRunStorePersistsEventsRunsReportsAndFindings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := storepkg.NewFileRunStore(root)
	if err != nil {
		t.Fatalf("NewFileRunStore returned error: %v", err)
	}
	event := automationevent.Event{
		ID:       "evt-1",
		Source:   "schedule://daily-triage",
		Type:     automationevent.EventTypeScheduleTick,
		DedupKey: "daily-triage:today",
	}
	if err := store.RecordEvent(ctx, event); err != nil {
		t.Fatalf("RecordEvent returned error: %v", err)
	}
	if err := store.RecordRun(ctx, storepkg.RunRecord{
		ID:         "run-1",
		EventID:    event.ID,
		DedupKey:   event.DedupKey,
		LoopName:   "daily-triage",
		AgentName:  "Friday",
		StopReason: automationgoal.GoalStopCompleted,
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	report := storepkg.LoopReport{
		RunID:      "run-1",
		EventID:    event.ID,
		LoopName:   "daily-triage",
		Summary:    "No blocking issue found.",
		Evidence:   []string{"scripted check"},
		NextAction: "keep monitoring",
		StopReason: automationgoal.GoalStopCompleted,
		Verified:   true,
	}
	if err := store.RecordReport(ctx, report); err != nil {
		t.Fatalf("RecordReport returned error: %v", err)
	}
	finding := storepkg.Finding{
		ID:          "finding-1",
		EventID:     event.ID,
		LoopName:    "daily-triage",
		Title:       "CI failure",
		Description: "The scheduled scan found a failing workflow.",
		Status:      storepkg.FindingOpen,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.RecordFinding(ctx, finding); err != nil {
		t.Fatalf("RecordFinding returned error: %v", err)
	}

	reopened, err := storepkg.NewFileRunStore(root)
	if err != nil {
		t.Fatalf("reopen NewFileRunStore returned error: %v", err)
	}
	seen, err := reopened.SeenEvent(ctx, event.DedupKey)
	if err != nil {
		t.Fatalf("SeenEvent returned error: %v", err)
	}
	if !seen {
		t.Fatalf("reopened store should remember event dedup key")
	}
	for _, path := range []string{
		filepath.Join(root, "events.jsonl"),
		filepath.Join(root, "runs.jsonl"),
		filepath.Join(root, "findings.jsonl"),
		filepath.Join(root, "reports", "run-1.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected persisted file %s: %v", path, err)
		}
	}
	if err := reopened.RecordReport(ctx, storepkg.LoopReport{RunID: "../escape"}); err == nil {
		t.Fatalf("RecordReport should reject unsafe run id")
	}
}

func TestFileRunStoreSummarizesBudgetUsage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storepkg.NewFileRunStore(root)
	if err != nil {
		t.Fatalf("NewFileRunStore returned error: %v", err)
	}
	start := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	for _, run := range []storepkg.RunRecord{
		{ID: "run-1", EventType: "release.requested", LoopName: "release-loop", StartedAt: start.Add(time.Hour), ModelCalls: 1, ToolCalls: 2, InputTokens: 10, OutputTokens: 4, EstimatedCost: 1.25},
		{ID: "run-2", EventType: "review.requested", LoopName: "review-loop", StartedAt: start.Add(2 * time.Hour), ModelCalls: 2, ToolCalls: 1, InputTokens: 5, OutputTokens: 1, EstimatedCost: 0.75},
		{ID: "run-3", EventType: "release.requested", LoopName: "release-loop", StartedAt: start.Add(25 * time.Hour), InputTokens: 100, OutputTokens: 100, EstimatedCost: 100},
	} {
		if err := store.RecordRun(context.Background(), run); err != nil {
			t.Fatalf("RecordRun returned error: %v", err)
		}
	}

	reopened, err := storepkg.NewFileRunStore(root)
	if err != nil {
		t.Fatalf("reopen NewFileRunStore returned error: %v", err)
	}
	usage, err := reopened.BudgetUsage(context.Background(), storepkg.BudgetWindow{
		Start: start,
		End:   start.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("BudgetUsage returned error: %v", err)
	}
	if usage.Runs != 2 || usage.ModelCalls != 3 || usage.ToolCalls != 3 ||
		usage.InputTokens != 15 || usage.OutputTokens != 5 ||
		usage.TotalTokens != 20 || usage.EstimatedCost != 2 {
		t.Fatalf("BudgetUsage mismatch: %#v", usage)
	}

	releaseUsage, err := reopened.BudgetUsage(context.Background(), storepkg.BudgetWindow{
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

	reviewUsage, err := reopened.BudgetUsage(context.Background(), storepkg.BudgetWindow{
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
