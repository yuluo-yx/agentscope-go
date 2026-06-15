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

package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/tool/skill"
)

func TestListMCPToolsConnectsStatefulClientsAndReportsErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mcpTool := assemblyTool(t, "MCPWeather")
	client := &assemblyMCP{
		name:     "weather",
		stateful: true,
		tools:    []Tool{mcpTool},
	}
	tools, err := listMCPTools(ctx, []MCPClient{nil, client})
	if err != nil {
		t.Fatalf("listMCPTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "MCPWeather" {
		t.Fatalf("unexpected MCP tools: %#v", tools)
	}
	if client.connectCalls != 1 || client.listCalls != 1 {
		t.Fatalf("stateful disconnected client should connect once and list once: connect=%d list=%d", client.connectCalls, client.listCalls)
	}

	connectErr := errors.New("connect failed")
	_, err = listMCPTools(ctx, []MCPClient{&assemblyMCP{
		name:       "broken",
		stateful:   true,
		connectErr: connectErr,
	}})
	if !errors.Is(err, connectErr) {
		t.Fatalf("listMCPTools connect error = %v, want %v", err, connectErr)
	}

	listErr := errors.New("list failed")
	_, err = listMCPTools(ctx, []MCPClient{&assemblyMCP{
		name:      "broken-list",
		stateful:  true,
		connected: true,
		listErr:   listErr,
	}})
	if !errors.Is(err, listErr) {
		t.Fatalf("listMCPTools list error = %v, want %v", err, listErr)
	}
}

func TestBuildAgentResourcesErrorBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if _, err := BuildAgentResources(ctx, nil); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("BuildAgentResources nil workspace error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := BuildAgentResources(canceled, &assemblyWorkspace{alive: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildAgentResources canceled error = %v", err)
	}

	initErr := errors.New("init failed")
	if _, err := BuildAgentResources(ctx, &assemblyWorkspace{initErr: initErr}); !errors.Is(err, initErr) {
		t.Fatalf("BuildAgentResources init error = %v", err)
	}

	instructionErr := errors.New("instructions failed")
	if _, err := BuildAgentResources(ctx, &assemblyWorkspace{alive: true, instructionsErr: instructionErr}); !errors.Is(err, instructionErr) {
		t.Fatalf("BuildAgentResources instructions error = %v", err)
	}

	toolsErr := errors.New("tools failed")
	if _, err := BuildAgentResources(ctx, &assemblyWorkspace{alive: true, toolsErr: toolsErr}); !errors.Is(err, toolsErr) {
		t.Fatalf("BuildAgentResources tools error = %v", err)
	}

	mcpsErr := errors.New("mcps failed")
	if _, err := BuildAgentResources(ctx, &assemblyWorkspace{alive: true, mcpsErr: mcpsErr}); !errors.Is(err, mcpsErr) {
		t.Fatalf("BuildAgentResources MCP error = %v", err)
	}

	mcpListErr := errors.New("mcp list failed")
	if _, err := BuildAgentResources(ctx, &assemblyWorkspace{
		alive: true,
		mcps:  []MCPClient{&assemblyMCP{name: "broken", listErr: mcpListErr}},
	}); !errors.Is(err, mcpListErr) {
		t.Fatalf("BuildAgentResources MCP tool error = %v", err)
	}

	skillsErr := errors.New("skills failed")
	if _, err := BuildAgentResources(ctx, &assemblyWorkspace{alive: true, skillsErr: skillsErr}); !errors.Is(err, skillsErr) {
		t.Fatalf("BuildAgentResources skills error = %v", err)
	}

	duplicate := assemblyTool(t, "Duplicate")
	if _, err := BuildAgentResources(ctx, &assemblyWorkspace{
		alive: true,
		tools: []Tool{duplicate},
		mcps:  []MCPClient{&assemblyMCP{name: "dup", tools: []Tool{duplicate}}},
	}); err == nil || !strings.Contains(err.Error(), "duplicate tool") {
		t.Fatalf("BuildAgentResources duplicate tool error = %v", err)
	}
}

func assemblyTool(t *testing.T, name string) Tool {
	t.Helper()

	tool, err := tool.NewFunctionTool(
		name,
		"Test tool.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *state.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("ok")}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	return tool
}

type assemblyWorkspace struct {
	alive           bool
	instructions    string
	tools           []Tool
	mcps            []MCPClient
	skills          []skill.Skill
	initErr         error
	instructionsErr error
	toolsErr        error
	mcpsErr         error
	skillsErr       error
}

func (w *assemblyWorkspace) WorkspaceID() string { return "assembly-workspace" }

func (w *assemblyWorkspace) IsAlive() bool { return w.alive }

func (w *assemblyWorkspace) Initialize(context.Context) error {
	if w.initErr != nil {
		return w.initErr
	}
	w.alive = true
	return nil
}

func (*assemblyWorkspace) Close(context.Context) error { return nil }

func (*assemblyWorkspace) Reset(context.Context) error { return nil }

func (w *assemblyWorkspace) GetInstructions(context.Context) (string, error) {
	if w.instructionsErr != nil {
		return "", w.instructionsErr
	}
	return w.instructions, nil
}

func (w *assemblyWorkspace) ListTools(context.Context) ([]Tool, error) {
	if w.toolsErr != nil {
		return nil, w.toolsErr
	}
	return append([]Tool(nil), w.tools...), nil
}

func (w *assemblyWorkspace) ListMCPs(context.Context) ([]MCPClient, error) {
	if w.mcpsErr != nil {
		return nil, w.mcpsErr
	}
	return append([]MCPClient(nil), w.mcps...), nil
}

func (w *assemblyWorkspace) ListSkills(context.Context) ([]skill.Skill, error) {
	if w.skillsErr != nil {
		return nil, w.skillsErr
	}
	return append([]skill.Skill(nil), w.skills...), nil
}

func (*assemblyWorkspace) OffloadContext(context.Context, string, []*message.Message) (string, error) {
	return "context.jsonl", nil
}

func (*assemblyWorkspace) OffloadToolResult(context.Context, string, *message.ToolResultBlock) (string, error) {
	return "tool-result.txt", nil
}

func (*assemblyWorkspace) OffloadDataBlock(context.Context, *message.DataBlock) (*message.DataBlock, error) {
	return message.NewDataBlock(message.NewURLSource("file:///data.bin", "application/octet-stream")), nil
}

func (*assemblyWorkspace) AddMCP(context.Context, MCPClient) error { return nil }

func (*assemblyWorkspace) RemoveMCP(context.Context, string) error { return nil }

func (*assemblyWorkspace) AddSkill(context.Context, string) error { return nil }

func (*assemblyWorkspace) RemoveSkill(context.Context, string) error { return nil }

type assemblyMCP struct {
	name         string
	stateful     bool
	connected    bool
	tools        []Tool
	connectErr   error
	listErr      error
	connectCalls int
	listCalls    int
}

func (m *assemblyMCP) Name() string { return m.name }

func (m *assemblyMCP) IsStateful() bool { return m.stateful }

func (m *assemblyMCP) IsConnected() bool { return m.connected }

func (m *assemblyMCP) Connect(context.Context) error {
	m.connectCalls++
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (*assemblyMCP) Close() error { return nil }

func (m *assemblyMCP) ListTools(context.Context) ([]Tool, error) {
	m.listCalls++
	if m.listErr != nil {
		return nil, m.listErr
	}
	return append([]Tool(nil), m.tools...), nil
}

var (
	_ Workspace = (*assemblyWorkspace)(nil)
	_ MCPClient = (*assemblyMCP)(nil)
)
