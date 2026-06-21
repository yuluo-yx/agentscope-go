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
	"testing"

	eventpkg "github.com/yuluo-yx/agentscope-go/loop/automation/event"
)

func TestStaticRouterReturnsDecision(t *testing.T) {
	t.Parallel()

	decision := eventpkg.RouteDecision{
		LoopName:  "daily-triage",
		AgentName: "Friday",
		Labels:    []string{"triage"},
		Metadata:  map[string]any{"mode": "report"},
	}
	router := eventpkg.StaticRouter{Decision: decision}

	got, err := router.Route(context.Background(), eventpkg.Event{
		ID: "evt-1", Source: "manual://user", Type: "manual.requested",
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if got.LoopName != decision.LoopName || got.AgentName != decision.AgentName ||
		len(got.Labels) != 1 || got.Labels[0] != "triage" || got.Metadata["mode"] != "report" {
		t.Fatalf("RouteDecision mismatch: %#v", got)
	}
}

func TestRuleRouterMatchesGenericEventFields(t *testing.T) {
	t.Parallel()

	router := eventpkg.RuleRouter{
		Rules: []eventpkg.RouteRule{
			{
				SourcePrefix: "schedule://",
				Type:         "schedule.tick",
				Subject:      "repo://current",
				Labels:       []string{"daily"},
				Decision: eventpkg.RouteDecision{
					LoopName:  "daily-triage",
					AgentName: "Friday",
				},
			},
		},
		Default: eventpkg.RouteDecision{LoopName: "fallback", AgentName: "Fallback"},
	}

	got, err := router.Route(context.Background(), eventpkg.Event{
		ID:      "evt-1",
		Source:  "schedule://daily-triage",
		Type:    "schedule.tick",
		Subject: "repo://current",
		Labels:  []string{"daily", "triage"},
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}
	if got.LoopName != "daily-triage" || got.AgentName != "Friday" {
		t.Fatalf("RouteDecision = %#v, want matching rule", got)
	}

	fallback, err := router.Route(context.Background(), eventpkg.Event{
		ID: "evt-2", Source: "manual://user", Type: "manual.requested",
	})
	if err != nil {
		t.Fatalf("fallback Route returned error: %v", err)
	}
	if fallback.LoopName != "fallback" {
		t.Fatalf("fallback RouteDecision = %#v", fallback)
	}
}
