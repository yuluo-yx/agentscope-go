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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yuluo-yx/agentscope-go/loop/automation/event"
)

// RunStore records automation events and Agent runs.
type RunStore interface {
	SeenEvent(context.Context, string) (bool, error)
	RecordEvent(context.Context, event.Event) error
	RecordRun(context.Context, RunRecord) error
}

// RunRecord is the audit record for one Agent run triggered by an event.
type RunRecord struct {
	ID                string
	EventID           string
	EventSource       string
	EventType         string
	DedupKey          string
	LoopName          string
	AgentName         string
	ReplyID           string
	SessionID         string
	WorkspaceRoot     string
	WorkspaceMetadata map[string]string
	GateReason        string
	GateMetadata      map[string]string
	ModelCalls        int
	ToolCalls         int
	InputTokens       int
	OutputTokens      int
	EstimatedCost     float64
	StartedAt         time.Time
	FinishedAt        time.Time
	StopReason        string
	Error             string
}

// BudgetWindow 描述自动化预算统计的时间窗口。
type BudgetWindow struct {
	Start     time.Time
	End       time.Time
	EventType string
	LoopName  string
}

// BudgetUsage 汇总一个预算窗口内的 run 用量。
type BudgetUsage struct {
	Runs          int
	ModelCalls    int
	ToolCalls     int
	InputTokens   int
	OutputTokens  int
	TotalTokens   int
	EstimatedCost float64
}

// MemoryRunStore stores event and run records in memory.
type MemoryRunStore struct {
	mu      sync.Mutex
	seen    map[string]struct{}
	events  []event.Event
	runs    []RunRecord
	reports []LoopReport
}

// NewMemoryRunStore creates an empty in-memory run store.
func NewMemoryRunStore() *MemoryRunStore {
	return &MemoryRunStore{
		seen:    map[string]struct{}{},
		events:  []event.Event{},
		runs:    []RunRecord{},
		reports: []LoopReport{},
	}
}

// SeenEvent reports whether the de-duplication key has already been recorded.
func (s *MemoryRunStore) SeenEvent(ctx context.Context, key string) (bool, error) {
	if err := checkStoreInput(ctx); err != nil {
		return false, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false, fmt.Errorf("automation: event key is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	_, ok := s.seen[key]
	return ok, nil
}

// RecordEvent stores an event under its de-duplication key.
func (s *MemoryRunStore) RecordEvent(ctx context.Context, event event.Event) error {
	if err := checkStoreInput(ctx); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	key := event.DeduplicationKey()
	if key == "" {
		return fmt.Errorf("automation: event key is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	cp := event.Clone()
	if _, ok := s.seen[key]; !ok {
		s.events = append(s.events, cp)
	}
	s.seen[key] = struct{}{}
	if event.DedupKey != "" {
		s.seen[event.ID] = struct{}{}
	}
	return nil
}

// RecordRun stores one run record.
func (s *MemoryRunStore) RecordRun(ctx context.Context, record RunRecord) error {
	if err := checkStoreInput(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("automation: run id is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	s.runs = append(s.runs, cloneRunRecord(record))
	return nil
}

// RecordReport stores one report.
func (s *MemoryRunStore) RecordReport(ctx context.Context, report LoopReport) error {
	if err := checkStoreInput(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(report.RunID) == "" {
		return fmt.Errorf("automation: report run id is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	s.reports = append(s.reports, report)
	return nil
}

// Events returns a snapshot of recorded events.
func (s *MemoryRunStore) Events() []event.Event {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	events := make([]event.Event, 0, len(s.events))
	for _, event := range s.events {
		events = append(events, event.Clone())
	}
	return events
}

// Runs returns a snapshot of recorded runs.
func (s *MemoryRunStore) Runs() []RunRecord {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	runs := make([]RunRecord, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, cloneRunRecord(run))
	}
	return runs
}

// Reports returns a snapshot of recorded reports.
func (s *MemoryRunStore) Reports() []LoopReport {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	return append([]LoopReport(nil), s.reports...)
}

// BudgetUsage 返回指定窗口内的聚合 run 用量。
func (s *MemoryRunStore) BudgetUsage(ctx context.Context, window BudgetWindow) (BudgetUsage, error) {
	if err := checkStoreInput(ctx); err != nil {
		return BudgetUsage{}, err
	}
	if err := validateBudgetWindow(window); err != nil {
		return BudgetUsage{}, err
	}
	if s == nil {
		return BudgetUsage{}, fmt.Errorf("automation: run store is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	var usage BudgetUsage
	for _, run := range s.runs {
		if !window.includes(run) {
			continue
		}
		usage.addRun(run)
	}
	return usage, nil
}

func (s *MemoryRunStore) ensureLocked() {
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	if s.events == nil {
		s.events = []event.Event{}
	}
	if s.runs == nil {
		s.runs = []RunRecord{}
	}
	if s.reports == nil {
		s.reports = []LoopReport{}
	}
}

func checkStoreInput(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("automation: context is nil")
	}
	return ctx.Err()
}

func cloneRunRecord(record RunRecord) RunRecord {
	record.WorkspaceMetadata = event.CloneStringMap(record.WorkspaceMetadata)
	record.GateMetadata = event.CloneStringMap(record.GateMetadata)
	return record
}

func validateBudgetWindow(window BudgetWindow) error {
	if window.Start.IsZero() {
		return fmt.Errorf("automation: budget window start is zero")
	}
	if !window.End.IsZero() && !window.End.After(window.Start) {
		return fmt.Errorf("automation: budget window end must be after start")
	}
	return nil
}

func (w BudgetWindow) includes(run RunRecord) bool {
	if w.EventType != "" && run.EventType != w.EventType {
		return false
	}
	if w.LoopName != "" && run.LoopName != w.LoopName {
		return false
	}
	return w.containsTime(runTime(run))
}

func (w BudgetWindow) containsTime(at time.Time) bool {
	if at.IsZero() || at.Before(w.Start) {
		return false
	}
	return w.End.IsZero() || at.Before(w.End)
}

func runTime(run RunRecord) time.Time {
	if !run.StartedAt.IsZero() {
		return run.StartedAt
	}
	return run.FinishedAt
}

func (u *BudgetUsage) addRun(run RunRecord) {
	u.Runs++
	u.ModelCalls += run.ModelCalls
	u.ToolCalls += run.ToolCalls
	u.InputTokens += run.InputTokens
	u.OutputTokens += run.OutputTokens
	u.TotalTokens += run.InputTokens + run.OutputTokens
	u.EstimatedCost += run.EstimatedCost
}
