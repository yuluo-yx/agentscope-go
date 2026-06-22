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

package core

import (
	"context"
	"time"
)

// Observer receives loop lifecycle events for run logs or metrics.
type Observer interface {
	ObserveLoop(context.Context, RunEvent) error
}

// ObserverFunc adapts a function into an Observer.
type ObserverFunc func(context.Context, RunEvent) error

// ObserveLoop calls f(ctx, event).
func (f ObserverFunc) ObserveLoop(ctx context.Context, event RunEvent) error {
	if f == nil {
		return nil
	}
	return f(ctx, event)
}

// Metrics contains loop counters for one point in time.
type Metrics struct {
	Iteration    int
	ModelCalls   int
	ToolCalls    int
	InputTokens  int
	OutputTokens int
}

// RunEvent is the observer-facing loop event envelope.
type RunEvent struct {
	Type      string
	AgentName string
	SessionID string
	ReplyID   string
	LoopName  string
	Mode      Mode
	Reason    string
	Metrics   Metrics
	Time      time.Time
}
