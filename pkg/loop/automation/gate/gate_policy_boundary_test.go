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

	automationevent "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
	gatepkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/gate"
)

func TestGateFuncAndPolicyRejectInvalidInputs(t *testing.T) {
	t.Parallel()

	if _, err := (gatepkg.GateFunc)(nil).Evaluate(context.Background(), automationevent.Event{}, automationevent.RouteDecision{}); err == nil {
		t.Fatalf("nil GateFunc should fail")
	}

	called := false
	decision, err := gatepkg.GateFunc(func(context.Context, automationevent.Event, automationevent.RouteDecision) (gatepkg.GateDecision, error) {
		called = true
		return gatepkg.GateDecision{StopReason: "manual_stop"}, nil
	}).Evaluate(context.Background(), automationevent.Event{}, automationevent.RouteDecision{})
	if err != nil {
		t.Fatalf("GateFunc returned error: %v", err)
	}
	if !called || !decision.RequiresStop() {
		t.Fatalf("GateFunc should return the wrapped decision: called=%v decision=%#v", called, decision)
	}

	var nilCtx context.Context
	if _, err := (gatepkg.GatePolicy{}).Evaluate(nilCtx, automationevent.Event{}, automationevent.RouteDecision{}); err == nil {
		t.Fatalf("GatePolicy should reject a nil context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (gatepkg.GatePolicy{}).Evaluate(ctx, automationevent.Event{}, automationevent.RouteDecision{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("GatePolicy canceled error = %v, want %v", err, context.Canceled)
	}
}

func TestGatePolicyPreservesExplicitRuleMetadata(t *testing.T) {
	t.Parallel()

	policy := gatepkg.GatePolicy{Rules: []gatepkg.GateRule{{
		Name:       "requires-approval",
		Type:       "manual.requested",
		StopReason: "approval_required",
		Metadata:   map[string]string{"rule": "custom-rule-id"},
	}}}

	decision, err := policy.Evaluate(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "manual://operator", Type: "manual.requested"},
		automationevent.RouteDecision{},
	)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.StopReason != "approval_required" || decision.Metadata["rule"] != "custom-rule-id" {
		t.Fatalf("gate should preserve explicit stop reason and rule metadata: %#v", decision)
	}
}

func TestGatePolicyAllowsWhenAnyRuleFieldMismatches(t *testing.T) {
	t.Parallel()

	baseEvent := automationevent.Event{
		ID:         "evt-1",
		Source:     "manual://operator",
		Type:       "manual.requested",
		Subject:    "service://billing",
		Labels:     []string{"approval", "release"},
		Extensions: map[string]string{"risk": "high"},
	}
	baseRoute := automationevent.RouteDecision{
		LoopName:  "release-loop",
		AgentName: "Friday",
		Labels:    []string{"release"},
		Metadata:  map[string]any{"mode": "assisted"},
	}
	tests := []struct {
		name  string
		rule  gatepkg.GateRule
		event automationevent.Event
		route automationevent.RouteDecision
	}{
		{
			name:  "source prefix",
			rule:  gatepkg.GateRule{SourcePrefix: "schedule://"},
			event: baseEvent,
			route: baseRoute,
		},
		{
			name:  "type",
			rule:  gatepkg.GateRule{Type: automationevent.EventTypeScheduleTick},
			event: baseEvent,
			route: baseRoute,
		},
		{
			name:  "subject",
			rule:  gatepkg.GateRule{Subject: "service://checkout"},
			event: baseEvent,
			route: baseRoute,
		},
		{
			name:  "route loop",
			rule:  gatepkg.GateRule{RouteLoopName: "triage-loop"},
			event: baseEvent,
			route: baseRoute,
		},
		{
			name:  "route agent",
			rule:  gatepkg.GateRule{RouteAgentName: "Saturday"},
			event: baseEvent,
			route: baseRoute,
		},
		{
			name:  "event labels",
			rule:  gatepkg.GateRule{EventLabels: []string{"security"}},
			event: baseEvent,
			route: baseRoute,
		},
		{
			name:  "route labels",
			rule:  gatepkg.GateRule{RouteLabels: []string{"security"}},
			event: baseEvent,
			route: baseRoute,
		},
		{
			name:  "event extensions",
			rule:  gatepkg.GateRule{EventExtensions: map[string]string{"risk": "low"}},
			event: baseEvent,
			route: baseRoute,
		},
		{
			name:  "route metadata",
			rule:  gatepkg.GateRule{RouteMetadata: map[string]string{"mode": "autonomous"}},
			event: baseEvent,
			route: baseRoute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy := gatepkg.GatePolicy{Rules: []gatepkg.GateRule{tt.rule}}
			decision, err := policy.Evaluate(context.Background(), tt.event, tt.route)
			if err != nil {
				t.Fatalf("Evaluate returned error: %v", err)
			}
			if decision.RequiresStop() {
				t.Fatalf("mismatched rule should allow the event: %#v", decision)
			}
		})
	}
}
