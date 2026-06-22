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

package event

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// Router selects the loop and Agent that should process an event.
type Router interface {
	Route(context.Context, Event) (RouteDecision, error)
}

// RouterFunc adapts a function to Router.
type RouterFunc func(context.Context, Event) (RouteDecision, error)

// Route calls f(ctx, event).
func (f RouterFunc) Route(ctx context.Context, event Event) (RouteDecision, error) {
	if f == nil {
		return RouteDecision{}, fmt.Errorf("automation: router is nil")
	}
	return f(ctx, event)
}

// RouteDecision records which loop and Agent should process an event.
type RouteDecision struct {
	LoopName  string
	AgentName string
	Labels    []string
	Metadata  map[string]any
}

// Clone returns a deep copy of the route decision.
func (d RouteDecision) Clone() RouteDecision {
	cp := d
	cp.Labels = append([]string(nil), d.Labels...)
	cp.Metadata = utils.CloneAnyMap(d.Metadata)
	return cp
}

// StaticRouter returns the same decision for every event.
type StaticRouter struct {
	Decision RouteDecision
}

// Route returns the configured static decision.
func (r StaticRouter) Route(context.Context, Event) (RouteDecision, error) {
	return r.Decision.Clone(), nil
}

// RouteRule is one simple field-matching routing rule.
type RouteRule struct {
	SourcePrefix string
	Type         string
	Subject      string
	Labels       []string
	Decision     RouteDecision
}

// RuleRouter routes events using simple source, type, subject, and label rules.
type RuleRouter struct {
	Rules   []RouteRule
	Default RouteDecision
}

// Route returns the first matching rule decision or the default decision.
func (r RuleRouter) Route(ctx context.Context, event Event) (RouteDecision, error) {
	if ctx == nil {
		return RouteDecision{}, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return RouteDecision{}, err
	}
	for _, rule := range r.Rules {
		if rule.matches(event) {
			return rule.Decision.Clone(), nil
		}
	}
	return r.Default.Clone(), nil
}

func (r RouteRule) matches(event Event) bool {
	if r.SourcePrefix != "" && !strings.HasPrefix(event.Source, r.SourcePrefix) {
		return false
	}
	if r.Type != "" && r.Type != event.Type {
		return false
	}
	if r.Subject != "" && r.Subject != event.Subject {
		return false
	}
	return HasAllLabels(event.Labels, r.Labels)
}

// HasAllLabels reports whether labels include every required label.
func HasAllLabels(labels, required []string) bool {
	if len(required) == 0 {
		return true
	}
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		seen[label] = struct{}{}
	}
	for _, label := range required {
		if _, ok := seen[label]; !ok {
			return false
		}
	}
	return true
}
