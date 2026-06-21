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
	"testing"

	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	gatepkg "github.com/yuluo-yx/agentscope-go/loop/automation/gate"
	automationgoal "github.com/yuluo-yx/agentscope-go/loop/automation/goal"
)

func TestGatePolicyMatchesGenericEventAndRouteFields(t *testing.T) {
	t.Parallel()

	policy := gatepkg.GatePolicy{
		Rules: []gatepkg.GateRule{{
			Name:            "release-window",
			Reason:          "release requires approval",
			SourcePrefix:    "manual://",
			Type:            "manual.requested",
			Subject:         "service://billing",
			EventLabels:     []string{"approval"},
			RouteLabels:     []string{"release"},
			RouteLoopName:   "release-loop",
			RouteAgentName:  "Friday",
			EventExtensions: map[string]string{"risk": "high"},
			RouteMetadata:   map[string]string{"mode": "assisted"},
			Metadata:        map[string]string{"owner": "platform"},
		}},
	}
	event := automationevent.Event{
		ID:         "evt-1",
		Source:     "manual://operator",
		Type:       "manual.requested",
		Subject:    "service://billing",
		Labels:     []string{"approval", "release"},
		Extensions: map[string]string{"risk": "high"},
	}
	route := automationevent.RouteDecision{
		LoopName:  "release-loop",
		AgentName: "Friday",
		Labels:    []string{"release"},
		Metadata:  map[string]any{"mode": "assisted"},
	}

	decision, err := policy.Evaluate(context.Background(), event, route)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.StopReason != automationgoal.GoalStopWaitingUser ||
		decision.Reason != "release requires approval" ||
		decision.Metadata["owner"] != "platform" ||
		decision.Metadata["rule"] != "release-window" {
		t.Fatalf("gate decision mismatch: %#v", decision)
	}

	decision.Metadata["owner"] = "mutated"
	again, err := policy.Evaluate(context.Background(), event, route)
	if err != nil {
		t.Fatalf("second Evaluate returned error: %v", err)
	}
	if again.Metadata["owner"] != "platform" {
		t.Fatalf("gate policy should clone rule metadata, got %#v", again.Metadata)
	}
}

func TestGatePolicyAllowsWhenNoRuleMatches(t *testing.T) {
	t.Parallel()

	policy := gatepkg.GatePolicy{
		Rules: []gatepkg.GateRule{{
			Name:         "manual-only",
			SourcePrefix: "manual://",
		}},
	}
	decision, err := policy.Evaluate(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "schedule://daily", Type: "schedule.tick"},
		automationevent.RouteDecision{},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.RequiresStop() {
		t.Fatalf("gate decision should allow unmatched events: %#v", decision)
	}
}
