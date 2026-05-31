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

package jsonutil_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/internal/jsonutil"
)

func TestLoadObjectParsesStrictJSONObject(t *testing.T) {
	t.Parallel()

	got, err := jsonutil.LoadObject(`{"name":"Bash","count":2}`)
	if err != nil {
		t.Fatalf("LoadObject returned error: %v", err)
	}

	want := map[string]any{"name": "Bash", "count": float64(2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadObject mismatch:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestLoadObjectRepairsStreamingToolArguments(t *testing.T) {
	t.Parallel()

	got, err := jsonutil.LoadObject("```json\n{\"command\":\"go test ./...\",")
	if err != nil {
		t.Fatalf("LoadObject returned error: %v", err)
	}

	want := map[string]any{"command": "go test ./..."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadObject mismatch:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestLoadObjectRepairsUnclosedString(t *testing.T) {
	t.Parallel()

	got, err := jsonutil.LoadObject(`{"query":"AgentScope`)
	if err != nil {
		t.Fatalf("LoadObject returned error: %v", err)
	}
	if got["query"] != "AgentScope" {
		t.Fatalf("unexpected repaired query: %#v", got)
	}
}

func TestLoadObjectRepairsEscapedQuoteAndTrailingComma(t *testing.T) {
	t.Parallel()

	got, err := jsonutil.LoadObject(`{"query":"a \"quoted\" value",`)
	if err != nil {
		t.Fatalf("LoadObject returned error: %v", err)
	}
	if got["query"] != `a "quoted" value` {
		t.Fatalf("unexpected repaired query: %#v", got)
	}
}

func TestLoadObjectRejectsNonObjectJSON(t *testing.T) {
	t.Parallel()

	_, err := jsonutil.LoadObject(`["not","object"]`)
	if err == nil {
		t.Fatal("LoadObject expected an error for non-object JSON")
	}
	if !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("error should mention JSON object, got: %v", err)
	}
}
