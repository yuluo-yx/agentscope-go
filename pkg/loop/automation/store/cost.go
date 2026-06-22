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

// CostEstimator 基于通用 RunRecord 估算一次 run 的成本。
type CostEstimator interface {
	EstimateRunCost(context.Context, RunRecord) (float64, error)
}

// CostEstimatorFunc 将函数适配为 CostEstimator。
type CostEstimatorFunc func(context.Context, RunRecord) (float64, error)

// EstimateRunCost 调用底层函数估算成本。
func (f CostEstimatorFunc) EstimateRunCost(ctx context.Context, record RunRecord) (float64, error) {
	if f == nil {
		return 0, fmt.Errorf("automation: cost estimator func is nil")
	}
	return f(ctx, record)
}

// CostingRunStore 在写入 run 前补充通用成本估算。
type CostingRunStore struct {
	Store     RunStore
	Estimator CostEstimator
}

// SeenEvent 委托给底层 RunStore。
func (s *CostingRunStore) SeenEvent(ctx context.Context, key string) (bool, error) {
	store, err := s.store()
	if err != nil {
		return false, err
	}
	return store.SeenEvent(ctx, key)
}

// RecordEvent 委托给底层 RunStore。
func (s *CostingRunStore) RecordEvent(ctx context.Context, event event.Event) error {
	store, err := s.store()
	if err != nil {
		return err
	}
	return store.RecordEvent(ctx, event)
}

// RecordRun 估算成本后委托给底层 RunStore 记录 run。
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
