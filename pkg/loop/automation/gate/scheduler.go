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
	"errors"
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
)

// ErrSchedulerQueueFull indicates that Scheduler has no queue capacity available.
var ErrSchedulerQueueFull = errors.New("automation: scheduler queue full")

// SchedulerPolicy describes concurrency and backpressure policy for generic event handling.
type SchedulerPolicy struct {
	MaxConcurrent  int
	MaxQueueSize   int
	PerSourceLimit map[string]int
	PerTypeLimit   map[string]int
}

// Scheduler applies generic concurrency control and backpressure before calling the downstream EventHandler.
type Scheduler struct {
	handler event.EventHandler

	queue       chan struct{}
	running     chan struct{}
	sourceLimit map[string]chan struct{}
	typeLimit   map[string]chan struct{}
}

// NewScheduler creates an EventHandler constrained by SchedulerPolicy.
func NewScheduler(policy SchedulerPolicy, handler event.EventHandler) (*Scheduler, error) {
	if handler == nil {
		return nil, fmt.Errorf("automation: event handler is nil")
	}
	normalized, err := normalizeSchedulerPolicy(policy)
	if err != nil {
		return nil, err
	}
	return &Scheduler{
		handler:     handler,
		queue:       make(chan struct{}, normalized.MaxQueueSize),
		running:     make(chan struct{}, normalized.MaxConcurrent),
		sourceLimit: makeLimiters(normalized.PerSourceLimit),
		typeLimit:   makeLimiters(normalized.PerTypeLimit),
	}, nil
}

// HandleEvent calls the downstream handler after policy permits execution.
func (s *Scheduler) HandleEvent(ctx context.Context, evt event.Event) error {
	if s == nil {
		return fmt.Errorf("automation: scheduler is nil")
	}
	if ctx == nil {
		return fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := evt.Validate(); err != nil {
		return err
	}
	if err := acquireQueue(ctx, s.queue); err != nil {
		return err
	}
	defer releaseSlot(s.queue)

	if limiter := s.sourceLimit[strings.TrimSpace(evt.Source)]; limiter != nil {
		if err := acquireSlot(ctx, limiter); err != nil {
			return err
		}
		defer releaseSlot(limiter)
	}
	if limiter := s.typeLimit[strings.TrimSpace(evt.Type)]; limiter != nil {
		if err := acquireSlot(ctx, limiter); err != nil {
			return err
		}
		defer releaseSlot(limiter)
	}
	if err := acquireSlot(ctx, s.running); err != nil {
		return err
	}
	defer releaseSlot(s.running)

	return s.handler.HandleEvent(ctx, evt.Clone())
}

func normalizeSchedulerPolicy(policy SchedulerPolicy) (SchedulerPolicy, error) {
	if policy.MaxConcurrent <= 0 {
		policy.MaxConcurrent = 1
	}
	if policy.MaxQueueSize <= 0 {
		policy.MaxQueueSize = policy.MaxConcurrent
	}

	sourceLimits, err := normalizeLimiterConfig("source", policy.PerSourceLimit)
	if err != nil {
		return SchedulerPolicy{}, err
	}
	typeLimits, err := normalizeLimiterConfig("type", policy.PerTypeLimit)
	if err != nil {
		return SchedulerPolicy{}, err
	}
	policy.PerSourceLimit = sourceLimits
	policy.PerTypeLimit = typeLimits
	return policy, nil
}

func normalizeLimiterConfig(name string, limits map[string]int) (map[string]int, error) {
	if len(limits) == 0 {
		return nil, nil
	}
	out := make(map[string]int, len(limits))
	for key, limit := range limits {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("automation: scheduler %s limit key is empty", name)
		}
		if limit <= 0 {
			return nil, fmt.Errorf("automation: scheduler %s limit for %q must be positive", name, key)
		}
		out[key] = limit
	}
	return out, nil
}

func makeLimiters(limits map[string]int) map[string]chan struct{} {
	if len(limits) == 0 {
		return nil
	}
	out := make(map[string]chan struct{}, len(limits))
	for key, limit := range limits {
		out[key] = make(chan struct{}, limit)
	}
	return out
}

func acquireQueue(ctx context.Context, queue chan struct{}) error {
	select {
	case queue <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrSchedulerQueueFull
	}
}

func acquireSlot(ctx context.Context, slot chan struct{}) error {
	select {
	case slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseSlot(slot chan struct{}) {
	<-slot
}

var _ event.EventHandler = (*Scheduler)(nil)
