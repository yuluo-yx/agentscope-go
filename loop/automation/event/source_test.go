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

package event_test

import (
	"context"
	"errors"
	"testing"
	"time"

	eventpkg "github.com/yuluo-yx/agentscope-go/loop/automation/event"
)

func TestTickerSourceEmitsGenericScheduleEventAndStopsOnCancel(t *testing.T) {
	t.Parallel()

	source := eventpkg.TickerSource{
		Source:   "schedule://daily-triage",
		Interval: time.Millisecond,
		Subject:  "repo://current",
		Labels:   []string{"daily"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got eventpkg.Event
	err := source.Start(ctx, eventpkg.EventHandlerFunc(func(_ context.Context, event eventpkg.Event) error {
		got = event
		cancel()
		return nil
	}))
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if got.ID == "" || got.Source != source.Source || got.Type != eventpkg.EventTypeScheduleTick ||
		got.Subject != source.Subject || len(got.Labels) != 1 || got.Labels[0] != "daily" {
		t.Fatalf("ticker event mismatch: %#v", got)
	}
}

func TestTickerSourceReturnsHandlerError(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("handler failed")
	source := eventpkg.TickerSource{Source: "schedule://daily-triage", Interval: time.Millisecond}

	err := source.Start(context.Background(), eventpkg.EventHandlerFunc(func(context.Context, eventpkg.Event) error {
		return handlerErr
	}))
	if !errors.Is(err, handlerErr) {
		t.Fatalf("Start error = %v, want handler error", err)
	}
}
