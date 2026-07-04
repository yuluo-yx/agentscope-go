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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
)

// FileRunStore records events, runs, reports, and findings under one directory.
type FileRunStore struct {
	root string
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewFileRunStore creates or opens a file-backed run store.
func NewFileRunStore(root string) (*FileRunStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("automation: file run store root is empty")
	}
	store := &FileRunStore{root: root, seen: map[string]struct{}{}}
	if err := os.MkdirAll(filepath.Join(root, "reports"), 0o755); err != nil {
		return nil, fmt.Errorf("automation: create file run store: %w", err)
	}
	if err := store.loadSeen(); err != nil {
		return nil, err
	}
	return store, nil
}

// SeenEvent reports whether a de-duplication key has been recorded.
func (s *FileRunStore) SeenEvent(ctx context.Context, key string) (bool, error) {
	if err := checkStoreInput(ctx); err != nil {
		return false, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false, fmt.Errorf("automation: event key is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[key]
	return ok, nil
}

// RecordEvent appends an event to events.jsonl.
func (s *FileRunStore) RecordEvent(ctx context.Context, event event.Event) error {
	if err := checkStoreInput(ctx); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	key := event.DeduplicationKey()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := appendJSONLine(s.path("events.jsonl"), event.Clone()); err != nil {
		return err
	}
	s.seen[key] = struct{}{}
	if event.DedupKey != "" {
		s.seen[event.ID] = struct{}{}
	}
	return nil
}

// RecordRun appends a run record to runs.jsonl.
func (s *FileRunStore) RecordRun(ctx context.Context, record RunRecord) error {
	if err := checkStoreInput(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("automation: run id is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return appendJSONLine(s.path("runs.jsonl"), record)
}

// RecordFinding appends a finding to findings.jsonl.
func (s *FileRunStore) RecordFinding(ctx context.Context, finding Finding) error {
	if err := checkStoreInput(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(finding.ID) == "" {
		return fmt.Errorf("automation: finding id is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return appendJSONLine(s.path("findings.jsonl"), finding)
}

// RecordReport writes a markdown report under reports/<run-id>.md.
func (s *FileRunStore) RecordReport(ctx context.Context, report LoopReport) error {
	if err := checkStoreInput(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(report.RunID) == "" {
		return fmt.Errorf("automation: report run id is empty")
	}
	reportName, err := safeReportName(report.RunID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.root, "reports", reportName)
	return os.WriteFile(path, []byte(renderReport(report)), 0o600)
}

// BudgetUsage returns aggregated run usage within the specified window.
func (s *FileRunStore) BudgetUsage(ctx context.Context, window BudgetWindow) (usage BudgetUsage, err error) {
	if err := checkStoreInput(ctx); err != nil {
		return BudgetUsage{}, err
	}
	if err := validateBudgetWindow(window); err != nil {
		return BudgetUsage{}, err
	}
	if s == nil {
		return BudgetUsage{}, fmt.Errorf("automation: file run store is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.Open(s.path("runs.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return BudgetUsage{}, nil
		}
		return BudgetUsage{}, fmt.Errorf("automation: open runs store: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("automation: close runs store: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return BudgetUsage{}, err
		}
		var run RunRecord
		if err := json.Unmarshal(scanner.Bytes(), &run); err != nil {
			return BudgetUsage{}, fmt.Errorf("automation: decode stored run: %w", err)
		}
		if window.includes(run) {
			usage.addRun(run)
		}
	}
	if err := scanner.Err(); err != nil {
		return BudgetUsage{}, fmt.Errorf("automation: scan runs store: %w", err)
	}
	return usage, nil
}

func (s *FileRunStore) path(name string) string {
	return filepath.Join(s.root, name)
}

func (s *FileRunStore) loadSeen() (err error) {
	path := s.path("events.jsonl")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("automation: open events store: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("automation: close events store: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event event.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("automation: decode stored event: %w", err)
		}
		if key := event.DeduplicationKey(); key != "" {
			s.seen[key] = struct{}{}
		}
		if event.DedupKey != "" {
			s.seen[event.ID] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("automation: scan events store: %w", err)
	}
	return nil
}

func appendJSONLine(path string, value any) (err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("automation: open jsonl store: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("automation: close jsonl store: %w", closeErr)
		}
	}()

	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("automation: encode jsonl record: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("automation: write jsonl record: %w", err)
	}
	return nil
}

func safeReportName(runID string) (string, error) {
	runID = strings.TrimSpace(runID)
	if runID == "." || runID == ".." || filepath.Base(runID) != runID {
		return "", fmt.Errorf("automation: report run id %q is not a safe file name", runID)
	}
	return runID + ".md", nil
}

func renderReport(report LoopReport) string {
	var builder strings.Builder
	builder.WriteString("# Loop Report\n\n")
	writeReportField(&builder, "Run ID", report.RunID)
	writeReportField(&builder, "Event ID", report.EventID)
	writeReportField(&builder, "Loop", report.LoopName)
	writeReportField(&builder, "Stop Reason", report.StopReason)
	if report.Verified {
		writeReportField(&builder, "Verified", "true")
	} else {
		writeReportField(&builder, "Verified", "false")
	}
	writeReportSection(&builder, "Summary", []string{report.Summary})
	writeReportSection(&builder, "Changes", report.Changes)
	writeReportSection(&builder, "Evidence", report.Evidence)
	writeReportSection(&builder, "Blockers", report.Blockers)
	writeReportSection(&builder, "Risks", report.Risks)
	writeReportSection(&builder, "Next Action", []string{report.NextAction})
	return builder.String()
}

func writeReportField(builder *strings.Builder, name, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	builder.WriteString("- ")
	builder.WriteString(name)
	builder.WriteString(": ")
	builder.WriteString(value)
	builder.WriteString("\n")
}

func writeReportSection(builder *strings.Builder, name string, values []string) {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			filtered = append(filtered, value)
		}
	}
	if len(filtered) == 0 {
		return
	}
	builder.WriteString("\n## ")
	builder.WriteString(name)
	builder.WriteString("\n\n")
	for _, value := range filtered {
		builder.WriteString("- ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
}
