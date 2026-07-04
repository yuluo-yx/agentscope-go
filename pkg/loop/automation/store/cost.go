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

	"github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
)

// CostEstimator estimates the cost of one run from a generic RunRecord.
type CostEstimator interface {
	EstimateRunCost(context.Context, RunRecord) (float64, error)
}

// CostEstimatorFunc adapts a function into a CostEstimator.
type CostEstimatorFunc func(context.Context, RunRecord) (float64, error)

// EstimateRunCost calls the underlying function to estimate cost.
func (f CostEstimatorFunc) EstimateRunCost(ctx context.Context, record RunRecord) (float64, error) {
	if f == nil {
		return 0, fmt.Errorf("automation: cost estimator func is nil")
	}
	return f(ctx, record)
}

// CostingRunStore adds a generic cost estimate before writing a run.
type CostingRunStore struct {
	Store     RunStore
	Estimator CostEstimator
}

// SeenEvent delegates to the underlying RunStore.
func (s *CostingRunStore) SeenEvent(ctx context.Context, key string) (bool, error) {
	store, err := s.store()
	if err != nil {
		return false, err
	}
	return store.SeenEvent(ctx, key)
}

// RecordEvent delegates to the underlying RunStore.
func (s *CostingRunStore) RecordEvent(ctx context.Context, event event.Event) error {
	store, err := s.store()
	if err != nil {
		return err
	}
	return store.RecordEvent(ctx, event)
}

// RecordRun estimates cost and delegates run recording to the underlying RunStore.
func (s *CostingRunStore) RecordRun(ctx context.Context, record RunRecord) error {
	if err := checkStoreInput(ctx); err != nil {
		return err
	}
	store, err := s.store()
	if err != nil {
		return err
	}
	if s.Estimator != nil {
		cost, err := s.Estimator.EstimateRunCost(ctx, record)
		if err != nil {
			return fmt.Errorf("automation: estimate run cost: %w", err)
		}
		if cost < 0 {
			return fmt.Errorf("automation: estimated cost is negative")
		}
		record.EstimatedCost = cost
	}
	return store.RecordRun(ctx, record)
}

func (s *CostingRunStore) store() (RunStore, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("automation: costing run store is nil")
	}
	return s.Store, nil
}

var _ RunStore = (*CostingRunStore)(nil)
