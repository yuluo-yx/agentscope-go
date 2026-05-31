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

package utils_test

import (
	"reflect"
	"testing"

	"github.com/yuluo-yx/agentscope-go/utils"
)

func TestCloneAnyMapDeepCopiesNestedValues(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"nested": map[string]any{
			"items": []any{
				map[string]any{"name": "first"},
			},
		},
		"tags": []string{"a", "b"},
	}

	cloned := utils.CloneAnyMap(original)
	cloned["nested"].(map[string]any)["items"].([]any)[0].(map[string]any)["name"] = "changed"
	cloned["tags"].([]string)[0] = "changed"

	if got := original["nested"].(map[string]any)["items"].([]any)[0].(map[string]any)["name"]; got != "first" {
		t.Fatalf("nested map should be deep copied, got %q", got)
	}
	if got := original["tags"].([]string)[0]; got != "a" {
		t.Fatalf("string slice should be copied, got %q", got)
	}
}

func TestCloneAnyHandlesNilAndScalars(t *testing.T) {
	t.Parallel()

	if utils.CloneAnyMap(nil) != nil {
		t.Fatal("nil map should stay nil")
	}
	if got := utils.CloneAny("value"); got != "value" {
		t.Fatalf("scalar should be returned unchanged, got %#v", got)
	}

	value := []any{map[string]any{"key": "value"}}
	cloned := utils.CloneAny(value)
	if !reflect.DeepEqual(value, cloned) {
		t.Fatalf("clone should preserve shape: %#v", cloned)
	}
	cloned.([]any)[0].(map[string]any)["key"] = "changed"
	if value[0].(map[string]any)["key"] != "value" {
		t.Fatalf("nested value should be isolated: %#v", value)
	}
}
