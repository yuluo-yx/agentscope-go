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

package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

// ToolLoader is the minimal MCP client surface required by DeferredToolkit.
type ToolLoader interface {
	Name() string
	ListTools(context.Context) ([]astool.Tool, error)
}

// DeferredToolkit loads MCP tools on first use instead of construction time.
type DeferredToolkit struct {
	loader ToolLoader

	mu      sync.Mutex
	toolkit *astool.Toolkit
}

// NewDeferredToolkit creates a toolkit that defers MCP ListTools until a tool
// schema, lookup, or call is requested.
func NewDeferredToolkit(loader ToolLoader) (*DeferredToolkit, error) {
	if loader == nil {
		return nil, fmt.Errorf("mcp: nil deferred tool loader")
	}
	return &DeferredToolkit{loader: loader}, nil
}

// Invalidate clears the wrapped tool cache so the next operation reloads tools.
func (t *DeferredToolkit) Invalidate() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.toolkit = nil
}

// ToolSchemas returns model-facing tool schemas, loading MCP tools on demand.
func (t *DeferredToolkit) ToolSchemas(activeGroups ...string) ([]asmodel.ToolSchema, error) {
	kit, err := t.ensureToolkit(context.Background())
	if err != nil {
		return nil, err
	}
	return kit.ToolSchemas(activeGroups...)
}

// FindTool looks up one tool, loading MCP tools on demand.
func (t *DeferredToolkit) FindTool(name string, activeGroups ...string) (astool.Tool, bool) {
	kit, err := t.ensureToolkit(context.Background())
	if err != nil {
		return nil, false
	}
	return kit.FindTool(name, activeGroups...)
}

// CallTool executes one tool call through the deferred toolkit.
func (t *DeferredToolkit) CallTool(ctx context.Context, call *message.ToolCallBlock, state *asstate.AgentState) (<-chan astool.ToolChunk, error) {
	kit, err := t.ensureToolkit(ctx)
	if err != nil {
		return nil, err
	}
	return kit.CallTool(ctx, call, state)
}

// RunTool executes one tool call and accumulates chunks into a response.
func (t *DeferredToolkit) RunTool(ctx context.Context, call *message.ToolCallBlock, state *asstate.AgentState) (*astool.ToolResponse, error) {
	kit, err := t.ensureToolkit(ctx)
	if err != nil {
		return nil, err
	}
	return kit.RunTool(ctx, call, state)
}

func (t *DeferredToolkit) ensureToolkit(ctx context.Context) (*astool.Toolkit, error) {
	if t == nil {
		return nil, fmt.Errorf("mcp: nil deferred toolkit")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.toolkit != nil {
		return t.toolkit, nil
	}
	tools, err := t.loader.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	kit, err := astool.NewToolkit(tools...)
	if err != nil {
		return nil, err
	}
	t.toolkit = kit
	return t.toolkit, nil
}
