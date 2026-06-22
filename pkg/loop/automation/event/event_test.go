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
	"encoding/json"
	"strings"
	"testing"
	"time"

	eventpkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
)

func TestEventValidateAcceptsGenericEnvelope(t *testing.T) {
	t.Parallel()

	event := eventpkg.Event{
		ID:              "evt-1",
		Source:          "schedule://daily-triage",
		Type:            "schedule.tick",
		Subject:         "repo://current",
		Time:            time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC),
		DataContentType: "application/json",
		Data:            json.RawMessage(`{"kind":"anything"}`),
		Extensions:      map[string]string{"adapter": "test"},
		CorrelationID:   "corr-1",
		CausationID:     "cause-1",
		DedupKey:        "daily-triage:2026-06-20",
		Labels:          []string{"daily", "triage"},
		Priority:        3,
	}

	if err := event.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if got := event.DeduplicationKey(); got != event.DedupKey {
		t.Fatalf("DeduplicationKey() = %q, want %q", got, event.DedupKey)
	}
}

func TestEventValidateRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event eventpkg.Event
		want  string
	}{
		{name: "missing id", event: eventpkg.Event{Source: "manual://user", Type: "manual.requested"}, want: "id"},
		{name: "missing source", event: eventpkg.Event{ID: "evt-1", Type: "manual.requested"}, want: "source"},
		{name: "missing type", event: eventpkg.Event{ID: "evt-1", Source: "manual://user"}, want: "type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestEventDeduplicationKeyFallsBackToID(t *testing.T) {
	t.Parallel()

	event := eventpkg.Event{ID: "evt-1", Source: "manual://user", Type: "manual.requested"}
	if got := event.DeduplicationKey(); got != "evt-1" {
		t.Fatalf("DeduplicationKey() = %q, want event id", got)
	}
}
