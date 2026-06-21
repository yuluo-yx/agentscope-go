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
	"strings"
	"testing"
	"time"

	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	storepkg "github.com/yuluo-yx/agentscope-go/loop/automation/store"
)

func TestFileRunStoreRejectsInvalidInputsAndCorruptFiles(t *testing.T) {
	t.Parallel()

	if _, err := storepkg.NewFileRunStore("   "); err == nil {
		t.Fatalf("NewFileRunStore should reject an empty root")
	}

	ctx := context.Background()
	store, err := storepkg.NewFileRunStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileRunStore returned error: %v", err)
	}
	var nilCtx context.Context
	if _, err := store.SeenEvent(nilCtx, "evt-1"); err == nil {
		t.Fatalf("SeenEvent should reject a nil context")
	}
	if _, err := store.SeenEvent(ctx, "   "); err == nil {
		t.Fatalf("SeenEvent should reject an empty key")
	}
	if err := store.RecordEvent(ctx, automationevent.Event{ID: "evt-1"}); err == nil {
		t.Fatalf("RecordEvent should reject invalid events")
	}
	if err := store.RecordRun(ctx, storepkg.RunRecord{}); err == nil {
		t.Fatalf("RecordRun should reject an empty run id")
	}
	if err := store.RecordFinding(ctx, storepkg.Finding{}); err == nil {
		t.Fatalf("RecordFinding should reject an empty finding id")
	}
	if err := store.RecordReport(ctx, storepkg.LoopReport{}); err == nil {
		t.Fatalf("RecordReport should reject an empty run id")
	}

	validWindow := storepkg.BudgetWindow{Start: time.Now().Add(-time.Hour)}
	var nilFileStore *storepkg.FileRunStore
	if _, err := nilFileStore.BudgetUsage(ctx, validWindow); err == nil {
		t.Fatalf("BudgetUsage should reject a nil file store")
	}
	if _, err := store.BudgetUsage(nilCtx, validWindow); err == nil {
		t.Fatalf("BudgetUsage should reject a nil context")
	}
	if _, err := store.BudgetUsage(ctx, storepkg.BudgetWindow{}); err == nil {
		t.Fatalf("BudgetUsage should reject a zero start time")
	}
	emptyUsage, err := store.BudgetUsage(ctx, validWindow)
	if err != nil {
		t.Fatalf("BudgetUsage without runs returned error: %v", err)
	}
	if emptyUsage != (storepkg.BudgetUsage{}) {
		t.Fatalf("empty BudgetUsage mismatch: %#v", emptyUsage)
	}

	corruptEventsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(corruptEventsRoot, "events.jsonl"), []byte("{bad json}\n"), 0o644); err != nil {
		t.Fatalf("write corrupt events store: %v", err)
	}
	if _, err := storepkg.NewFileRunStore(corruptEventsRoot); err == nil || !strings.Contains(err.Error(), "decode stored event") {
		t.Fatalf("NewFileRunStore corrupt events error = %v", err)
	}

	corruptRunsRoot := t.TempDir()
	corruptRunsStore, err := storepkg.NewFileRunStore(corruptRunsRoot)
	if err != nil {
		t.Fatalf("NewFileRunStore corrupt runs root returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptRunsRoot, "runs.jsonl"), []byte("{bad json}\n"), 0o644); err != nil {
		t.Fatalf("write corrupt runs store: %v", err)
	}
	if _, err := corruptRunsStore.BudgetUsage(ctx, validWindow); err == nil || !strings.Contains(err.Error(), "decode stored run") {
		t.Fatalf("BudgetUsage corrupt runs error = %v", err)
	}
}

func TestFileRunStoreReportIncludesOnlyNonEmptySections(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := storepkg.NewFileRunStore(root)
	if err != nil {
		t.Fatalf("NewFileRunStore returned error: %v", err)
	}
	report := storepkg.LoopReport{
		RunID:      "run-report",
		EventID:    "evt-1",
		LoopName:   "release-loop",
		Summary:    "verified release readiness",
		Changes:    []string{"updated loop tests", "   "},
		Evidence:   []string{"make coverage"},
		Blockers:   []string{"none"},
		Risks:      []string{"coverage regression"},
		NextAction: "monitor CI",
		Verified:   true,
	}
	if err := store.RecordReport(context.Background(), report); err != nil {
		t.Fatalf("RecordReport returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "reports", "run-report.md"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"- Run ID: run-report",
		"- Event ID: evt-1",
		"- Loop: release-loop",
		"- Verified: true",
		"## Summary",
		"## Changes",
		"## Evidence",
		"## Blockers",
		"## Risks",
		"## Next Action",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report should contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "-    ") {
		t.Fatalf("report should filter blank list items:\n%s", text)
	}
}
