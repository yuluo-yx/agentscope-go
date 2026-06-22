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
	"encoding/json"
	"strings"
	"testing"

	eventpkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
)

func TestEventCloneIsolatesMutableFields(t *testing.T) {
	t.Parallel()

	event := eventpkg.Event{
		ID:         "evt-1",
		Source:     "manual://user",
		Type:       "manual.requested",
		Data:       json.RawMessage(`{"status":"open"}`),
		Extensions: map[string]string{"provider": "local"},
		Labels:     []string{"triage"},
	}

	clone := event.Clone()
	clone.Data[11] = 'X'
	clone.Extensions["provider"] = "mutated"
	clone.Labels[0] = "mutated"

	if string(event.Data) != `{"status":"open"}` || event.Extensions["provider"] != "local" || event.Labels[0] != "triage" {
		t.Fatalf("Event.Clone did not isolate mutable fields: %#v", event)
	}
}

func TestRouteDecisionCloneAndRouterFuncBoundaries(t *testing.T) {
	t.Parallel()

	decision := eventpkg.RouteDecision{
		LoopName: "triage",
		Labels:   []string{"daily"},
		Metadata: map[string]any{"workspace": map[string]any{"root": "/tmp/run"}},
	}
	clone := decision.Clone()
	clone.Labels[0] = "mutated"
	clone.Metadata["workspace"].(map[string]any)["root"] = "/tmp/mutated"
	if decision.Labels[0] != "daily" || decision.Metadata["workspace"].(map[string]any)["root"] != "/tmp/run" {
		t.Fatalf("RouteDecision.Clone did not isolate nested metadata: %#v", decision)
	}

	_, err := (eventpkg.RouterFunc(nil)).Route(context.Background(), eventpkg.Event{})
	if err == nil || !strings.Contains(err.Error(), "router is nil") {
		t.Fatalf("nil RouterFunc error = %v, want router is nil", err)
	}

	called := false
	got, err := (eventpkg.RouterFunc(func(_ context.Context, event eventpkg.Event) (eventpkg.RouteDecision, error) {
		called = event.ID == "evt-1"
		return eventpkg.RouteDecision{LoopName: "handled"}, nil
	})).Route(context.Background(), eventpkg.Event{ID: "evt-1"})
	if err != nil || !called || got.LoopName != "handled" {
		t.Fatalf("RouterFunc callback result = %#v called=%v err=%v", got, called, err)
	}
}

func TestEventHandlerFuncAndCloneStringMapBoundaries(t *testing.T) {
	t.Parallel()

	if err := (eventpkg.EventHandlerFunc(nil)).HandleEvent(context.Background(), eventpkg.Event{}); err == nil ||
		!strings.Contains(err.Error(), "event handler is nil") {
		t.Fatalf("nil EventHandlerFunc error = %v, want event handler is nil", err)
	}

	handled := false
	err := (eventpkg.EventHandlerFunc(func(_ context.Context, event eventpkg.Event) error {
		handled = event.ID == "evt-1"
		return nil
	})).HandleEvent(context.Background(), eventpkg.Event{ID: "evt-1"})
	if err != nil || !handled {
		t.Fatalf("EventHandlerFunc callback handled=%v err=%v", handled, err)
	}

	values := map[string]string{"key": "value"}
	clone := eventpkg.CloneStringMap(values)
	clone["key"] = "mutated"
	if values["key"] != "value" || eventpkg.CloneStringMap(nil) != nil {
		t.Fatalf("CloneStringMap did not preserve source map: %#v", values)
	}
}
