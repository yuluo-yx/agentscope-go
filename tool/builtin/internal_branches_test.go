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

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

func TestBashInternalSafetyAndPrefixBranches(t *testing.T) {
	t.Parallel()

	riskCases := []struct {
		command string
		want    string
	}{
		{command: "echo $(pwd)", want: "command substitution"},
		{command: "(echo hi)", want: "subshell"},
		{command: "if true; then echo hi; fi", want: "control flow"},
	}
	for _, tt := range riskCases {
		file, err := parseBash(tt.command)
		if err != nil {
			t.Fatalf("parseBash(%q): %v", tt.command, err)
		}
		if got := injectionRisk(file); !strings.Contains(got, tt.want) {
			t.Fatalf("injectionRisk(%q)=%q, want contains %q", tt.command, got, tt.want)
		}
	}

	dangerCases := []struct {
		words []string
		want  string
	}{
		{words: []string{"rm", "-rf", "tmp"}, want: "rm -rf"},
		{words: []string{"rm", "*"}, want: "dangerous removal path *"},
		{words: []string{"rmdir", "/tmp"}, want: "dangerous removal path /tmp"},
		{words: []string{"sudo", "chmod", "-R", "777", "/opt/app"}, want: "sudo chmod -R 777"},
		{words: []string{"chown", "-R", "root", "/opt/app"}, want: "chown -R"},
		{words: []string{"kill", "-9", "1"}, want: "kill -9"},
	}
	for _, tt := range dangerCases {
		if got := dangerousWords(tt.words); got != tt.want {
			t.Fatalf("dangerousWords(%#v)=%q, want %q", tt.words, got, tt.want)
		}
	}

	sedCases := []struct {
		args []string
		want string
	}{
		{args: []string{}, want: "missing expression"},
		{args: []string{"-z", "s/a/b/"}, want: "flag -z"},
		{args: []string{"s/a/b/w out.txt"}, want: "write operation"},
		{args: []string{"s/a/b/e"}, want: "execute operation"},
		{args: []string{"{p}"}, want: "curly"},
		{args: []string{"!p"}, want: "negation"},
		{args: []string{"1#comment"}, want: "comments"},
		{args: []string{"1,2,3p"}, want: "allowlist"},
	}
	for _, tt := range sedCases {
		if got := checkSedArgs(tt.args); !strings.Contains(got, tt.want) {
			t.Fatalf("checkSedArgs(%#v)=%q, want contains %q", tt.args, got, tt.want)
		}
	}
	if got := checkSedArgs([]string{"-n", "1,2p"}); got != "" {
		t.Fatalf("safe sed print should be allowed, got %q", got)
	}
	if got := checkSedArgs([]string{"s|a|b|g"}); got != "" {
		t.Fatalf("safe sed substitution should be allowed, got %q", got)
	}
	if got := commandPrefix("A=B malformed '"); got != "malformed '" {
		t.Fatalf("fallback command prefix mismatch: %q", got)
	}
	if !matchBashPrefixRule("go test", "go test ./... && go test ./tool/...") {
		t.Fatalf("prefix rule should match every command call")
	}
	if matchBashPrefixRule("go test", "go test ./... && go vet ./...") {
		t.Fatalf("prefix rule should reject a non-matching command call")
	}
	if !isRootOrRootChild("/") || !isRootOrRootChild("/var") || isRootOrRootChild("relative/path") {
		t.Fatalf("root child detection mismatch")
	}
}

func TestGlobGrepAndFileToolErrorBranches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "alpha.txt")
	if err := os.WriteFile(filePath, []byte("Needle\nneedle\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "binary.bin"), []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatalf("write binary fixture: %v", err)
	}

	assertToolTextContains(t, runInternalTool(t, NewGlob(), map[string]any{}, nil), message.ToolResultError, "pattern is required")
	assertToolTextContains(t, runInternalTool(t, NewGlob(), map[string]any{"pattern": "*.go", "path": filepath.Join(dir, "missing")}, nil), message.ToolResultError, "Directory not found")
	assertToolTextContains(t, runInternalTool(t, NewGlob(), map[string]any{"pattern": "*.go", "path": filePath}, nil), message.ToolResultError, "not a directory")
	assertToolTextContains(t, runInternalTool(t, NewGlob(), map[string]any{"pattern": "*.go", "path": dir}, nil), message.ToolResultSuccess, "No files found")

	assertToolTextContains(t, runInternalTool(t, NewGrep(), map[string]any{"pattern": "["}, nil), message.ToolResultError, "invalid regex")
	assertToolTextContains(t, runInternalTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": filepath.Join(dir, "missing")}, nil), message.ToolResultError, "path not found")
	assertToolTextContains(t, runInternalTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": filePath, "glob": "*.go"}, nil), message.ToolResultSuccess, "No matches")
	assertToolTextContains(t, runInternalTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": dir, "output_mode": "count", "case_insensitive": true}, nil), message.ToolResultSuccess, "alpha.txt:2")
	assertToolTextContains(t, runInternalTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": dir, "output_mode": "files"}, nil), message.ToolResultSuccess, "alpha.txt")
	assertToolTextContains(t, runInternalTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": dir, "head_limit": 1}, nil), message.ToolResultSuccess, "needle")

	assertToolTextContains(t, runInternalTool(t, NewRead(), map[string]any{"file_path": dir}, astate.NewAgentState()), message.ToolResultError, "directory")
	assertToolTextContains(t, runInternalTool(t, NewRead(), map[string]any{"file_path": filepath.Join(dir, "missing.txt")}, astate.NewAgentState()), message.ToolResultError, "does not exist")
	assertToolTextContains(t, runInternalTool(t, NewEdit(), map[string]any{"file_path": filePath, "old_string": "same", "new_string": "same"}, astate.NewAgentState()), message.ToolResultError, "identical")
	assertToolTextContains(t, runInternalTool(t, NewEdit(), map[string]any{"file_path": filepath.Join(dir, "missing.txt"), "old_string": "x", "new_string": "y"}, astate.NewAgentState()), message.ToolResultError, "File not found")
}

func runInternalTool(t *testing.T, tool astool.Tool, input map[string]any, state *astate.AgentState) *astool.ToolResponse {
	t.Helper()

	chunks, err := tool.Execute(context.Background(), input, state)
	if err != nil {
		t.Fatalf("%s Execute returned error: %v", tool.Name(), err)
	}
	response := astool.NewToolResponse(astool.WithToolResponseID("call-1"))
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			t.Fatalf("AppendChunk returned error: %v", err)
		}
	}
	return response
}

func assertToolTextContains(t *testing.T, response *astool.ToolResponse, state message.ToolResultState, want string) {
	t.Helper()
	if response.State != state {
		t.Fatalf("tool state mismatch: got %s want %s response=%#v", response.State, state, response)
	}
	text := response.GetTextContent("")
	if text == nil || !strings.Contains(*text, want) {
		t.Fatalf("tool response text should contain %q, got %#v", want, text)
	}
}
