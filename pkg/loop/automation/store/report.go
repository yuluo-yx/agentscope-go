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

package store

import (
	"context"
	"time"
)

// ReportRecorder records human-readable loop reports.
type ReportRecorder interface {
	RecordReport(context.Context, LoopReport) error
}

// FindingRecorder records discovery results for triage.
type FindingRecorder interface {
	RecordFinding(context.Context, Finding) error
}

// LoopReport is the human-readable summary for one loop run.
type LoopReport struct {
	RunID      string
	EventID    string
	LoopName   string
	Summary    string
	Changes    []string
	Evidence   []string
	Blockers   []string
	Risks      []string
	NextAction string
	StopReason string
	Verified   bool
	StartedAt  time.Time
	FinishedAt time.Time
}

// CloneLoopReport returns a deep copy of a report.
func CloneLoopReport(report LoopReport) LoopReport {
	report.Changes = append([]string(nil), report.Changes...)
	report.Evidence = append([]string(nil), report.Evidence...)
	report.Blockers = append([]string(nil), report.Blockers...)
	report.Risks = append([]string(nil), report.Risks...)
	return report
}

// FindingStatus is the lifecycle state of a triage finding.
type FindingStatus string

const (
	FindingOpen      FindingStatus = "open"
	FindingAccepted  FindingStatus = "accepted"
	FindingDismissed FindingStatus = "dismissed"
	FindingRunning   FindingStatus = "running"
	FindingDone      FindingStatus = "done"
)

// Finding is a generic discovery item, independent of any issue tracker.
type Finding struct {
	ID          string
	EventID     string
	LoopName    string
	Title       string
	Description string
	Evidence    []string
	Severity    string
	Labels      []string
	Status      FindingStatus
	NextAction  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
