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

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	astate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/builtin"
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
	if writeResp.Metadata["file_path"] != filePath {
		t.Fatalf("Write metadata should include file path, got %#v", writeResp.Metadata)
	}
	writeDiff, _ := writeResp.Metadata["diff"].(string)
	if !strings.Contains(writeDiff, "-one") || !strings.Contains(writeDiff, "+alpha") {
		t.Fatalf("Write metadata should include unified diff, got %q", writeDiff)
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
	if editResp.Metadata["file_path"] != filePath || editResp.Metadata["occurrences"] != 1 {
		t.Fatalf("Edit metadata should include file path and occurrences, got %#v", editResp.Metadata)
	}
	editDiff, _ := editResp.Metadata["diff"].(string)
	if !strings.Contains(editDiff, "-alpha") || !strings.Contains(editDiff, "+gamma") {
		t.Fatalf("Edit metadata should include unified diff, got %q", editDiff)
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

func TestBashExploreModeAllowsInputAwareReadOnlyCommands(t *testing.T) {
	t.Parallel()

	engine := permission.NewEngine(permission.NewContext(permission.ModeExplore))
	decision, err := engine.CheckPermission(context.Background(), builtin.NewBash(), map[string]any{"command": "pwd"})
	if err != nil {
		t.Fatalf("CheckPermission returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("read-only bash command should be allowed in explore mode, got %#v", decision)
	}

	decision, err = engine.CheckPermission(context.Background(), builtin.NewBash(), map[string]any{"command": "touch created.txt"})
	if err != nil {
		t.Fatalf("CheckPermission returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorDeny {
		t.Fatalf("write bash command should be denied in explore mode, got %#v", decision)
	}
}

func TestBashInputAwareReadOnlyBranches(t *testing.T) {
	t.Parallel()

	bash := builtin.NewBash()
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "empty", command: "", want: false},
		{name: "parse error", command: "echo 'unterminated", want: false},
		{name: "command substitution", command: "echo $(pwd)", want: false},
		{name: "git status", command: "git status --short", want: true},
		{name: "docker ps", command: "docker ps", want: true},
		{name: "output redirection", command: "pwd > out.txt", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := bash.IsReadOnlyInput(map[string]any{"command": tt.command}); got != tt.want {
				t.Fatalf("IsReadOnlyInput(%q)=%v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestBashRunsCommandsInConfiguredWorkingDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	response := runTool(t, builtin.NewBash(builtin.WithBashCWD(dir)), map[string]any{
		"command": "pwd",
	}, astate.NewAgentState())
	text := response.GetTextContent("")
	if response.State != message.ToolResultSuccess || text == nil {
		t.Fatalf("Bash cwd execution failed: %#v", response)
	}
	if got := filepath.Clean(strings.TrimSpace(*text)); got != filepath.Clean(dir) {
		t.Fatalf("Bash should run in configured cwd: got %q want %q", got, filepath.Clean(dir))
	}
}

func TestGlobExecuteBranches(t *testing.T) {
	t.Parallel()

	glob := builtin.NewGlob()
	if resp := runTool(t, glob, map[string]any{"pattern": ""}, nil); resp.State != message.ToolResultError {
		t.Fatalf("empty glob pattern should fail, got %#v", resp)
	}
	if resp := runTool(t, glob, map[string]any{"pattern": "*.go", "path": filepath.Join(t.TempDir(), "missing")}, nil); resp.State != message.ToolResultError {
		t.Fatalf("missing glob directory should fail, got %#v", resp)
	}
	filePath := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if resp := runTool(t, glob, map[string]any{"pattern": "*.go", "path": filePath}, nil); resp.State != message.ToolResultError {
		t.Fatalf("file glob path should fail, got %#v", resp)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatalf("write go fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# readme\n"), 0o600); err != nil {
		t.Fatalf("write md fixture: %v", err)
	}
	if resp := runTool(t, glob, map[string]any{"pattern": "*.py", "path": dir}, nil); resp.State != message.ToolResultSuccess || !strings.Contains(*resp.GetTextContent(""), "No files found") {
		t.Fatalf("no-match glob response mismatch: %#v", resp)
	}
	resp := runTool(t, glob, map[string]any{"pattern": "*.go", "path": dir}, nil)
	if text := resp.GetTextContent(""); resp.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, "a.go") {
		t.Fatalf("glob match response mismatch: %#v", resp)
	}
}

func TestFileToolExecuteErrorBranches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filePath, []byte("alpha\nalpha\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if resp := runTool(t, builtin.NewRead(), map[string]any{"file_path": filepath.Join(dir, "missing.txt")}, nil); resp.State != message.ToolResultError {
		t.Fatalf("missing read should fail, got %#v", resp)
	}
	if resp := runTool(t, builtin.NewRead(), map[string]any{"file_path": dir}, nil); resp.State != message.ToolResultError {
		t.Fatalf("directory read should fail, got %#v", resp)
	}
	longFile := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(longFile, []byte(strings.Repeat("x", 2100)+"\n"), 0o600); err != nil {
		t.Fatalf("write long fixture: %v", err)
	}
	resp := runTool(t, builtin.NewRead(), map[string]any{"file_path": longFile, "offset": -5, "limit": 0}, astate.NewAgentState())
	if text := resp.GetTextContent(""); resp.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, "[truncated]") {
		t.Fatalf("long read should truncate and normalize bounds, got %#v", resp)
	}

	if resp := runTool(t, builtin.NewWrite(), map[string]any{"file_path": filePath, "content": "changed\n"}, nil); resp.State != message.ToolResultError {
		t.Fatalf("write existing file without state should fail, got %#v", resp)
	}
	newPath := filepath.Join(dir, "nested", "created.txt")
	if resp := runTool(t, builtin.NewWrite(), map[string]any{"file_path": newPath, "content": "created\n"}, astate.NewAgentState()); resp.State != message.ToolResultSuccess {
		t.Fatalf("write new file should succeed, got %#v", resp)
	}

	if resp := runTool(t, builtin.NewEdit(), map[string]any{"file_path": filepath.Join(dir, "missing-edit.txt"), "old_string": "a", "new_string": "b"}, astate.NewAgentState()); resp.State != message.ToolResultError {
		t.Fatalf("edit missing file should fail, got %#v", resp)
	}
	if resp := runTool(t, builtin.NewEdit(), map[string]any{"file_path": filePath, "old_string": "same", "new_string": "same"}, astate.NewAgentState()); resp.State != message.ToolResultError {
		t.Fatalf("edit identical strings should fail, got %#v", resp)
	}
	if resp := runTool(t, builtin.NewEdit(), map[string]any{"file_path": filePath, "old_string": "alpha", "new_string": "beta"}, nil); resp.State != message.ToolResultError {
		t.Fatalf("edit without state should fail, got %#v", resp)
	}
	state := astate.NewAgentState()
	_ = runTool(t, builtin.NewRead(), map[string]any{"file_path": filePath}, state)
	if resp := runTool(t, builtin.NewEdit(), map[string]any{"file_path": filePath, "old_string": "missing", "new_string": "beta"}, state); resp.State != message.ToolResultError {
		t.Fatalf("edit missing old string should fail, got %#v", resp)
	}
	if resp := runTool(t, builtin.NewEdit(), map[string]any{"file_path": filePath, "old_string": "alpha", "new_string": "beta"}, state); resp.State != message.ToolResultError {
		t.Fatalf("edit duplicate old string should fail without replace_all, got %#v", resp)
	}
	if resp := runTool(t, builtin.NewEdit(), map[string]any{"file_path": filePath, "old_string": "alpha", "new_string": "beta", "replace_all": true}, state); resp.State != message.ToolResultSuccess {
		t.Fatalf("edit replace_all should succeed, got %#v", resp)
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

	windowsGlobResp := runTool(t, builtin.NewGlob(), map[string]any{
		"pattern": `sub\*.txt`,
		"path":    dir,
	}, astate.NewAgentState())
	if windowsGlobResp.State != message.ToolResultSuccess {
		t.Fatalf("Glob should accept Windows-style separators, got %#v", windowsGlobResp)
	}
	windowsGlobOutput := windowsGlobResp.GetTextContent("")
	if windowsGlobOutput == nil || !strings.Contains(*windowsGlobOutput, filepath.Join("sub", "b.txt")) {
		t.Fatalf("Glob output should include sub/b.txt for Windows-style pattern, got %#v", windowsGlobOutput)
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
