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

package task

import (
	"context"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/permission"
)

func TestBaseToolMetadataPermissionsAndSuggestions(t *testing.T) {
	t.Parallel()

	schema := map[string]any{"properties": map[string]any{"task_id": map[string]any{"type": "string"}}}
	base := baseTool{
		name:            "TaskGet",
		description:     "Get one task.",
		schema:          schema,
		concurrencySafe: true,
		readOnly:        true,
	}

	if base.Name() != "TaskGet" || base.Description() != "Get one task." {
		t.Fatalf("base tool identity mismatch: %s %s", base.Name(), base.Description())
	}
	if !base.IsConcurrencySafe() || !base.IsReadOnly() || !base.IsStateInjected() || base.IsExternalTool() || base.IsMCP() || base.MCPName() != "" {
		t.Fatalf("base tool flags mismatch")
	}
	clonedSchema := base.InputSchema()
	clonedSchema["properties"].(map[string]any)["task_id"].(map[string]any)["type"] = "integer"
	if schema["properties"].(map[string]any)["task_id"].(map[string]any)["type"] != "string" {
		t.Fatalf("InputSchema should clone nested schema: %#v", schema)
	}

	decision, err := base.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow || decision.DecisionReason == "" {
		t.Fatalf("permission decision mismatch: %#v", decision)
	}
	if !base.MatchRule("", nil) || base.MatchRule("non-empty", nil) {
		t.Fatalf("MatchRule should only match empty task rules")
	}
	suggestions := base.GenerateSuggestions(nil)
	if len(suggestions) != 1 || suggestions[0].ToolName != "TaskGet" || suggestions[0].Behavior != permission.BehaviorAllow {
		t.Fatalf("suggestion mismatch: %#v", suggestions)
	}
}
