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

func TestFunctionToolExecutesAndClonesSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"value": map[string]any{"type": "string"}},
	}
	fn, err := tool.NewFunctionTool("Echo", "Echoes a value", schema,
		func(_ context.Context, input map[string]any, _ *astate.AgentState) (message.ContentBlockList, error) {
			value, _ := input["value"].(string)
			return message.ContentBlockList{message.NewTextBlock(value)}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}

	schema["type"] = "changed"
	if fn.InputSchema()["type"] != "object" {
		t.Fatalf("InputSchema should be cloned from caller mutation: %#v", fn.InputSchema())
	}

	chunks, err := fn.Execute(context.Background(), map[string]any{"value": "hello"}, astate.NewAgentState())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	chunk := <-chunks
	if chunk.State != message.ToolResultSuccess || chunk.Content[0].(*message.TextBlock).Text != "hello" {
		t.Fatalf("unexpected function chunk: %#v", chunk)
	}
}

func TestFunctionToolDefaultsToAskPermission(t *testing.T) {
	t.Parallel()

	fn, err := tool.NewFunctionTool("Echo", "Echoes a value", map[string]any{"type": "object"},
		func(context.Context, map[string]any, *astate.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("ok")}, nil
		})
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}

	decision, err := fn.CheckPermissions(context.Background(), map[string]any{"value": "x"}, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAsk {
		t.Fatalf("custom function tools should ask by default, got %#v", decision)
	}
	if !fn.MatchRule("", map[string]any{"value": "x"}) {
		t.Fatal("empty rule content should match the tool")
	}
	if fn.MatchRule("x", map[string]any{"value": "x"}) {
		t.Fatal("function tools should not infer non-empty rule matches by default")
	}
	suggestions := fn.GenerateSuggestions(map[string]any{"value": "x"})
	if len(suggestions) != 1 || suggestions[0].ToolName != "Echo" || suggestions[0].Behavior != permission.BehaviorAllow {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}
}

func TestFunctionToolWrapsHandlerErrorsAsErrorChunks(t *testing.T) {
	t.Parallel()

	fn, err := tool.NewFunctionTool("Fail", "Fails", map[string]any{"type": "object"},
		func(context.Context, map[string]any, *astate.AgentState) (message.ContentBlockList, error) {
			return nil, errBoom{}
		})
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}

	chunks, err := fn.Execute(context.Background(), nil, astate.NewAgentState())
	if err != nil {
		t.Fatalf("Execute should expose handler failures as chunks, got error: %v", err)
	}
	chunk := <-chunks
	if chunk.State != message.ToolResultError {
		t.Fatalf("expected error chunk, got %#v", chunk)
	}
	if got := chunk.Content[0].(*message.TextBlock).Text; !strings.Contains(got, "boom") {
		t.Fatalf("error chunk should include handler error, got %q", got)
	}
}

type errBoom struct{}

func (errBoom) Error() string {
	return "boom"
}
