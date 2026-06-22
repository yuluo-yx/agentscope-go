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
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileSink publishes run records and reports under one local directory.
type FileSink struct {
	root string
	mu   sync.Mutex
}

// NewFileSink creates a file-backed sink.
func NewFileSink(root string) (*FileSink, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("automation: file sink root is empty")
	}
	if err := os.MkdirAll(filepath.Join(root, "reports"), 0o755); err != nil {
		return nil, fmt.Errorf("automation: create file sink: %w", err)
	}
	return &FileSink{root: root}, nil
}

// PublishRun appends the run to runs.jsonl and writes reports/<run-id>.md.
func (s *FileSink) PublishRun(ctx context.Context, run RunRecord, report LoopReport) error {
	if s == nil {
		return fmt.Errorf("automation: file sink is nil")
	}
	if err := checkStoreInput(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(run.ID) == "" {
		return fmt.Errorf("automation: run id is empty")
	}
	if report.RunID == "" {
		report.RunID = run.ID
	}
	reportName, err := safeReportName(report.RunID)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := appendJSONLine(filepath.Join(s.root, "runs.jsonl"), cloneRunRecord(run)); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.root, "reports", reportName), []byte(renderReport(report)), 0o600)
}

var _ Sink = (*FileSink)(nil)
