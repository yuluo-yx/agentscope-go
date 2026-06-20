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
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

func TestBashSafetyAndPrefixMatching(t *testing.T) {
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
	if !matchBashPrefixRule("", "anything") {
		t.Fatalf("empty prefix should match every command")
	}
	if !matchBashPrefixRule("go test", "go test 'unterminated") {
		t.Fatalf("invalid shell syntax should fall back to permission pattern matching")
	}
	if matchBashPrefixRule("go test", "# no commands") {
		t.Fatalf("prefix rule should reject a file with no command calls")
	}
	if got := commandPrefix(""); got != "" {
		t.Fatalf("empty command prefix mismatch: %q", got)
	}
	if got := commandPrefix("FOO=bar"); got != "" {
		t.Fatalf("assignment-only command prefix mismatch: %q", got)
	}
	if got := fallbackCommandPrefix("FOO=bar ls -la"); got != "ls" {
		t.Fatalf("fallback command prefix with flags mismatch: %q", got)
	}
	if callMatchesPrefix([]string{"go"}, "go test") {
		t.Fatalf("short command should not match longer prefix")
	}
	if !isRootOrRootChild("/") || !isRootOrRootChild("/var") || isRootOrRootChild("relative/path") {
		t.Fatalf("root child detection mismatch")
	}
}

func TestBashPathReadOnlyAndLiteralBranches(t *testing.T) {
	t.Parallel()

	parse := func(command string) *syntax.File {
		t.Helper()
		file, err := parseBash(command)
		if err != nil {
			t.Fatalf("parseBash(%q): %v", command, err)
		}
		return file
	}

	if got := dangerousRedirection(parse("cat input.txt > /dev/null")); got != "> /dev/" {
		t.Fatalf("dangerousRedirection mismatch: %q", got)
	}
	if got := dangerousRedirection(parse("cat < input.txt")); got != "" {
		t.Fatalf("input redirection should not be dangerous: %q", got)
	}
	if got := dangerousBashPath(parse("printf token > .env")); got != ".env" {
		t.Fatalf("dangerousBashPath should inspect redirection target, got %q", got)
	}
	if got := dangerousBashPath(parse("touch -m .env")); got != ".env" {
		t.Fatalf("dangerousBashPath should skip flags and inspect file args, got %q", got)
	}
	if got := bashFilePaths(parse("cp source.txt target.txt > out.log")); len(got) != 3 {
		t.Fatalf("bashFilePaths should collect redirection and file args, got %#v", got)
	}
	if !isFileManipulatingCommand("/bin/touch") || isFileManipulatingCommand("cat") {
		t.Fatalf("file manipulating command classification mismatch")
	}
	if got := dangerousRemovePattern([]string{"-f", "safe.txt"}); got != "" {
		t.Fatalf("safe removal pattern should be empty, got %q", got)
	}
	if got := dangerousChmodPattern([]string{"777", "file.txt"}); got != "chmod 777" {
		t.Fatalf("chmod 777 pattern mismatch: %q", got)
	}
	if got := dangerousWords([]string{"dd", "if=/tmp/in", "of=/tmp/out"}); got != "dd" {
		t.Fatalf("dd should be dangerous, got %q", got)
	}
	if got := dangerousWords([]string{"format", "/dev/disk1"}); got != "format" {
		t.Fatalf("format should be dangerous, got %q", got)
	}

	if isReadOnlyCommand(parse("# comment only")) {
		t.Fatal("file with no calls should not be read-only")
	}
	if isReadOnlyCommand(parse("pwd > out.txt")) {
		t.Fatal("output redirection should make command write-capable")
	}
	if !isReadOnlyCommand(parse("git show HEAD && docker info")) {
		t.Fatal("known read-only git and docker commands should be allowed")
	}
	if isReadOnlyCommand(parse("git checkout main")) {
		t.Fatal("write-capable git command should not be read-only")
	}
	if got := baseCommand(parse("# comment only")); got != "" {
		t.Fatalf("base command for empty script mismatch: %q", got)
	}

	file := parse(`echo "$PLAIN" "${PLAIN:-fallback}"`)
	calls := commandCalls(file)
	if len(calls) != 1 {
		t.Fatalf("expected one command call, got %d", len(calls))
	}
	words := callLiteralWords(calls[0])
	if len(words) != 3 || words[1] != "$PLAIN" || words[2] != "" {
		t.Fatalf("literal word extraction mismatch: %#v", words)
	}
	if got, ok := wordPartLiteral(nil); ok || got != "" {
		t.Fatalf("nil word part should not be literal, got %q ok=%v", got, ok)
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

	globSuggestions := NewGlob().GenerateSuggestions(map[string]any{})
	if len(globSuggestions) != 1 || !strings.HasSuffix(globSuggestions[0].RuleContent, "/**") {
		t.Fatalf("Glob suggestions should default to current working directory, got %#v", globSuggestions)
	}
	assertToolTextContains(t, runBuiltinTool(t, NewGlob(), map[string]any{}, nil), message.ToolResultError, "pattern is required")
	assertToolTextContains(t, runBuiltinTool(t, NewGlob(), map[string]any{"pattern": "*.go", "path": filepath.Join(dir, "missing")}, nil), message.ToolResultError, "Directory not found")
	assertToolTextContains(t, runBuiltinTool(t, NewGlob(), map[string]any{"pattern": "*.go", "path": filePath}, nil), message.ToolResultError, "not a directory")
	assertToolTextContains(t, runBuiltinTool(t, NewGlob(), map[string]any{"pattern": "*.go", "path": dir}, nil), message.ToolResultSuccess, "No files found")
	assertToolTextContains(t, runBuiltinTool(t, NewGlob(), map[string]any{"pattern": "*.txt", "path": dir}, nil), message.ToolResultSuccess, "alpha.txt")

	grepSuggestions := NewGrep().GenerateSuggestions(map[string]any{})
	if len(grepSuggestions) != 1 || !strings.HasSuffix(grepSuggestions[0].RuleContent, "/**") {
		t.Fatalf("Grep suggestions should default to current working directory, got %#v", grepSuggestions)
	}
	assertToolTextContains(t, runBuiltinTool(t, NewGrep(), map[string]any{}, nil), message.ToolResultError, "pattern is required")
	assertToolTextContains(t, runBuiltinTool(t, NewGrep(), map[string]any{"pattern": "["}, nil), message.ToolResultError, "invalid regex")
	assertToolTextContains(t, runBuiltinTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": filepath.Join(dir, "missing")}, nil), message.ToolResultError, "path not found")
	assertToolTextContains(t, runBuiltinTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": filePath, "glob": "*.go"}, nil), message.ToolResultSuccess, "No matches")
	assertToolTextContains(t, runBuiltinTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": dir, "output_mode": "count", "case_insensitive": true}, nil), message.ToolResultSuccess, "alpha.txt:2")
	assertToolTextContains(t, runBuiltinTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": dir, "output_mode": "files"}, nil), message.ToolResultSuccess, "alpha.txt")
	assertToolTextContains(t, runBuiltinTool(t, NewGrep(), map[string]any{"pattern": "needle", "path": dir, "head_limit": 1}, nil), message.ToolResultSuccess, "needle")

	assertToolTextContains(t, runBuiltinTool(t, NewRead(), map[string]any{"file_path": ""}, nil), message.ToolResultError, "file_path is required")
	assertToolTextContains(t, runBuiltinTool(t, NewRead(), map[string]any{"file_path": dir}, astate.NewAgentState()), message.ToolResultError, "directory")
	assertToolTextContains(t, runBuiltinTool(t, NewRead(), map[string]any{"file_path": filepath.Join(dir, "missing.txt")}, astate.NewAgentState()), message.ToolResultError, "does not exist")
	assertToolTextContains(t, runBuiltinTool(t, NewWrite(), map[string]any{"file_path": "", "content": "x"}, nil), message.ToolResultError, "file_path is required")
	assertToolTextContains(t, runBuiltinTool(t, NewWrite(), map[string]any{"file_path": filePath, "content": "changed"}, nil), message.ToolResultError, "agent state required")
	assertToolTextContains(t, runBuiltinTool(t, NewWrite(), map[string]any{"file_path": filePath, "content": "changed"}, astate.NewAgentState()), message.ToolResultError, "has not been read yet")
	assertToolTextContains(t, runBuiltinTool(t, NewEdit(), map[string]any{"file_path": filePath, "old_string": "same", "new_string": "same"}, astate.NewAgentState()), message.ToolResultError, "identical")
	assertToolTextContains(t, runBuiltinTool(t, NewEdit(), map[string]any{"file_path": filepath.Join(dir, "missing.txt"), "old_string": "x", "new_string": "y"}, astate.NewAgentState()), message.ToolResultError, "File not found")
}

func TestCommonHelperBranches(t *testing.T) {
	t.Parallel()

	if _, ok := unsignedIntValue(-1); ok {
		t.Fatal("negative value should not parse as unsigned int")
	}
	if _, ok := unsignedIntValue(uint64(math.MaxUint64)); ok {
		t.Fatal("overflowing uint64 should not parse as int")
	}
	if got, ok := unsignedIntValue(uint(3)); !ok || got != 3 {
		t.Fatal("uint unsigned int mismatch")
	}
	if got, ok := floatIntValue(float64(4)); !ok || got != 4 {
		t.Fatal("float int mismatch")
	}
	if split := splitLinesPreserve("a\nb"); len(split) != 2 || split[0] != "a\n" || split[1] != "b" {
		t.Fatalf("splitLinesPreserve mismatch: %#v", split)
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "cached.txt")
	if err := os.WriteFile(filePath, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	lines, err := readFileLines(filePath)
	if err != nil || len(lines) != 2 {
		t.Fatalf("readFileLines mismatch: %#v err=%v", lines, err)
	}
	if _, err := readFileLines(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatal("readFileLines should fail for a missing file")
	}
	state := astate.NewAgentState()
	cacheFile(state, filePath, lines)
	cached, fromCache, err := cachedOrDiskLines(filePath, state)
	if err != nil || !fromCache || len(cached) != 2 {
		t.Fatalf("cachedOrDiskLines cache mismatch: %#v %v %v", cached, fromCache, err)
	}
	disk, fromCache, err := cachedOrDiskLines(filePath, nil)
	if err != nil || fromCache || len(disk) != 2 {
		t.Fatalf("cachedOrDiskLines disk mismatch: %#v %v %v", disk, fromCache, err)
	}

	ctx := permission.NewContext(permission.ModeAcceptEdits)
	ctx.WorkingDirectories[dir] = permission.AdditionalWorkingDirectory{Path: dir, Source: "test"}
	ctx.WorkingDirectories["empty"] = permission.AdditionalWorkingDirectory{Source: "test"}
	if !pathInAllowedWorkingDir(filepath.Join(dir, "cached.txt"), ctx) {
		t.Fatal("path should be allowed inside working directory")
	}
	if pathInAllowedWorkingDir(filepath.Join(t.TempDir(), "other.txt"), ctx) {
		t.Fatal("path outside working directory should not be allowed")
	}
	if pathInAllowedWorkingDir(filePath, nil) {
		t.Fatal("nil permission context should not allow writes")
	}
	resolved, err := resolvePermissionPath(filePath)
	wantResolved, _ := filepath.EvalSymlinks(filePath)
	if wantResolved == "" {
		wantResolved = filePath
	}
	if err != nil || resolved != filepath.Clean(wantResolved) {
		t.Fatalf("resolvePermissionPath mismatch: %q err=%v", resolved, err)
	}
	if _, err := resolvePermissionPath(""); err == nil {
		t.Fatal("empty permission path should fail")
	}
	missingPath := filepath.Join(dir, "missing", "nested.txt")
	resolvedMissing, err := resolvePermissionPath(missingPath)
	resolvedDir, dirErr := filepath.EvalSymlinks(dir)
	if dirErr != nil {
		t.Fatalf("resolve temp dir: %v", dirErr)
	}
	wantMissing := filepath.Join(resolvedDir, "missing", "nested.txt")
	if err != nil || resolvedMissing != filepath.Clean(wantMissing) {
		t.Fatalf("resolvePermissionPath should resolve through existing parent, got %q want %q err=%v", resolvedMissing, wantMissing, err)
	}
	if _, err := resolvePermissionPath("relative.txt"); err == nil {
		t.Fatal("relative permission path should fail")
	}

	globCases := []struct {
		pattern string
		value   string
		want    bool
	}{
		{pattern: "**/*.go", value: "a/b/main.go", want: true},
		{pattern: "src/**", value: "src/a/b/main.go", want: true},
		{pattern: "?.go", value: "x.go", want: true},
		{pattern: "*.go", value: "main.go", want: true},
		{pattern: "*.go", value: "main.txt", want: false},
		{pattern: "bad[", value: "bad[", want: true},
	}
	for _, tt := range globCases {
		if got := globMatch(tt.pattern, tt.value); got != tt.want {
			t.Fatalf("globMatch(%q,%q)=%v, want %v", tt.pattern, tt.value, got, tt.want)
		}
	}
}

func runBuiltinTool(t *testing.T, tool astool.Tool, input map[string]any, state *astate.AgentState) *astool.ToolResponse {
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
