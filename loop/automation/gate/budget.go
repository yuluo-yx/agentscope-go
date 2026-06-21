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

package gate

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/loop/automation/event"
	"github.com/yuluo-yx/agentscope-go/loop/automation/store"
)

// AutomationBudget 描述跨 run 的自动化预算上限。
type AutomationBudget struct {
	MaxRunsPerDay   int
	MaxTokensPerDay int
	MaxCostPerDay   float64
	PerEventType    map[string]BudgetLimit
	PerLoop         map[string]BudgetLimit
}

// BudgetLimit 描述一个预算作用域内的每日上限。
type BudgetLimit struct {
	MaxRunsPerDay   int
	MaxTokensPerDay int
	MaxCostPerDay   float64
}

// BudgetStore 提供指定时间窗口内的自动化用量。
type BudgetStore interface {
	BudgetUsage(context.Context, store.BudgetWindow) (store.BudgetUsage, error)
}

// BudgetGate 在启动 Agent 前检查长期预算。
type BudgetGate struct {
	Store      BudgetStore
	Budget     AutomationBudget
	Now        func() time.Time
	StopReason string
}

// Evaluate 根据当天历史用量判断本次 run 是否应该暂停。
func (g BudgetGate) Evaluate(ctx context.Context, evt event.Event, decision event.RouteDecision) (GateDecision, error) {
	if ctx == nil {
		return GateDecision{}, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return GateDecision{}, err
	}
	if !g.Budget.hasLimits() {
		return GateDecision{}, nil
	}
	if g.Store == nil {
		return GateDecision{}, fmt.Errorf("automation: budget store is nil")
	}

	window := dailyBudgetWindow(g.now())
	if decision, err := g.evaluateLimit(ctx, "global", "", window, g.Budget.globalLimit()); err != nil || decision.RequiresStop() {
		return decision, err
	}
	if limit, ok := g.Budget.PerEventType[strings.TrimSpace(evt.Type)]; ok {
		scoped := window
		scoped.EventType = strings.TrimSpace(evt.Type)
		if decision, err := g.evaluateLimit(ctx, "event_type", scoped.EventType, scoped, limit); err != nil || decision.RequiresStop() {
			return decision, err
		}
	}
	if limit, ok := g.Budget.PerLoop[strings.TrimSpace(decision.LoopName)]; ok {
		scoped := window
		scoped.LoopName = strings.TrimSpace(decision.LoopName)
		if decision, err := g.evaluateLimit(ctx, "loop", scoped.LoopName, scoped, limit); err != nil || decision.RequiresStop() {
			return decision, err
		}
	}
	return GateDecision{}, nil
}

func (g BudgetGate) evaluateLimit(
	ctx context.Context,
	scope string,
	scopeValue string,
	window store.BudgetWindow,
	limit BudgetLimit,
) (GateDecision, error) {
	if !limit.hasLimits() {
		return GateDecision{}, nil
	}
	usage, err := g.Store.BudgetUsage(ctx, window)
	if err != nil {
		return GateDecision{}, err
	}
	if limit.MaxRunsPerDay > 0 && usage.Runs >= limit.MaxRunsPerDay {
		return g.stopDecisionInt(scope, scopeValue, "runs_per_day", usage.Runs, limit.MaxRunsPerDay, window), nil
	}
	if limit.MaxTokensPerDay > 0 && usage.TotalTokens >= limit.MaxTokensPerDay {
		return g.stopDecisionInt(scope, scopeValue, "tokens_per_day", usage.TotalTokens, limit.MaxTokensPerDay, window), nil
	}
	if limit.MaxCostPerDay > 0 && usage.EstimatedCost >= limit.MaxCostPerDay {
		return g.stopDecisionFloat(scope, scopeValue, "cost_per_day", usage.EstimatedCost, limit.MaxCostPerDay, window), nil
	}
	return GateDecision{}, nil
}

func (b AutomationBudget) hasLimits() bool {
	if b.globalLimit().hasLimits() {
		return true
	}
	for _, limit := range b.PerEventType {
		if limit.hasLimits() {
			return true
		}
	}
	for _, limit := range b.PerLoop {
		if limit.hasLimits() {
			return true
		}
	}
	return false
}

func (b AutomationBudget) globalLimit() BudgetLimit {
	return BudgetLimit{
		MaxRunsPerDay:   b.MaxRunsPerDay,
		MaxTokensPerDay: b.MaxTokensPerDay,
		MaxCostPerDay:   b.MaxCostPerDay,
	}
}

func (l BudgetLimit) hasLimits() bool {
	return l.MaxRunsPerDay > 0 || l.MaxTokensPerDay > 0 || l.MaxCostPerDay > 0
}

func (g BudgetGate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g BudgetGate) stopDecisionInt(scope, scopeValue, name string, used, limit int, window store.BudgetWindow) GateDecision {
	return g.stopDecision(scope, scopeValue, name, strconv.Itoa(used), strconv.Itoa(limit), window)
}

func (g BudgetGate) stopDecisionFloat(scope, scopeValue, name string, used, limit float64, window store.BudgetWindow) GateDecision {
	return g.stopDecision(scope, scopeValue, name,
		strconv.FormatFloat(used, 'f', -1, 64),
		strconv.FormatFloat(limit, 'f', -1, 64),
		window,
	)
}

func (g BudgetGate) stopDecision(scope, scopeValue, name, used, limit string, window store.BudgetWindow) GateDecision {
	stopReason := g.StopReason
	if stopReason == "" {
		stopReason = "waiting_user"
	}
	metadata := map[string]string{
		"budget":       name,
		"used":         used,
		"limit":        limit,
		"window_start": window.Start.Format(time.RFC3339),
		"window_end":   window.End.Format(time.RFC3339),
	}
	if scope != "" {
		metadata["scope"] = scope
	}
	if scopeValue != "" {
		metadata["scope_value"] = scopeValue
	}
	return GateDecision{
		StopReason: stopReason,
		Reason:     "automation budget exceeded: " + name,
		Metadata:   metadata,
	}
}

func dailyBudgetWindow(now time.Time) store.BudgetWindow {
	if now.IsZero() {
		now = time.Now()
	}
	year, month, day := now.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	return store.BudgetWindow{Start: start, End: start.AddDate(0, 0, 1)}
}

var _ Gate = BudgetGate{}
