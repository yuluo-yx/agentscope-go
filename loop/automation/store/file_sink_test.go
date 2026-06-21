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

	automationgoal "github.com/yuluo-yx/agentscope-go/loop/automation/goal"
	storepkg "github.com/yuluo-yx/agentscope-go/loop/automation/store"
)

func TestFileSinkPublishesRunAndReport(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sink, err := storepkg.NewFileSink(root)
	if err != nil {
		t.Fatalf("NewFileSink returned error: %v", err)
	}
	run := storepkg.RunRecord{
		ID:            "run-1",
		EventID:       "evt-1",
		LoopName:      "daily-triage",
		AgentName:     "maker",
		StopReason:    automationgoal.GoalStopCompleted,
		WorkspaceRoot: "/workspace/run-1",
		StartedAt:     time.Now(),
		FinishedAt:    time.Now(),
	}
	report := storepkg.LoopReport{
		RunID:      "run-1",
		EventID:    "evt-1",
		LoopName:   "daily-triage",
		Summary:    "all checks passed",
		Evidence:   []string{"go test ./..."},
		NextAction: "done",
		StopReason: automationgoal.GoalStopCompleted,
		Verified:   true,
	}

	if err := sink.PublishRun(context.Background(), run, report); err != nil {
		t.Fatalf("PublishRun returned error: %v", err)
	}

	runsData, err := os.ReadFile(filepath.Join(root, "runs.jsonl"))
	if err != nil {
		t.Fatalf("read runs.jsonl: %v", err)
	}
	if !strings.Contains(string(runsData), `"ID":"run-1"`) ||
		!strings.Contains(string(runsData), `"WorkspaceRoot":"/workspace/run-1"`) {
		t.Fatalf("runs.jsonl missing run record: %s", string(runsData))
	}
	reportData, err := os.ReadFile(filepath.Join(root, "reports", "run-1.md"))
	if err != nil {
		t.Fatalf("read report markdown: %v", err)
	}
	reportText := string(reportData)
	if !strings.Contains(reportText, "all checks passed") ||
		!strings.Contains(reportText, "go test ./...") ||
		!strings.Contains(reportText, "Verified: true") {
		t.Fatalf("report markdown missing fields: %s", reportText)
	}
}

func TestFileSinkRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := storepkg.NewFileSink(""); err == nil {
		t.Fatalf("NewFileSink should reject empty root")
	}
	sink, err := storepkg.NewFileSink(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileSink returned error: %v", err)
	}
	if err := sink.PublishRun(context.Background(), storepkg.RunRecord{}, storepkg.LoopReport{}); err == nil {
		t.Fatalf("PublishRun should reject empty run id")
	}
	err = sink.PublishRun(
		context.Background(),
		storepkg.RunRecord{ID: "../escape"},
		storepkg.LoopReport{RunID: "../escape"},
	)
	if err == nil {
		t.Fatalf("PublishRun should reject unsafe run id")
	}
}
