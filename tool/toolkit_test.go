// Copyright 20\d\d AgentScope Go
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

package tool_test

import (
	"context"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func TestToolkitSchemasRespectActivatedGroups(t *testing.T) {
	t.Parallel()

	echo := newTestTool("Echo", "echo input", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{"type": "string"},
		},
	})
	search := newTestTool("Search", "search files", map[string]any{
		"type":       "object",
		"properties": map[string]any{"query": map[string]any{"type": "string"}},
	})
	group, err := tool.NewGroup("search",
		tool.WithGroupDescription("Search tools"),
		tool.WithGroupInstructions("Use search when repository lookup is required."),
		tool.WithGroupTools(search),
	)
	if err != nil {
		t.Fatalf("NewGroup returned error: %v", err)
	}
	kit, err := tool.NewToolkitWithGroups([]tool.Tool{echo}, group)
	if err != nil {
		t.Fatalf("NewToolkitWithGroups returned error: %v", err)
	}

	schemas, err := kit.ToolSchemas()
	if err != nil {
		t.Fatalf("ToolSchemas returned error: %v", err)
	}
	if len(schemas) != 2 {
		t.Fatalf("basic tools plus reset_tools should be visible, got %d", len(schemas))
	}
	if schemas[0].Type != "function" || schemas[0].Function.Name != "Echo" {
		t.Fatalf("unexpected first schema: %#v", schemas[0])
	}
	if schemas[0].Function.Parameters["type"] != "object" {
		t.Fatalf("input schema not preserved: %#v", schemas[0].Function.Parameters)
	}
	if schemas[1].Function.Name != "reset_tools" {
		t.Fatalf("reset_tools should be advertised when optional groups exist: %#v", schemas)
	}

	schemas, err = kit.ToolSchemas("search")
	if err != nil {
		t.Fatalf("ToolSchemas with group returned error: %v", err)
	}
	if len(schemas) != 3 || schemas[1].Function.Name != "Search" {
		t.Fatalf("activated group tool missing from schemas: %#v", schemas)
	}
}

func TestToolkitRejectsInvalidGroupsAndDuplicateTools(t *testing.T) {
	t.Parallel()

	if _, err := tool.NewGroup("filesystem"); err == nil {
		t.Fatal("non-basic group without description should fail")
	}

	dup := newTestTool("Echo", "first", map[string]any{"type": "object"})
	if _, err := tool.NewToolkit(dup, dup); err == nil {
		t.Fatal("duplicate tool names should fail")
	}
}

func TestToolkitCallToolParsesInputAndAccumulatesResponse(t *testing.T) {
	t.Parallel()

	echo := newTestTool("Echo", "echo input", map[string]any{"type": "object"})
	kit, err := tool.NewToolkit(echo)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}

	call := message.NewToolCallBlock("call-1", "Echo", `{"value":"hello"}`)
	response, err := kit.RunTool(context.Background(), call, astate.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool returned error: %v", err)
	}
	if response.ID != "call-1" || response.State != message.ToolResultSuccess {
		t.Fatalf("unexpected response metadata: %#v", response)
	}
	if got := response.Content[0].(*message.TextBlock).Text; got != "hello" {
		t.Fatalf("tool output not accumulated: %q", got)
	}
}

func TestToolkitCallToolReturnsErrorChunksForBadCalls(t *testing.T) {
	t.Parallel()

	kit, err := tool.NewToolkit(newTestTool("Echo", "echo input", map[string]any{"type": "object"}))
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}

	tests := []struct {
		name string
		call *message.ToolCallBlock
		want string
	}{
		{
			name: "unknown tool",
			call: message.NewToolCallBlock("missing", "Missing", `{}`),
			want: "tool Missing not found",
		},
		{
			name: "invalid json",
			call: message.NewToolCallBlock("bad-json", "Echo", `not-json`),
			want: "decode JSON object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			response, err := kit.RunTool(context.Background(), tt.call, astate.NewAgentState())
			if err != nil {
				t.Fatalf("RunTool returned error: %v", err)
			}
			if response.State != message.ToolResultError {
				t.Fatalf("expected error response, got %#v", response)
			}
			got := response.Content[0].(*message.TextBlock).Text
			if !strings.Contains(got, tt.want) {
				t.Fatalf("error text %q does not contain %q", got, tt.want)
			}
		})
	}
}

func TestToolkitUsesActivatedGroupsFromState(t *testing.T) {
	t.Parallel()

	search := newTestTool("Search", "search files", map[string]any{"type": "object"})
	group, err := tool.NewGroup("search", tool.WithGroupDescription("Search tools"), tool.WithGroupTools(search))
	if err != nil {
		t.Fatalf("NewGroup returned error: %v", err)
	}
	kit, err := tool.NewToolkitWithGroups(nil, group)
	if err != nil {
		t.Fatalf("NewToolkitWithGroups returned error: %v", err)
	}

	state := astate.NewAgentState()
	response, err := kit.RunTool(context.Background(), message.NewToolCallBlock("call-1", "Search", `{"value":"x"}`), state)
	if err != nil {
		t.Fatalf("RunTool returned error: %v", err)
	}
	if response.State != message.ToolResultError {
		t.Fatalf("inactive group should return error, got %#v", response)
	}

	state.ToolContext.ActivatedGroups = []string{"search"}
	response, err = kit.RunTool(context.Background(), message.NewToolCallBlock("call-2", "Search", `{"value":"x"}`), state)
	if err != nil {
		t.Fatalf("RunTool active group returned error: %v", err)
	}
	if response.State != message.ToolResultSuccess {
		t.Fatalf("activated group should execute successfully: %#v", response)
	}
}

func TestToolkitMapsContextCanceledToInterruptedChunk(t *testing.T) {
	t.Parallel()

	canceling := cancelingTool{name: "Cancel", description: "cancel", schema: map[string]any{"type": "object"}}
	kit, err := tool.NewToolkit(canceling)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}

	response, err := kit.RunTool(context.Background(), message.NewToolCallBlock("call-cancel", "Cancel", `{}`), astate.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool returned error: %v", err)
	}
	if response.State != message.ToolResultInterrupted {
		t.Fatalf("context cancellation should map to interrupted, got %#v", response)
	}
}

type testTool struct {
	name        string
	description string
	schema      map[string]any
}

type cancelingTool struct {
	name        string
	description string
	schema      map[string]any
}

func (t cancelingTool) Name() string {
	return t.name
}

func (t cancelingTool) Description() string {
	return t.description
}

func (t cancelingTool) InputSchema() map[string]any {
	return t.schema
}

func (t cancelingTool) IsConcurrencySafe() bool {
	return true
}

func (t cancelingTool) IsReadOnly() bool {
	return true
}

func (t cancelingTool) IsExternalTool() bool {
	return false
}

func (t cancelingTool) IsStateInjected() bool {
	return false
}

func (t cancelingTool) IsMCP() bool {
	return false
}

func (t cancelingTool) MCPName() string {
	return ""
}

func (t cancelingTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	return &permission.Decision{Behavior: permission.BehaviorPassthrough}, nil
}

func (t cancelingTool) MatchRule(string, map[string]any) bool {
	return false
}

func (t cancelingTool) GenerateSuggestions(map[string]any) []permission.Rule {
	return nil
}

func newTestTool(name, description string, schema map[string]any) testTool {
	return testTool{name: name, description: description, schema: schema}
}

func (t testTool) Name() string {
	return t.name
}

func (t testTool) Description() string {
	return t.description
}

func (t testTool) InputSchema() map[string]any {
	return t.schema
}

func (t testTool) IsConcurrencySafe() bool {
	return true
}

func (t testTool) IsReadOnly() bool {
	return true
}

func (t testTool) IsExternalTool() bool {
	return false
}

func (t testTool) IsStateInjected() bool {
	return false
}

func (t testTool) IsMCP() bool {
	return false
}

func (t testTool) MCPName() string {
	return ""
}

func (t testTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	return &permission.Decision{Behavior: permission.BehaviorPassthrough}, nil
}

func (t testTool) MatchRule(ruleContent string, input map[string]any) bool {
	value, _ := input["value"].(string)
	return permission.MatchPattern(ruleContent, value)
}

func (t testTool) GenerateSuggestions(map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName:    t.name,
		RuleContent: "",
		Behavior:    permission.BehaviorAllow,
		Source:      "test",
	}}
}

func (t testTool) Execute(ctx context.Context, input map[string]any, _ *astate.AgentState) (<-chan tool.ToolChunk, error) {
	chunks := make(chan tool.ToolChunk, 1)
	go func() {
		defer close(chunks)
		select {
		case <-ctx.Done():
			chunks <- *tool.NewToolChunk("", message.ContentBlockList{message.NewTextBlock(ctx.Err().Error())}, tool.WithToolChunkState(message.ToolResultInterrupted))
		default:
			value, _ := input["value"].(string)
			chunks <- *tool.NewToolChunk("", message.ContentBlockList{message.NewTextBlock(value)}, tool.WithToolChunkState(message.ToolResultSuccess))
		}
	}()
	return chunks, nil
}

func (t cancelingTool) Execute(context.Context, map[string]any, *astate.AgentState) (<-chan tool.ToolChunk, error) {
	return nil, context.Canceled
}
