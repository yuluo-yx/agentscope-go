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

package tool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
)

const (
	coverageSchemaObjectType = "object"
	coverageResetToolsName   = "reset_tools"
)

func TestFunctionToolOptionsAndErrorBranches(t *testing.T) {
	t.Parallel()

	if _, err := NewFunctionTool("Missing", "desc", nil, nil); err == nil {
		t.Fatal("expected nil function handler to fail")
	}
	if _, err := NewStreamFunctionTool(" ", "desc", nil, func(context.Context, map[string]any, *asstate.AgentState) (<-chan ToolChunk, error) {
		return singleChunk(*NewToolChunk(nil)), nil
	}); err == nil {
		t.Fatal("expected empty stream function name to fail")
	}
	if _, err := NewStreamFunctionTool("Stream", "desc", nil, nil); err == nil {
		t.Fatal("expected nil stream handler to fail")
	}

	permissionCalled := false
	tool, err := NewStreamFunctionTool(
		" Stream ",
		" desc ",
		map[string]any{"properties": map[string]any{"value": map[string]any{"type": "string"}}},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (<-chan ToolChunk, error) {
			if input == nil {
				t.Fatal("Execute should normalize nil input to an empty map")
			}
			return singleChunk(*NewToolChunk(message.ContentBlockList{message.NewTextBlock("ok")}, WithToolChunkState(message.ToolResultSuccess))), nil
		},
		WithFunctionConcurrencySafe(false),
		WithFunctionReadOnly(true),
		WithFunctionStateInjected(true),
		WithFunctionExternalTool(true),
		WithFunctionMCP("filesystem"),
		WithFunctionSuggestedRule("value:*"),
		WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			permissionCalled = true
			return &permission.Decision{Behavior: permission.BehaviorAllow}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewStreamFunctionTool returned error: %v", err)
	}
	if tool.Name() != "Stream" || tool.Description() != "desc" {
		t.Fatalf("name/description mismatch: %q %q", tool.Name(), tool.Description())
	}
	if tool.IsConcurrencySafe() || !tool.IsReadOnly() || !tool.IsStateInjected() || !tool.IsExternalTool() || !tool.IsMCP() || tool.MCPName() != "filesystem" {
		t.Fatalf("function option flags mismatch")
	}
	schema := tool.InputSchema()
	schema["properties"] = "changed"
	if _, ok := tool.InputSchema()["properties"].(map[string]any); !ok {
		t.Fatalf("InputSchema should clone caller mutation: %#v", tool.InputSchema())
	}
	formattedSchema, err := schemaForTool(tool)
	if err != nil {
		t.Fatalf("schemaForTool returned error: %v", err)
	}
	if formattedSchema.Function.Parameters["type"] != coverageSchemaObjectType {
		t.Fatalf("schemaForTool should add object type: %#v", formattedSchema.Function.Parameters)
	}
	decision, err := tool.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAllow || !permissionCalled {
		t.Fatalf("custom permission mismatch: decision=%#v err=%v called=%v", decision, err, permissionCalled)
	}
	suggestions := tool.GenerateSuggestions(nil)
	if len(suggestions) != 1 || suggestions[0].RuleContent != "value:*" {
		t.Fatalf("suggestions mismatch: %#v", suggestions)
	}
	chunks, err := tool.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if chunk := <-chunks; chunk.State != message.ToolResultSuccess {
		t.Fatalf("chunk mismatch: %#v", chunk)
	}

	if got := cloneSchemaOrObject(nil); got["type"] != coverageSchemaObjectType {
		t.Fatalf("cloneSchemaOrObject nil mismatch: %#v", got)
	}
	if chunk := errorChunk("id-1", "boom"); chunk.ID != "id-1" || chunk.State != message.ToolResultError {
		t.Fatalf("errorChunk mismatch: %#v", chunk)
	}
	if chunk := errorChunk("", "boom"); chunk.ID == "" {
		t.Fatalf("errorChunk should generate an id when empty: %#v", chunk)
	}
}

func TestToolkitErrorBranches(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewToolkitWithMCPs(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("NewToolkitWithMCPs canceled error = %v", err)
	}
	if _, err := NewToolkitWithMCPs(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("NewToolkitWithMCPs nil client error = %v", err)
	}
	listErr := errors.New("list failed")
	if _, err := NewToolkitWithMCPs(context.Background(), nil, &coverageMCPClient{listErr: listErr}); !errors.Is(err, listErr) {
		t.Fatalf("NewToolkitWithMCPs list error = %v", err)
	}
	if _, err := NewToolkitWithGroups(nil, nil); err == nil || !strings.Contains(err.Error(), "nil tool group") {
		t.Fatalf("NewToolkitWithGroups nil group error = %v", err)
	}
	basic, err := NewGroup("basic")
	if err != nil {
		t.Fatalf("NewGroup basic returned error: %v", err)
	}
	if _, err := NewToolkitWithGroups(nil, basic); err == nil || !strings.Contains(err.Error(), "basic group is reserved") {
		t.Fatalf("basic group error = %v", err)
	}
	if _, err := NewToolkitWithGroups([]Tool{nil}); err == nil || !strings.Contains(err.Error(), "nil tool") {
		t.Fatalf("nil tool error = %v", err)
	}
	if _, err := NewGroup(" "); err == nil || !strings.Contains(err.Error(), "group name is required") {
		t.Fatalf("empty group error = %v", err)
	}

	invalidSchema := coverageTool{name: "Invalid", schema: map[string]any{"type": "array"}}
	if _, err := NewToolkit(invalidSchema); err != nil {
		t.Fatalf("NewToolkit invalid schema tool should construct: %v", err)
	}
	if _, err := schemaForTool(nil); err == nil || !strings.Contains(err.Error(), "nil tool") {
		t.Fatalf("schemaForTool nil error = %v", err)
	}
	if _, err := schemaForTool(coverageTool{name: " "}); err == nil || !strings.Contains(err.Error(), "schema tool name") {
		t.Fatalf("schemaForTool empty name error = %v", err)
	}
	if _, err := schemaForTool(invalidSchema); err == nil || !strings.Contains(err.Error(), "must be an object") {
		t.Fatalf("schemaForTool invalid schema error = %v", err)
	}

	normal := coverageTool{name: "Normal", schema: nil}
	groupTool := coverageTool{name: "GroupTool"}
	group, err := NewGroup("search", WithGroupDescription("Search"), WithGroupTools(groupTool))
	if err != nil {
		t.Fatalf("NewGroup search returned error: %v", err)
	}
	kit, err := NewToolkitWithGroups([]Tool{normal}, group)
	if err != nil {
		t.Fatalf("NewToolkitWithGroups returned error: %v", err)
	}
	if tools := kit.AvailableTools(); len(tools) != 2 || tools[0].Group != basicGroupName || tools[1].Tool.Name() != coverageResetToolsName {
		t.Fatalf("AvailableTools mismatch: %#v", tools)
	}
	if found, ok := kit.FindTool("Normal"); !ok || found.Name() != "Normal" {
		t.Fatalf("FindTool normal mismatch: %#v %v", found, ok)
	}
	if response, err := kit.RunTool(context.Background(), message.NewToolCallBlock("call-group", "GroupTool", "{}"), asstate.NewAgentState()); err != nil || response.State != message.ToolResultError || !strings.Contains(*response.GetTextContent(""), "inactive group") {
		t.Fatalf("inactive group response mismatch: response=%#v err=%v", response, err)
	}
	if _, err := kit.RunTool(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "nil tool call") {
		t.Fatalf("RunTool nil call error = %v", err)
	}

	nilStreamKit, err := NewToolkit(coverageTool{name: "NilStream", nilStream: true})
	if err != nil {
		t.Fatalf("NewToolkit nil stream returned error: %v", err)
	}
	if response, err := nilStreamKit.RunTool(context.Background(), message.NewToolCallBlock("call-nil", "NilStream", "{}"), nil); err != nil || response.State != message.ToolResultError || !strings.Contains(*response.GetTextContent(""), "nil chunk stream") {
		t.Fatalf("nil stream response mismatch: response=%#v err=%v", response, err)
	}
	errorKit, err := NewToolkit(coverageTool{name: "Error", execErr: errors.New("failed")})
	if err != nil {
		t.Fatalf("NewToolkit error tool returned error: %v", err)
	}
	if response, err := errorKit.RunTool(context.Background(), message.NewToolCallBlock("call-error", "Error", "{}"), nil); err != nil || response.State != message.ToolResultError || !strings.Contains(*response.GetTextContent(""), "failed") {
		t.Fatalf("error response mismatch: response=%#v err=%v", response, err)
	}

	if groups := activeGroupsFromState(nil); groups != nil {
		t.Fatalf("nil activeGroupsFromState = %#v", groups)
	}
	state := asstate.NewAgentState()
	state.ToolContext.ActivatedGroups = []string{" search ", "", "write"}
	groups := activeGroupsFromState(state)
	groups[0] = "changed"
	if state.ToolContext.ActivatedGroups[0] != " search " {
		t.Fatalf("activeGroupsFromState should clone: %#v", state.ToolContext.ActivatedGroups)
	}
	active := activeSet(state.ToolContext.ActivatedGroups)
	if !active["search"] || !active["write"] || active[""] {
		t.Fatalf("activeSet mismatch: %#v", active)
	}
}

func TestResetToolsBranches(t *testing.T) {
	t.Parallel()

	reset := NewResetTools([]GroupInfo{
		{Name: " ", Description: "ignored"},
		{Name: " search ", Description: " Search desc ", Instructions: " Use rg "},
		{Name: "write", Description: "Write desc"},
	})
	if reset.Name() != coverageResetToolsName || reset.Description() == "" {
		t.Fatalf("reset identity mismatch")
	}
	if reset.IsConcurrencySafe() || reset.IsReadOnly() || !reset.IsStateInjected() || reset.IsExternalTool() || reset.IsMCP() || reset.MCPName() != "" {
		t.Fatalf("reset flags mismatch")
	}
	schema := reset.InputSchema()
	schema["type"] = "changed"
	if reset.InputSchema()["type"] != coverageSchemaObjectType {
		t.Fatalf("InputSchema should be cloned: %#v", reset.InputSchema())
	}
	decision, err := reset.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("reset permission mismatch: %#v err=%v", decision, err)
	}
	if !reset.MatchRule("", nil) || reset.MatchRule("x", nil) {
		t.Fatalf("reset MatchRule mismatch")
	}
	if suggestions := reset.GenerateSuggestions(nil); len(suggestions) != 1 || suggestions[0].ToolName != coverageResetToolsName {
		t.Fatalf("reset suggestions mismatch: %#v", suggestions)
	}

	chunks, err := reset.Execute(context.Background(), map[string]any{"search": true, "write": false}, nil)
	if err != nil {
		t.Fatalf("reset Execute nil state returned error: %v", err)
	}
	chunk := <-chunks
	text := chunk.Content.GetTextContent("")
	if chunk.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, "search") || !strings.Contains(*text, "Use rg") {
		t.Fatalf("reset active chunk mismatch: %#v", chunk)
	}

	state := asstate.NewAgentState()
	chunks, err = reset.Execute(context.Background(), map[string]any{}, state)
	if err != nil {
		t.Fatalf("reset Execute empty returned error: %v", err)
	}
	chunk = <-chunks
	text = chunk.Content.GetTextContent("")
	if len(state.ToolContext.ActivatedGroups) != 0 || text == nil || !strings.Contains(*text, "No optional") {
		t.Fatalf("reset empty mismatch: state=%#v chunk=%#v", state.ToolContext, chunk)
	}
}

type coverageTool struct {
	name      string
	schema    map[string]any
	execErr   error
	nilStream bool
}

func (t coverageTool) Name() string { return t.name }

func (coverageTool) Description() string { return "coverage tool" }

func (t coverageTool) InputSchema() map[string]any { return t.schema }

func (coverageTool) IsConcurrencySafe() bool { return true }

func (coverageTool) IsReadOnly() bool { return true }

func (coverageTool) IsExternalTool() bool { return false }

func (coverageTool) IsStateInjected() bool { return false }

func (coverageTool) IsMCP() bool { return false }

func (coverageTool) MCPName() string { return "" }

func (coverageTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	return &permission.Decision{Behavior: permission.BehaviorAllow}, nil
}

func (coverageTool) MatchRule(string, map[string]any) bool { return false }

func (coverageTool) GenerateSuggestions(map[string]any) []permission.Rule { return nil }

func (t coverageTool) Execute(context.Context, map[string]any, *asstate.AgentState) (<-chan ToolChunk, error) {
	if t.execErr != nil {
		return nil, t.execErr
	}
	if t.nilStream {
		return nil, nil
	}
	return singleChunk(*NewToolChunk(message.ContentBlockList{message.NewTextBlock(t.name)}, WithToolChunkState(message.ToolResultSuccess))), nil
}

type coverageMCPClient struct {
	listErr error
}

func (coverageMCPClient) Name() string { return "coverage-mcp" }

func (coverageMCPClient) IsStateful() bool { return false }

func (coverageMCPClient) IsConnected() bool { return true }

func (coverageMCPClient) Connect(context.Context) error { return nil }

func (coverageMCPClient) Close() error { return nil }

func (c coverageMCPClient) ListTools(context.Context) ([]Tool, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return []Tool{coverageTool{name: "MCPTool"}}, nil
}
