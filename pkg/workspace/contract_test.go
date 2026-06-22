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

package workspace_test

import (
	"context"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	"github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/skill"
	"github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

func TestBuildAgentResourcesCombinesWorkspaceResources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	echo, err := tool.NewFunctionTool(
		"WorkspaceEcho",
		"Echo from workspace.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *state.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("ok")}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	mcpTool, err := tool.NewFunctionTool(
		"MCPWeather",
		"Weather from MCP.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *state.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("sunny")}, nil
		},
		tool.WithFunctionReadOnly(true),
		tool.WithFunctionMCP("weather"),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool MCP returned error: %v", err)
	}

	offloaded := message.NewDataBlock(
		message.NewBase64Source("aGVsbG8=", "text/plain"),
		message.WithDataBlockID("data-1"),
	)
	ws := &contractWorkspace{
		instructions: "<workspace>Use /tmp/workspace.</workspace>",
		tools:        []workspace.Tool{echo},
		mcps:         []workspace.MCPClient{contractMCP{name: "weather", tools: []workspace.Tool{mcpTool}}},
		skills: []skill.Skill{{
			Name:        "review",
			Description: "Review code",
			Dir:         "/tmp/workspace/skills/review",
			Markdown:    "Read files before reviewing.",
		}},
		offloadedData: message.NewDataBlock(message.NewURLSource("file:///tmp/workspace/data/hello.txt", "text/plain"), message.WithDataBlockID(offloaded.ID)),
	}

	resources, err := workspace.BuildAgentResources(ctx, ws)
	if err != nil {
		t.Fatalf("BuildAgentResources returned error: %v", err)
	}
	if resources.Offloader != ws {
		t.Fatalf("workspace should be exposed as offloader")
	}
	if !strings.Contains(resources.SystemPrompt, "Use /tmp/workspace") ||
		!strings.Contains(resources.SystemPrompt, "<agent-skills>") ||
		!strings.Contains(resources.SystemPrompt, "<name>review</name>") {
		t.Fatalf("system prompt should include workspace instructions and skills: %s", resources.SystemPrompt)
	}
	schemas, err := resources.Toolkit.ToolSchemas()
	if err != nil {
		t.Fatalf("ToolSchemas returned error: %v", err)
	}
	if got := schemaNames(schemas); strings.Join(got, ",") != "WorkspaceEcho,MCPWeather,Skill" {
		t.Fatalf("unexpected toolkit schemas: %#v", got)
	}

	gotData, err := resources.Offloader.OffloadDataBlock(ctx, offloaded)
	if err != nil {
		t.Fatalf("OffloadDataBlock returned error: %v", err)
	}
	source, ok := gotData.Source.(*message.URLSource)
	if !ok || source.URL != "file:///tmp/workspace/data/hello.txt" {
		t.Fatalf("OffloadDataBlock should return offloaded URL block: %#v", gotData)
	}
}

type contractWorkspace struct {
	instructions  string
	tools         []workspace.Tool
	mcps          []workspace.MCPClient
	skills        []skill.Skill
	offloadedData *message.DataBlock
}

func (w *contractWorkspace) WorkspaceID() string              { return "contract-workspace" }
func (w *contractWorkspace) IsAlive() bool                    { return true }
func (w *contractWorkspace) Initialize(context.Context) error { return nil }
func (w *contractWorkspace) Close(context.Context) error      { return nil }
func (w *contractWorkspace) Reset(context.Context) error      { return nil }
func (w *contractWorkspace) GetInstructions(context.Context) (string, error) {
	return w.instructions, nil
}

func (w *contractWorkspace) ListTools(context.Context) ([]workspace.Tool, error) {
	return append([]workspace.Tool(nil), w.tools...), nil
}

func (w *contractWorkspace) ListMCPs(context.Context) ([]workspace.MCPClient, error) {
	return append([]workspace.MCPClient(nil), w.mcps...), nil
}

func (w *contractWorkspace) ListSkills(context.Context) ([]skill.Skill, error) {
	return append([]skill.Skill(nil), w.skills...), nil
}

func (w *contractWorkspace) OffloadContext(context.Context, string, []*message.Message) (string, error) {
	return "context.jsonl", nil
}

func (w *contractWorkspace) OffloadToolResult(context.Context, string, *message.ToolResultBlock) (string, error) {
	return "tool-result.txt", nil
}

func (w *contractWorkspace) OffloadDataBlock(context.Context, *message.DataBlock) (*message.DataBlock, error) {
	return w.offloadedData, nil
}
func (w *contractWorkspace) AddMCP(context.Context, workspace.MCPClient) error { return nil }
func (w *contractWorkspace) RemoveMCP(context.Context, string) error           { return nil }
func (w *contractWorkspace) AddSkill(context.Context, string) error            { return nil }
func (w *contractWorkspace) RemoveSkill(context.Context, string) error         { return nil }

type contractMCP struct {
	name  string
	tools []workspace.Tool
}

func (m contractMCP) Name() string                  { return m.name }
func (m contractMCP) IsStateful() bool              { return false }
func (m contractMCP) IsConnected() bool             { return true }
func (m contractMCP) Connect(context.Context) error { return nil }
func (m contractMCP) Close() error                  { return nil }
func (m contractMCP) ListTools(context.Context) ([]workspace.Tool, error) {
	return append([]workspace.Tool(nil), m.tools...), nil
}

func schemaNames(schemas []workspace.ToolSchema) []string {
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		names = append(names, schema.Function.Name)
	}
	return names
}

var (
	_ workspace.Workspace = (*contractWorkspace)(nil)
	_ workspace.MCPClient = contractMCP{}
	_ workspace.Offloader = (*contractWorkspace)(nil)
)

func TestSkillToolAllowsReadingWorkspaceSkillMarkdown(t *testing.T) {
	t.Parallel()

	resources, err := workspace.BuildAgentResources(context.Background(), &contractWorkspace{
		instructions: "<workspace/>",
		skills: []skill.Skill{{
			Name:        "review",
			Description: "Review code",
			Markdown:    "Use the code review checklist.",
		}},
	})
	if err != nil {
		t.Fatalf("BuildAgentResources returned error: %v", err)
	}
	viewer, ok := resources.Toolkit.FindTool("Skill")
	if !ok {
		t.Fatalf("Skill tool should be registered when workspace has skills")
	}
	decision, err := viewer.CheckPermissions(context.Background(), map[string]any{"skill": "review"}, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("Skill tool should be read-only allow, got %s", decision.Behavior)
	}
	response, err := resources.Toolkit.RunTool(context.Background(), message.NewToolCallBlock("call-1", "Skill", `{"skill":"review"}`), state.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool returned error: %v", err)
	}
	text := response.GetTextContent("")
	if response.State != message.ToolResultSuccess || text == nil || *text != "Use the code review checklist." {
		t.Fatalf("unexpected skill tool response: %#v text=%v", response, text)
	}
}
