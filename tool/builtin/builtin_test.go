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

package builtin_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/tool/builtin"
)

func TestReadWriteEditUseAbsolutePathsAndReadCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filePath, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	state := astate.NewAgentState()

	readResp := runTool(t, builtin.NewRead(), map[string]any{
		"file_path": filePath,
		"offset":    2,
		"limit":     1,
	}, state)
	if readResp.State != message.ToolResultSuccess {
		t.Fatalf("Read should succeed, got %#v", readResp)
	}
	if got := readResp.GetTextContent(""); got == nil || !strings.Contains(*got, "     2\ttwo") {
		t.Fatalf("Read output should include padded line numbers, got %#v", got)
	}
	if _, ok := state.ToolContext.GetCache(filePath); !ok {
		t.Fatal("Read should cache file content in AgentState")
	}

	existingWithoutRead := runTool(t, builtin.NewWrite(), map[string]any{
		"file_path": filePath,
		"content":   "changed\n",
	}, astate.NewAgentState())
	if text := existingWithoutRead.GetTextContent(""); existingWithoutRead.State != message.ToolResultError || text == nil || !strings.Contains(*text, "has not been read yet") {
		t.Fatalf("Write should require prior Read for existing files, got %#v", existingWithoutRead)
	}

	writeResp := runTool(t, builtin.NewWrite(), map[string]any{
		"file_path": filePath,
		"content":   "alpha\nbeta\n",
	}, state)
	if writeResp.State != message.ToolResultSuccess {
		t.Fatalf("Write after Read should succeed, got %#v", writeResp)
	}
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(bytes) != "alpha\nbeta\n" {
		t.Fatalf("Write did not update file: %q", string(bytes))
	}

	state = astate.NewAgentState()
	_ = runTool(t, builtin.NewRead(), map[string]any{"file_path": filePath}, state)
	editResp := runTool(t, builtin.NewEdit(), map[string]any{
		"file_path":  filePath,
		"old_string": "alpha",
		"new_string": "gamma",
	}, state)
	if editResp.State != message.ToolResultSuccess {
		t.Fatalf("Edit after Read should succeed, got %#v", editResp)
	}
	bytes, err = os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if !strings.Contains(string(bytes), "gamma\nbeta") {
		t.Fatalf("Edit did not replace content: %q", string(bytes))
	}
}

func TestFileToolPermissionsMatchPythonSafetyRules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "safe.txt")
	ctx := permission.NewContext(permission.ModeAcceptEdits)
	ctx.WorkingDirectories[dir] = permission.AdditionalWorkingDirectory{Path: dir, Source: "test"}

	decision, err := builtin.NewWrite().CheckPermissions(context.Background(), map[string]any{"file_path": filePath}, ctx)
	if err != nil {
		t.Fatalf("Write CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("AcceptEdits working directory should allow Write, got %#v", decision)
	}

	decision, err = builtin.NewEdit().CheckPermissions(context.Background(), map[string]any{"file_path": filepath.Join(dir, ".env")}, ctx)
	if err != nil {
		t.Fatalf("Edit CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAsk || !strings.Contains(decision.DecisionReason, "Safety check") {
		t.Fatalf("dangerous file should require explicit review, got %#v", decision)
	}

	decision, err = builtin.NewRead().CheckPermissions(context.Background(), map[string]any{"file_path": filePath}, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("Read CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("Read should be allowed as read-only, got %#v", decision)
	}
}

func TestBashExecutesAndChecksDangerousCommands(t *testing.T) {
	t.Parallel()

	bash := builtin.NewBash()
	decision, err := bash.CheckPermissions(context.Background(), map[string]any{"command": "pwd"}, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("Bash CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("read-only bash command should be allowed, got %#v", decision)
	}

	decision, err = bash.CheckPermissions(context.Background(), map[string]any{"command": "rm -rf /"}, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("Bash dangerous CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAsk || !strings.Contains(decision.DecisionReason, "Safety check") {
		t.Fatalf("dangerous bash command should ask with safety reason, got %#v", decision)
	}

	response := runTool(t, bash, map[string]any{"command": "printf hello"}, astate.NewAgentState())
	if text := response.GetTextContent(""); response.State != message.ToolResultSuccess || text == nil || strings.TrimSpace(*text) != "hello" {
		t.Fatalf("Bash execution output mismatch: %#v", response)
	}
}

func TestBashUsesShellSyntaxForReadOnlyAndSuggestions(t *testing.T) {
	t.Parallel()

	bash := builtin.NewBash()
	decision, err := bash.CheckPermissions(context.Background(), map[string]any{
		"command": "printf 'a|b;c'",
	}, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("Bash CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("quoted shell operators should not split read-only commands, got %#v", decision)
	}

	suggestions := bash.GenerateSuggestions(map[string]any{
		"command": "GOFLAGS=-count=1 go test ./...",
	})
	if len(suggestions) != 1 || suggestions[0].RuleContent != "go test:*" {
		t.Fatalf("environment assignments should be skipped in suggestions, got %#v", suggestions)
	}
}

func TestBashPassthroughAllowsEngineRules(t *testing.T) {
	t.Parallel()

	engine := permission.NewEngine(permission.NewContext(permission.ModeDefault))
	engine.AddRule(permission.Rule{
		ToolName:    "Bash",
		RuleContent: "go test:*",
		Behavior:    permission.BehaviorAllow,
		Source:      "test",
	})

	decision, err := engine.CheckPermission(context.Background(), builtin.NewBash(), map[string]any{
		"command": "GOFLAGS=-count=1 go test ./...",
	})
	if err != nil {
		t.Fatalf("CheckPermission returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("allow rule should apply after Bash-specific checks, got %#v", decision)
	}
}

func TestBashSafetyChecksBypassImmunePatterns(t *testing.T) {
	t.Parallel()

	engine := permission.NewEngine(permission.NewContext(permission.ModeBypass))
	bash := builtin.NewBash()

	for _, command := range []string{
		"printf token > .env",
		"sed 's/a/b/e' file.txt",
		"sed -i.bak 's/a/b/' .env",
		"rm -rf './*'",
		"sudo dd if=/dev/zero of=/dev/sda",
		"sudo mkfs /dev/sda",
		"sudo chmod 777 /",
		"sudo chown -R root /",
		"sudo kill -9 1",
	} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			decision, err := engine.CheckPermission(context.Background(), bash, map[string]any{"command": command})
			if err != nil {
				t.Fatalf("CheckPermission returned error: %v", err)
			}
			if decision.Behavior != permission.BehaviorAsk || !strings.Contains(decision.DecisionReason, "Safety check") {
				t.Fatalf("safety-sensitive command should ask even in bypass mode, got %#v", decision)
			}
		})
	}
}

func TestWriteAndEditRequireStateForExistingFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filePath, []byte("old\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	writeResp := runTool(t, builtin.NewWrite(), map[string]any{
		"file_path": filePath,
		"content":   "new\n",
	}, nil)
	if text := writeResp.GetTextContent(""); writeResp.State != message.ToolResultError || text == nil || !strings.Contains(*text, "agent state required") {
		t.Fatalf("Write should require state for existing files, got %#v", writeResp)
	}

	editResp := runTool(t, builtin.NewEdit(), map[string]any{
		"file_path":  filePath,
		"old_string": "old",
		"new_string": "new",
	}, nil)
	if text := editResp.GetTextContent(""); editResp.State != message.ToolResultError || text == nil || !strings.Contains(*text, "agent state required") {
		t.Fatalf("Edit should require state for existing files, got %#v", editResp)
	}
}

func TestGlobAndGrepSearchFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\nconst word = \"needle\"\n"), 0o600); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	globResp := runTool(t, builtin.NewGlob(), map[string]any{
		"pattern": "**/*.go",
		"path":    dir,
	}, astate.NewAgentState())
	if globResp.State != message.ToolResultSuccess {
		t.Fatalf("Glob should succeed, got %#v", globResp)
	}
	globOutput := globResp.GetTextContent("")
	if globOutput == nil {
		t.Fatalf("Glob output should contain text, got %#v", globResp.Content)
	}
	if !strings.Contains(*globOutput, "a.go") || strings.Contains(*globOutput, "b.txt") {
		t.Fatalf("Glob output should include only Go file, got %q", *globOutput)
	}

	grepResp := runTool(t, builtin.NewGrep(), map[string]any{
		"pattern": "needle",
		"path":    dir,
		"glob":    "*.go",
	}, astate.NewAgentState())
	if grepResp.State != message.ToolResultSuccess {
		t.Fatalf("Grep should succeed, got %#v", grepResp)
	}
	grepOutput := grepResp.GetTextContent("")
	if grepOutput == nil {
		t.Fatalf("Grep output should contain text, got %#v", grepResp.Content)
	}
	if !strings.Contains(*grepOutput, "a.go") || strings.Contains(*grepOutput, "b.txt") {
		t.Fatalf("Grep output should respect glob filter, got %q", *grepOutput)
	}
}

func TestResetToolsUpdatesActivatedGroups(t *testing.T) {
	t.Parallel()

	reset := builtin.NewResetTools([]builtin.GroupInfo{
		{Name: "search", Description: "Search tools", Instructions: "Use Search."},
		{Name: "write", Description: "Write tools"},
	})
	state := astate.NewAgentState()
	response := runTool(t, reset, map[string]any{"search": true, "write": false}, state)
	if response.State != message.ToolResultSuccess {
		t.Fatalf("reset_tools should succeed, got %#v", response)
	}
	if len(state.ToolContext.ActivatedGroups) != 1 || state.ToolContext.ActivatedGroups[0] != "search" {
		t.Fatalf("activated groups not updated: %#v", state.ToolContext.ActivatedGroups)
	}
	if text := response.GetTextContent(""); text == nil || !strings.Contains(*text, "Use Search.") {
		t.Fatalf("reset_tools should include group instructions, got %#v", text)
	}
}

func runTool(t *testing.T, tool astool.Tool, input map[string]any, state *astate.AgentState) *astool.ToolResponse {
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
