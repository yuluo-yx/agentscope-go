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

package agent

import (
	"context"
	"fmt"

	"github.com/yuluo-yx/agentscope-go/message"
)

func (a *Agent) effectiveToolProvider() ToolProvider {
	if a == nil {
		return emptyToolProvider{}
	}
	if a.middlewareToolkit == nil {
		return a.toolkit
	}
	return compositeToolProvider{
		primary:   a.toolkit,
		secondary: a.middlewareToolkit,
	}
}

type compositeToolProvider struct {
	primary   ToolProvider
	secondary ToolProvider
}

func (p compositeToolProvider) ToolSchemas(activeGroups ...string) ([]ToolSchema, error) {
	primary, err := providerSchemas(p.primary, activeGroups...)
	if err != nil {
		return nil, err
	}
	secondary, err := providerSchemas(p.secondary, activeGroups...)
	if err != nil {
		return nil, err
	}
	if err := ensureUniqueToolSchemas(primary, secondary); err != nil {
		return nil, err
	}
	return append(primary, secondary...), nil
}

func (p compositeToolProvider) FindTool(name string, activeGroups ...string) (Tool, bool) {
	if p.primary != nil {
		if tool, ok := p.primary.FindTool(name, activeGroups...); ok {
			return tool, true
		}
	}
	if p.secondary != nil {
		return p.secondary.FindTool(name, activeGroups...)
	}
	return nil, false
}

func (p compositeToolProvider) CallTool(ctx context.Context, call *message.ToolCallBlock, state *AgentState) (<-chan ToolChunk, error) {
	if call == nil {
		return nil, fmt.Errorf("agentscope: nil tool call")
	}
	activeGroups := activeGroupsFromState(state)
	if p.primary != nil {
		if _, ok := p.primary.FindTool(call.Name, activeGroups...); ok {
			return p.primary.CallTool(ctx, call, state)
		}
	}
	if p.secondary != nil {
		if _, ok := p.secondary.FindTool(call.Name, activeGroups...); ok {
			return p.secondary.CallTool(ctx, call, state)
		}
	}
	if p.primary != nil {
		return p.primary.CallTool(ctx, call, state)
	}
	return emptyToolProvider{}.CallTool(ctx, call, state)
}

func providerSchemas(provider ToolProvider, activeGroups ...string) ([]ToolSchema, error) {
	if provider == nil {
		return nil, nil
	}
	return provider.ToolSchemas(activeGroups...)
}

func ensureUniqueToolSchemas(groups ...[]ToolSchema) error {
	seen := map[string]bool{}
	for _, schemas := range groups {
		for _, schema := range schemas {
			name := schema.Function.Name
			if name == "" {
				continue
			}
			if seen[name] {
				return fmt.Errorf("agentscope: duplicate tool schema %q", name)
			}
			seen[name] = true
		}
	}
	return nil
}

func activeGroupsFromState(state *AgentState) []string {
	if state == nil || state.ToolContext == nil {
		return nil
	}
	return append([]string(nil), state.ToolContext.ActivatedGroups...)
}
