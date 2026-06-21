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

package gate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	gatepkg "github.com/yuluo-yx/agentscope-go/loop/automation/gate"
)

func TestSchedulerRejectsEventWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	scheduler, err := gatepkg.NewScheduler(gatepkg.SchedulerPolicy{
		MaxConcurrent: 1,
		MaxQueueSize:  1,
	}, automationevent.EventHandlerFunc(func(ctx context.Context, event automationevent.Event) error {
		if event.ID != "evt-1" {
			t.Errorf("event id = %q, want evt-1", event.ID)
		}
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	if err != nil {
		t.Fatalf("NewScheduler returned error: %v", err)
	}

	go func() {
		done <- scheduler.HandleEvent(context.Background(), automationevent.Event{
			ID:     "evt-1",
			Source: "manual://one",
			Type:   "manual.requested",
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("first event did not start")
	}

	err = scheduler.HandleEvent(context.Background(), automationevent.Event{
		ID:     "evt-2",
		Source: "manual://two",
		Type:   "manual.requested",
	})
	if !errors.Is(err, gatepkg.ErrSchedulerQueueFull) {
		t.Fatalf("second event error = %v, want queue full", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first event returned error: %v", err)
	}
}

func TestSchedulerLimitsConcurrentRunsPerSource(t *testing.T) {
	t.Parallel()

	started := make(chan string, 3)
	release := make(chan struct{})
	scheduler, err := gatepkg.NewScheduler(gatepkg.SchedulerPolicy{
		MaxConcurrent: 2,
		MaxQueueSize:  3,
		PerSourceLimit: map[string]int{
			"manual://same": 1,
		},
	}, automationevent.EventHandlerFunc(func(ctx context.Context, event automationevent.Event) error {
		started <- event.ID
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	if err != nil {
		t.Fatalf("NewScheduler returned error: %v", err)
	}

	firstDone := make(chan error, 1)
	secondSameDone := make(chan error, 1)
	otherDone := make(chan error, 1)
	go func() {
		firstDone <- scheduler.HandleEvent(context.Background(), automationevent.Event{
			ID:     "same-1",
			Source: "manual://same",
			Type:   "manual.requested",
		})
	}()
	if got := waitStarted(t, started); got != "same-1" {
		t.Fatalf("first started event = %q, want same-1", got)
	}

	go func() {
		secondSameDone <- scheduler.HandleEvent(context.Background(), automationevent.Event{
			ID:     "same-2",
			Source: "manual://same",
			Type:   "manual.requested",
		})
	}()
	go func() {
		otherDone <- scheduler.HandleEvent(context.Background(), automationevent.Event{
			ID:     "other-1",
			Source: "manual://other",
			Type:   "manual.requested",
		})
	}()
	if got := waitStarted(t, started); got != "other-1" {
		t.Fatalf("second started event = %q, want other-1", got)
	}
	select {
	case got := <-started:
		t.Fatalf("same source event started before source slot was released: %q", got)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	for name, ch := range map[string]<-chan error{
		"first":       firstDone,
		"secondSame":  secondSameDone,
		"otherSource": otherDone,
	} {
		if err := <-ch; err != nil {
			t.Fatalf("%s returned error: %v", name, err)
		}
	}
}

func TestSchedulerLimitsConcurrentRunsPerType(t *testing.T) {
	t.Parallel()

	started := make(chan string, 3)
	release := make(chan struct{})
	scheduler, err := gatepkg.NewScheduler(gatepkg.SchedulerPolicy{
		MaxConcurrent: 2,
		MaxQueueSize:  3,
		PerTypeLimit: map[string]int{
			"release.requested": 1,
		},
	}, automationevent.EventHandlerFunc(func(ctx context.Context, event automationevent.Event) error {
		started <- event.ID
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	if err != nil {
		t.Fatalf("NewScheduler returned error: %v", err)
	}

	firstDone := make(chan error, 1)
	secondSameDone := make(chan error, 1)
	otherDone := make(chan error, 1)
	go func() {
		firstDone <- scheduler.HandleEvent(context.Background(), automationevent.Event{
			ID:     "type-1",
			Source: "manual://one",
			Type:   "release.requested",
		})
	}()
	if got := waitStarted(t, started); got != "type-1" {
		t.Fatalf("first started event = %q, want type-1", got)
	}

	go func() {
		secondSameDone <- scheduler.HandleEvent(context.Background(), automationevent.Event{
			ID:     "type-2",
			Source: "manual://two",
			Type:   "release.requested",
		})
	}()
	go func() {
		otherDone <- scheduler.HandleEvent(context.Background(), automationevent.Event{
			ID:     "other-type",
			Source: "manual://three",
			Type:   "review.requested",
		})
	}()
	if got := waitStarted(t, started); got != "other-type" {
		t.Fatalf("second started event = %q, want other-type", got)
	}
	select {
	case got := <-started:
		t.Fatalf("same type event started before type slot was released: %q", got)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	for name, ch := range map[string]<-chan error{
		"first":      firstDone,
		"secondSame": secondSameDone,
		"otherType":  otherDone,
	} {
		if err := <-ch; err != nil {
			t.Fatalf("%s returned error: %v", name, err)
		}
	}
}

func TestSchedulerReturnsContextErrorWhileWaiting(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	scheduler, err := gatepkg.NewScheduler(gatepkg.SchedulerPolicy{
		MaxConcurrent: 1,
		MaxQueueSize:  2,
	}, automationevent.EventHandlerFunc(func(ctx context.Context, event automationevent.Event) error {
		if event.ID == "evt-1" {
			close(started)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	if err != nil {
		t.Fatalf("NewScheduler returned error: %v", err)
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- scheduler.HandleEvent(context.Background(), automationevent.Event{
			ID:     "evt-1",
			Source: "manual://one",
			Type:   "manual.requested",
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatalf("first event did not start")
	}

	ctx, cancel := context.WithCancel(context.Background())
	waitingDone := make(chan error, 1)
	go func() {
		waitingDone <- scheduler.HandleEvent(ctx, automationevent.Event{
			ID:     "evt-2",
			Source: "manual://two",
			Type:   "manual.requested",
		})
	}()
	cancel()
	if err := <-waitingDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting event error = %v, want context canceled", err)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first event returned error: %v", err)
	}
}

func TestNewSchedulerValidatesPolicy(t *testing.T) {
	t.Parallel()

	_, err := gatepkg.NewScheduler(gatepkg.SchedulerPolicy{}, nil)
	if err == nil {
		t.Fatalf("NewScheduler should reject nil handler")
	}

	_, err = gatepkg.NewScheduler(gatepkg.SchedulerPolicy{
		PerSourceLimit: map[string]int{"manual://same": 0},
	}, automationevent.EventHandlerFunc(func(context.Context, automationevent.Event) error {
		return nil
	}))
	if err == nil {
		t.Fatalf("NewScheduler should reject non-positive per-source limit")
	}
}

func waitStarted(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case got := <-started:
		return got
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for event to start")
		return ""
	}
}
