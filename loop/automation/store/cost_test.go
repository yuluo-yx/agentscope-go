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

	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	storepkg "github.com/yuluo-yx/agentscope-go/loop/automation/store"
)

func TestCostingRunStoreEstimatesCostBeforeRecordingRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := storepkg.NewMemoryRunStore()
	costing := storepkg.CostingRunStore{
		Store: store,
		Estimator: storepkg.CostEstimatorFunc(func(_ context.Context, record storepkg.RunRecord) (float64, error) {
			if record.InputTokens != 40 || record.OutputTokens != 10 {
				t.Fatalf("estimator received record mismatch: %#v", record)
			}
			return 0.125, nil
		}),
	}

	if err := costing.RecordRun(ctx, storepkg.RunRecord{ID: "run-1", InputTokens: 40, OutputTokens: 10}); err != nil {
		t.Fatalf("RecordRun returned error: %v", err)
	}
	runs := store.Runs()
	if len(runs) != 1 {
		t.Fatalf("recorded runs = %d, want 1", len(runs))
	}
	if runs[0].EstimatedCost != 0.125 {
		t.Fatalf("EstimatedCost = %v, want 0.125", runs[0].EstimatedCost)
	}
}

func TestCostingRunStoreReturnsEstimatorErrorWithoutRecordingRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := storepkg.NewMemoryRunStore()
	estimatorErr := errors.New("pricing unavailable")
	costing := storepkg.CostingRunStore{
		Store: store,
		Estimator: storepkg.CostEstimatorFunc(func(context.Context, storepkg.RunRecord) (float64, error) {
			return 0, estimatorErr
		}),
	}

	err := costing.RecordRun(ctx, storepkg.RunRecord{ID: "run-1"})
	if !errors.Is(err, estimatorErr) {
		t.Fatalf("RecordRun error = %v, want estimator error", err)
	}
	if len(store.Runs()) != 0 {
		t.Fatalf("run should not be recorded when estimator fails: %#v", store.Runs())
	}
}

func TestCostingRunStoreDelegatesEventOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := storepkg.NewMemoryRunStore()
	costing := storepkg.CostingRunStore{Store: store}
	event := automationevent.Event{
		ID:       "evt-1",
		Source:   "manual://cost",
		Type:     "manual.requested",
		DedupKey: "work-1",
	}

	if err := costing.RecordEvent(ctx, event); err != nil {
		t.Fatalf("RecordEvent returned error: %v", err)
	}
	seen, err := costing.SeenEvent(ctx, "work-1")
	if err != nil {
		t.Fatalf("SeenEvent returned error: %v", err)
	}
	if !seen {
		t.Fatalf("SeenEvent should delegate to wrapped store")
	}
}

var _ storepkg.RunStore = (*storepkg.CostingRunStore)(nil)
