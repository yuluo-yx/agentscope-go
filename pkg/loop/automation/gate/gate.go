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
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
)

// Gate evaluates whether a generic event and routing decision should pause before Agent execution.
type Gate interface {
	Evaluate(context.Context, event.Event, event.RouteDecision) (GateDecision, error)
}

// GateFunc adapts a function into Gate.
type GateFunc func(context.Context, event.Event, event.RouteDecision) (GateDecision, error)

// Evaluate calls f(ctx, event, decision).
func (f GateFunc) Evaluate(ctx context.Context, evt event.Event, decision event.RouteDecision) (GateDecision, error) {
	if f == nil {
		return GateDecision{}, fmt.Errorf("automation: gate is nil")
	}
	return f(ctx, evt, decision)
}

// GateDecision is the gate evaluation result. An empty StopReason allows execution to continue.
type GateDecision struct {
	StopReason string
	Reason     string
	Metadata   map[string]string
}

// RequiresStop reports whether the current gate result should stop this run before Agent execution.
func (d GateDecision) RequiresStop() bool {
	return strings.TrimSpace(d.StopReason) != ""
}

// GatePolicy implements Gate with rules matched against generic fields.
type GatePolicy struct {
	Rules []GateRule
}

// GateRule is a gate rule that is not tied to a specific platform.
type GateRule struct {
	Name            string
	StopReason      string
	Reason          string
	SourcePrefix    string
	Type            string
	Subject         string
	EventLabels     []string
	RouteLabels     []string
	RouteLoopName   string
	RouteAgentName  string
	EventExtensions map[string]string
	RouteMetadata   map[string]string
	Metadata        map[string]string
}

// Evaluate returns the gate result for the first matching rule; without a match, execution continues.
func (p GatePolicy) Evaluate(ctx context.Context, evt event.Event, decision event.RouteDecision) (GateDecision, error) {
	if ctx == nil {
		return GateDecision{}, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return GateDecision{}, err
	}
	for _, rule := range p.Rules {
		if !rule.matches(evt, decision) {
			continue
		}
		stopReason := strings.TrimSpace(rule.StopReason)
		if stopReason == "" {
			stopReason = "waiting_user"
		}
		metadata := event.CloneStringMap(rule.Metadata)
		if strings.TrimSpace(rule.Name) != "" {
			if metadata == nil {
				metadata = map[string]string{}
			}
			if _, ok := metadata["rule"]; !ok {
				metadata["rule"] = rule.Name
			}
		}
		return GateDecision{
			StopReason: stopReason,
			Reason:     rule.Reason,
			Metadata:   metadata,
		}, nil
	}
	return GateDecision{}, nil
}

func (r GateRule) matches(evt event.Event, decision event.RouteDecision) bool {
	if r.SourcePrefix != "" && !strings.HasPrefix(evt.Source, r.SourcePrefix) {
		return false
	}
	if r.Type != "" && r.Type != evt.Type {
		return false
	}
	if r.Subject != "" && r.Subject != evt.Subject {
		return false
	}
	if r.RouteLoopName != "" && r.RouteLoopName != decision.LoopName {
		return false
	}
	if r.RouteAgentName != "" && r.RouteAgentName != decision.AgentName {
		return false
	}
	if !event.HasAllLabels(evt.Labels, r.EventLabels) {
		return false
	}
	if !event.HasAllLabels(decision.Labels, r.RouteLabels) {
		return false
	}
	if !stringMapMatches(evt.Extensions, r.EventExtensions) {
		return false
	}
	return anyMapMatchesStrings(decision.Metadata, r.RouteMetadata)
}

func stringMapMatches(values, required map[string]string) bool {
	for key, want := range required {
		if values[key] != want {
			return false
		}
	}
	return true
}

func anyMapMatchesStrings(values map[string]any, required map[string]string) bool {
	for key, want := range required {
		value, ok := values[key]
		if !ok || fmt.Sprint(value) != want {
			return false
		}
	}
	return true
}

var _ Gate = GatePolicy{}
