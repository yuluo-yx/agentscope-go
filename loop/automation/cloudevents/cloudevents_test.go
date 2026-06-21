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

package cloudevents_test

import (
	"encoding/json"
	"testing"
	"time"

	cesdk "github.com/cloudevents/sdk-go/v2"

	automationce "github.com/yuluo-yx/agentscope-go/loop/automation/cloudevents"
	eventpkg "github.com/yuluo-yx/agentscope-go/loop/automation/event"
)

func TestFromCloudEventMapsGenericAutomationEnvelope(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 6, 20, 10, 30, 0, 0, time.UTC)
	event := cesdk.NewEvent()
	event.SetID("evt-1")
	event.SetSource("webhook://ci")
	event.SetType("ci.workflow.failed")
	event.SetSubject("repo://agentscope-go")
	event.SetTime(occurredAt)
	if err := event.SetData("application/json", json.RawMessage(`{"workflow":"test"}`)); err != nil {
		t.Fatalf("SetData returned error: %v", err)
	}
	event.SetExtension("correlationid", "corr-1")
	event.SetExtension("causationid", "cause-1")
	event.SetExtension("dedupkey", "ci:main")
	event.SetExtension("labels", "ci,main")
	event.SetExtension("priority", int32(7))
	event.SetExtension("adapter", "github")

	converted, err := automationce.FromCloudEvent(event)
	if err != nil {
		t.Fatalf("FromCloudEvent returned error: %v", err)
	}
	if converted.ID != "evt-1" ||
		converted.Source != "webhook://ci" ||
		converted.Type != "ci.workflow.failed" ||
		converted.Subject != "repo://agentscope-go" ||
		!converted.Time.Equal(occurredAt) ||
		converted.DataContentType != "application/json" ||
		converted.CorrelationID != "corr-1" ||
		converted.CausationID != "cause-1" ||
		converted.DedupKey != "ci:main" ||
		converted.Priority != 7 ||
		converted.Extensions["adapter"] != "github" {
		t.Fatalf("converted event mismatch: %#v", converted)
	}
	if string(converted.Data) != `{"workflow":"test"}` {
		t.Fatalf("converted data = %s", string(converted.Data))
	}
	if len(converted.Labels) != 2 || converted.Labels[0] != "ci" || converted.Labels[1] != "main" {
		t.Fatalf("converted labels = %#v", converted.Labels)
	}
}

func TestToCloudEventMapsAutomationEnvelope(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 6, 20, 11, 0, 0, 0, time.UTC)
	event := eventpkg.Event{
		ID:              "evt-2",
		Source:          "schedule://daily",
		Type:            eventpkg.EventTypeScheduleTick,
		Subject:         "repo://agentscope-go",
		Time:            occurredAt,
		DataContentType: "application/json",
		Data:            json.RawMessage(`{"cadence":"daily"}`),
		CorrelationID:   "corr-2",
		CausationID:     "cause-2",
		DedupKey:        "daily:2026-06-20",
		Labels:          []string{"schedule", "daily"},
		Priority:        3,
		Extensions:      map[string]string{"adapter": "timer"},
	}

	converted, err := automationce.ToCloudEvent(event)
	if err != nil {
		t.Fatalf("ToCloudEvent returned error: %v", err)
	}
	if converted.ID() != event.ID ||
		converted.Source() != event.Source ||
		converted.Type() != event.Type ||
		converted.Subject() != event.Subject ||
		converted.DataContentType() != event.DataContentType ||
		!converted.Time().Equal(occurredAt) {
		t.Fatalf("converted CloudEvent mismatch: %#v", converted)
	}
	if string(converted.Data()) != string(event.Data) {
		t.Fatalf("converted data = %s", string(converted.Data()))
	}
	extensions := converted.Extensions()
	if extensions["correlationid"] != event.CorrelationID ||
		extensions["causationid"] != event.CausationID ||
		extensions["dedupkey"] != event.DedupKey ||
		extensions["labels"] != "schedule,daily" ||
		extensions["priority"] != int32(event.Priority) ||
		extensions["adapter"] != event.Extensions["adapter"] {
		t.Fatalf("converted extensions = %#v", extensions)
	}
}

func TestFromCloudEventRejectsInvalidAutomationEvent(t *testing.T) {
	t.Parallel()

	event := cesdk.NewEvent()
	event.SetID("evt-3")
	event.SetSource("manual://local")

	_, err := automationce.FromCloudEvent(event)
	if err == nil {
		t.Fatalf("FromCloudEvent should reject event without type")
	}
}
