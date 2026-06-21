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

package runner

import (
	"context"
	"fmt"

	"github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/loop/automation/event"
)

// AgentResolver returns the Agent selected by a route decision.
type AgentResolver interface {
	ResolveAgent(context.Context, event.RouteDecision) (*agent.Agent, error)
}

// AgentResolverFunc adapts a function to AgentResolver.
type AgentResolverFunc func(context.Context, event.RouteDecision) (*agent.Agent, error)

// ResolveAgent calls f(ctx, decision).
func (f AgentResolverFunc) ResolveAgent(ctx context.Context, decision event.RouteDecision) (*agent.Agent, error) {
	if f == nil {
		return nil, fmt.Errorf("automation: agent resolver is nil")
	}
	return f(ctx, decision)
}

// StaticAgentResolver returns the same Agent for every route decision.
type StaticAgentResolver struct {
	Agent *agent.Agent
}

// ResolveAgent returns the configured Agent.
func (r StaticAgentResolver) ResolveAgent(ctx context.Context, _ event.RouteDecision) (*agent.Agent, error) {
	if ctx == nil {
		return nil, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.Agent == nil {
		return nil, fmt.Errorf("automation: agent is nil")
	}
	return r.Agent, nil
}
