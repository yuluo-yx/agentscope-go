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
	"strings"
	"testing"
	"time"

	storepkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/store"
)

func TestCloneLoopReportCopiesMutableSections(t *testing.T) {
	t.Parallel()

	report := storepkg.LoopReport{
		RunID:    "run-1",
		Changes:  []string{"changed loop"},
		Evidence: []string{"go test"},
		Blockers: []string{"approval"},
		Risks:    []string{"timeout"},
	}

	clone := storepkg.CloneLoopReport(report)
	clone.Changes[0] = "mutated"
	clone.Evidence[0] = "mutated"
	clone.Blockers[0] = "mutated"
	clone.Risks[0] = "mutated"

	if report.Changes[0] != "changed loop" || report.Evidence[0] != "go test" ||
		report.Blockers[0] != "approval" || report.Risks[0] != "timeout" {
		t.Fatalf("CloneLoopReport did not isolate slices: %#v", report)
	}
}

func TestMemoryRunStoreRecordsReportsAndDefendsSnapshots(t *testing.T) {
	t.Parallel()

	store := storepkg.NewMemoryRunStore()
	report := storepkg.LoopReport{
		RunID:      "run-1",
		EventID:    "evt-1",
		LoopName:   "release-check",
		Summary:    "verified",
		Evidence:   []string{"go test ./..."},
		Verified:   true,
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}
	if err := store.RecordReport(context.Background(), report); err != nil {
		t.Fatalf("RecordReport returned error: %v", err)
	}
	reports := store.Reports()
	if len(reports) != 1 || reports[0].RunID != "run-1" {
		t.Fatalf("Reports mismatch: %#v", reports)
	}
	reports[0].RunID = "mutated"
	if store.Reports()[0].RunID != "run-1" {
		t.Fatalf("Reports should return snapshot copies")
	}

	if err := store.RecordReport(context.Background(), storepkg.LoopReport{}); err == nil ||
		!strings.Contains(err.Error(), "report run id is empty") {
		t.Fatalf("empty report error = %v, want report run id is empty", err)
	}
	if reports := (*storepkg.MemoryRunStore)(nil).Reports(); reports != nil {
		t.Fatalf("nil MemoryRunStore Reports = %#v, want nil", reports)
	}
}

func TestSinkFuncClonesInputsAndMultiSinkJoinsErrors(t *testing.T) {
	t.Parallel()

	run := storepkg.RunRecord{
		ID:                "run-1",
		WorkspaceMetadata: map[string]string{"root": "/tmp/work"},
		GateMetadata:      map[string]string{"gate": "release"},
	}
	report := storepkg.LoopReport{RunID: "run-1", Evidence: []string{"go test"}}
	var capturedRun storepkg.RunRecord
	var capturedReport storepkg.LoopReport
	sink := storepkg.SinkFunc(func(_ context.Context, run storepkg.RunRecord, report storepkg.LoopReport) error {
		capturedRun = run
		capturedReport = report
		run.WorkspaceMetadata["root"] = "mutated"
		run.GateMetadata["gate"] = "mutated"
		report.Evidence[0] = "mutated"
		return nil
	})
	if err := sink.PublishRun(context.Background(), run, report); err != nil {
		t.Fatalf("PublishRun returned error: %v", err)
	}
	if capturedRun.ID != "run-1" || capturedReport.RunID != "run-1" {
		t.Fatalf("captured run/report mismatch: %#v %#v", capturedRun, capturedReport)
	}
	if run.WorkspaceMetadata["root"] != "/tmp/work" || run.GateMetadata["gate"] != "release" || report.Evidence[0] != "go test" {
		t.Fatalf("SinkFunc should clone run and report before publishing: %#v %#v", run, report)
	}
	if err := (storepkg.SinkFunc(nil)).PublishRun(context.Background(), run, report); err == nil ||
		!strings.Contains(err.Error(), "sink is nil") {
		t.Fatalf("nil SinkFunc error = %v, want sink is nil", err)
	}

	firstErr := errors.New("first sink failed")
	secondErr := errors.New("second sink failed")
	err := (storepkg.MultiSink{
		nil,
		storepkg.SinkFunc(func(context.Context, storepkg.RunRecord, storepkg.LoopReport) error { return firstErr }),
		storepkg.SinkFunc(func(context.Context, storepkg.RunRecord, storepkg.LoopReport) error { return secondErr }),
	}).PublishRun(context.Background(), run, report)
	if err == nil || !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("MultiSink error = %v, want joined sink errors", err)
	}
}
