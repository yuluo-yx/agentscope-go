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

package types_test

import (
	"testing"

	"github.com/yuluo-yx/agentscope-go/types"
)

func TestJSONSerializableValidation(t *testing.T) {
	t.Parallel()

	valid := map[string]any{
		"name": "Friday",
		"tags": []any{"agent", float64(2), true, nil},
		"nested": map[string]any{
			"enabled": true,
		},
	}
	if !types.IsJSONSerializable(valid) {
		t.Fatalf("expected nested JSON value to be serializable")
	}

	invalid := map[string]any{"bad": make(chan struct{})}
	if types.IsJSONSerializable(invalid) {
		t.Fatalf("channel should not be JSON serializable")
	}
}

func TestToolChoiceValidation(t *testing.T) {
	t.Parallel()

	choice, err := types.NewToolChoice("Read", "Read", "Write")
	if err != nil {
		t.Fatalf("NewToolChoice returned error: %v", err)
	}
	if err := choice.Validate([]string{"Read", "Write", "Bash"}); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	if _, err := types.NewToolChoice("Read", "Write"); err == nil {
		t.Fatal("specific tool mode should be rejected when excluded by tools filter")
	}
	if err := (&types.ToolChoice{Mode: "Missing"}).Validate([]string{"Read"}); err == nil {
		t.Fatal("missing forced tool should return validation error")
	}
}

func TestJSONPrimitiveValidation(t *testing.T) {
	t.Parallel()

	if !types.IsJSONPrimitive("text") || !types.IsJSONPrimitive(1) || !types.IsJSONPrimitive(nil) {
		t.Fatal("string, number and nil should be JSON primitives")
	}
	if types.IsJSONPrimitive([]string{"x"}) {
		t.Fatal("slice should not be JSON primitive")
	}
}

func TestToolChoiceBuiltInModesAndClone(t *testing.T) {
	t.Parallel()

	choice, err := types.NewToolChoice("")
	if err != nil {
		t.Fatalf("empty mode should default to auto: %v", err)
	}
	if choice.Mode != string(types.ToolChoiceAuto) {
		t.Fatalf("unexpected default mode: %q", choice.Mode)
	}
	if err := (&types.ToolChoice{Mode: ""}).Validate(nil); err == nil {
		t.Fatal("empty explicit mode should return validation error")
	}
	if err := (&types.ToolChoice{Mode: string(types.ToolChoiceNone), Tools: []string{"Read"}}).Validate([]string{"Write"}); err == nil {
		t.Fatal("tools filter with unavailable tool should return validation error")
	}

	original := &types.ToolChoice{Mode: "Read", Tools: []string{"Read"}}
	cloned := original.Clone()
	cloned.Tools[0] = "Write"
	if cloned.Mode != "Read" {
		t.Fatalf("clone should preserve mode, got %q", cloned.Mode)
	}
	if original.Tools[0] != "Read" {
		t.Fatalf("clone should not mutate original: %#v", original)
	}
	if (*types.ToolChoice)(nil).Clone() != nil {
		t.Fatal("nil tool choice clone should return nil")
	}
}
