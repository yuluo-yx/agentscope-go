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

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/tool/skill"
)

func TestBaseToolMetadataPermissionsAndRules(t *testing.T) {
	base := baseTool{
		name:            "Base",
		description:     "Base description",
		schema:          map[string]any{"type": "object"},
		concurrencySafe: true,
		readOnly:        true,
		stateInjected:   true,
	}
	if base.Name() != "Base" || base.Description() == "" || !base.IsConcurrencySafe() || !base.IsReadOnly() || !base.IsStateInjected() {
		t.Fatalf("base metadata mismatch: %#v", base)
	}
	if base.IsExternalTool() || base.IsMCP() || base.MCPName() != "" {
		t.Fatalf("base external metadata mismatch")
	}
	schema := base.InputSchema()
	schema["type"] = "changed"
	if base.InputSchema()["type"] != "object" {
		t.Fatalf("InputSchema should be cloned, got %#v", base.InputSchema())
	}
	decision, err := base.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow || !strings.Contains(decision.DecisionReason, "Read-only") {
		t.Fatalf("read-only decision mismatch: %#v", decision)
	}
	writeBase := base
	writeBase.readOnly = false
	decision, err = writeBase.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("write CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorPassthrough {
		t.Fatalf("write-capable base should pass through, got %#v", decision)
	}
	if !base.MatchRule("", nil) || base.MatchRule("non-empty", nil) {
		t.Fatal("base MatchRule should only match empty rule content")
	}
	suggestions := base.GenerateSuggestions(nil)
	if len(suggestions) != 1 || suggestions[0].ToolName != "Base" || suggestions[0].Source != sourceSuggested {
		t.Fatalf("base suggestions mismatch: %#v", suggestions)
	}
}

func TestValuePathAndFilePermissionHelpers(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "note.txt")
	ctx := permission.NewContext(permission.ModeAcceptEdits)
	ctx.WorkingDirectories[dir] = permission.AdditionalWorkingDirectory{Path: dir, Source: "test"}

	inputs := []struct {
		value any
		want  int
	}{
		{int8(1), 1},
		{int16(2), 2},
		{int32(3), 3},
		{int64(4), 4},
		{uint(5), 5},
		{uint8(6), 6},
		{uint16(7), 7},
		{uint32(8), 8},
		{uint64(9), 9},
		{float32(10.8), 10},
		{float64(11.8), 11},
		{"12", 12},
		{"bad", 99},
		{uint64(math.MaxInt) + 1, 99},
	}
	for _, tt := range inputs {
		if got := intValue(map[string]any{"value": tt.value}, "value", 99); got != tt.want {
			t.Fatalf("intValue(%T)=%d want %d", tt.value, got, tt.want)
		}
	}
	if intValue(nil, "missing", 13) != 13 {
		t.Fatal("intValue should return fallback for missing values")
	}
	if _, err := absolutePath(""); err == nil {
		t.Fatal("absolutePath should reject empty paths")
	}
	if _, err := absolutePath("relative.txt"); err == nil {
		t.Fatal("absolutePath should reject relative paths")
	}
	if got, err := absolutePath(filePath); err != nil || got != filePath {
		t.Fatalf("absolutePath mismatch: got %q err=%v", got, err)
	}
	if homePath, err := absolutePath("~/agentscope-test"); err != nil || !filepath.IsAbs(homePath) {
		t.Fatalf("absolutePath should expand home paths: %q err=%v", homePath, err)
	}

	state := &astate.AgentState{}
	if err := os.WriteFile(filePath, []byte("line\n"), 0o600); err != nil {
		t.Fatalf("write cache fixture: %v", err)
	}
	cacheFile(state, filePath, []string{"line\n"})
	if state.ToolContext == nil {
		t.Fatal("cacheFile should initialize ToolContext")
	}
	if _, ok := state.ToolContext.GetCache(filePath); !ok {
		t.Fatal("cacheFile should store file lines")
	}
	cacheFile(nil, filePath, nil)

	if !fileMatchRule(filepath.ToSlash(dir)+"/**", map[string]any{"file_path": filePath}) {
		t.Fatal("fileMatchRule should match file path patterns")
	}
	if fileMatchRule("**", map[string]any{}) {
		t.Fatal("fileMatchRule should reject missing file_path")
	}
	if got := fileSuggestions("Read", map[string]any{"file_path": filePath}); got[0].RuleContent != filepath.ToSlash(dir)+"/**" {
		t.Fatalf("fileSuggestions path mismatch: %#v", got)
	}
	if got := fileSuggestions("Read", nil); got[0].RuleContent != "**" {
		t.Fatalf("fileSuggestions default mismatch: %#v", got)
	}
	if !pathInAllowedWorkingDir(filePath, ctx) || pathInAllowedWorkingDir(filePath, nil) {
		t.Fatal("pathInAllowedWorkingDir mismatch")
	}
	if pathInAllowedWorkingDir(filePath, permission.NewContext(permission.ModeDefault)) {
		t.Fatal("empty working directory set should not allow file path")
	}

	decision, err := writableFilePermission("Write", "write", map[string]any{}, ctx)
	if err != nil || decision.Behavior != permission.BehaviorAsk || !strings.Contains(decision.DecisionReason, "Missing") {
		t.Fatalf("missing path decision mismatch: %#v err=%v", decision, err)
	}
	decision, err = writableFilePermission("Write", "write", map[string]any{"file_path": filepath.Join(dir, ".env")}, ctx)
	if err != nil || decision.Behavior != permission.BehaviorAsk || !strings.Contains(decision.DecisionReason, "dangerous") {
		t.Fatalf("dangerous path decision mismatch: %#v err=%v", decision, err)
	}
	decision, err = writableFilePermission("Write", "write", map[string]any{"file_path": filePath}, ctx)
	if err != nil || decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("accept-edits decision mismatch: %#v err=%v", decision, err)
	}
	decision, err = writableFilePermission("Write", "write", map[string]any{"file_path": filePath}, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAsk {
		t.Fatalf("default write decision mismatch: %#v err=%v", decision, err)
	}
	outside := t.TempDir()
	link := filepath.Join(dir, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}
	linkTarget := filepath.Join(link, "escape.txt")
	decision, err = writableFilePermission("Write", "write", map[string]any{"file_path": linkTarget}, ctx)
	if err != nil || decision.Behavior != permission.BehaviorAsk {
		t.Fatalf("symlink escape decision mismatch: %#v err=%v", decision, err)
	}

	if !globMatch("**/*.go", "cmd/main.go") || !globMatch("*.go", "main.go") || globMatch("*.go", "README.md") {
		t.Fatal("globMatch should handle recursive and basename patterns")
	}
	if got := globToRegexp("**/*.go?"); !strings.Contains(got, "(?:.*/)?") || !strings.Contains(got, "[^/]") {
		t.Fatalf("globToRegexp mismatch: %q", got)
	}
}

func TestBashSedParsingAndPermissionBranches(t *testing.T) {
	if checkSedExpression("1,2p", true) != "" {
		t.Fatal("sed print expressions should be allowed with -n")
	}
	if checkSedExpression("s/foo/bar/g", false) != "" || checkSedExpression("s#foo#bar#2", false) != "" {
		t.Fatal("sed substitution expressions should be allowed")
	}
	if !isSedPrintExpression("12p") || isSedPrintExpression("p") || isSedPrintExpression("1,2,3p") || isDecimalDigits("12x") {
		t.Fatal("sed print digit validation mismatch")
	}
	if isSedSubstitutionExpression("x/foo/bar/") || isSedSubstitutionExpression("s/foo/bar/z") {
		t.Fatal("sed substitution validation mismatch")
	}
	if reason := checkSedArgs([]string{"-z", "s/a/b/"}); !strings.Contains(reason, "flag") {
		t.Fatalf("disallowed sed flag mismatch: %q", reason)
	}
	if flags, expressions, files := splitSedArgs([]string{"--in-place", "-ne", "1p", "file.txt"}); len(flags) != 3 || len(expressions) != 1 || len(files) != 1 {
		t.Fatalf("splitSedArgs mismatch: flags=%v expressions=%v files=%v", flags, expressions, files)
	}

	file, err := parseBash(`echo "$HOME" ${HOME:-fallback}`)
	if err != nil {
		t.Fatalf("parseBash returned error: %v", err)
	}
	calls := commandCalls(file)
	words := callLiteralWords(calls[0])
	if len(words) != 3 || words[1] != "$HOME" || words[2] != "" {
		t.Fatalf("literal words should preserve simple params and reject complex params: %#v", words)
	}
	partText, partOK := wordPartLiteral(nil)
	if wordLiteral(nil) != "" || partText != "" || partOK {
		t.Fatal("nil word literal should be empty")
	}
	if baseCommand(file) != "echo" || isFilesystemCommand("echo") || !isFilesystemCommand("mkdir") {
		t.Fatal("base/filesystem command classification mismatch")
	}
	if fallbackCommandPrefix("GOFLAGS=-count=1 go test ./...") != "go test" || commandPrefix("unterminated '") != "unterminated '" {
		t.Fatal("command prefix fallback mismatch")
	}
	if !matchBashPrefixRule("", "unterminated '") || !matchBashPrefixRule("go test", "go test ./...\ngo test ./tool") || matchBashPrefixRule("go test", "go vet ./...") {
		t.Fatal("bash prefix rule mismatch")
	}
	if !hasShortFlag([]string{"-rf"}, "r") || hasShortFlag([]string{"--recursive"}, "r") {
		t.Fatal("short flag detection mismatch")
	}

	bash := NewBash()
	decision, err := bash.CheckPermissions(context.Background(), map[string]any{"command": ""}, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorDeny {
		t.Fatalf("empty bash command decision mismatch: %#v err=%v", decision, err)
	}
	decision, err = bash.CheckPermissions(context.Background(), map[string]any{"command": "echo 'unterminated"}, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAsk || !strings.Contains(decision.DecisionReason, "invalid shell syntax") {
		t.Fatalf("invalid syntax decision mismatch: %#v err=%v", decision, err)
	}
	decision, err = bash.CheckPermissions(context.Background(), map[string]any{"command": "echo $(pwd)"}, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAsk || !strings.Contains(decision.DecisionReason, "command substitution") {
		t.Fatalf("dynamic shell decision mismatch: %#v err=%v", decision, err)
	}
	accept := permission.NewContext(permission.ModeAcceptEdits)
	decision, err = bash.CheckPermissions(context.Background(), map[string]any{"command": "mkdir ./tmp-dir"}, accept)
	if err != nil || decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("accept-edits filesystem decision mismatch: %#v err=%v", decision, err)
	}

	failedChunks, failedErr := bash.Execute(context.Background(), map[string]any{"command": "exit 7"}, nil)
	failed := collectBuiltinChunks(t, mustExecute(t, failedChunks, failedErr))
	if len(failed) != 1 || failed[0].State != message.ToolResultError || !strings.Contains(*failed[0].Content.GetTextContent(""), "Command failed") {
		t.Fatalf("failed bash execution mismatch: %#v", failed)
	}
	timeoutChunks, timeoutErr := bash.Execute(context.Background(), map[string]any{"command": "sleep 0.1", "timeout_ms": 1}, nil)
	timedOut := collectBuiltinChunks(t, mustExecute(t, timeoutChunks, timeoutErr))
	if len(timedOut) != 1 || timedOut[0].State != message.ToolResultError || !strings.Contains(*timedOut[0].Content.GetTextContent(""), "timed out") {
		t.Fatalf("timeout bash execution mismatch: %#v", timedOut)
	}
}

func TestConcreteToolRuleMethodsAndSkillViewer(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	fileTools := []astool.Tool{NewRead(), NewWrite(), NewEdit()}
	for _, current := range fileTools {
		if !current.MatchRule(filepath.ToSlash(dir)+"/**", map[string]any{"file_path": filePath}) {
			t.Fatalf("%s should match file rule", current.Name())
		}
		suggestions := current.GenerateSuggestions(map[string]any{"file_path": filePath})
		if len(suggestions) != 1 || suggestions[0].ToolName != current.Name() || suggestions[0].RuleContent != filepath.ToSlash(dir)+"/**" {
			t.Fatalf("%s suggestions mismatch: %#v", current.Name(), suggestions)
		}
	}

	glob := NewGlob()
	if !glob.MatchRule(filepath.ToSlash(dir), map[string]any{"path": dir}) || !glob.MatchRule("*.go", map[string]any{"pattern": "*.go"}) {
		t.Fatal("Glob MatchRule mismatch")
	}
	if got := glob.GenerateSuggestions(map[string]any{"path": dir}); got[0].RuleContent != filepath.ToSlash(dir)+"/**" {
		t.Fatalf("Glob suggestions mismatch: %#v", got)
	}
	grep := NewGrep()
	if !grep.MatchRule(filepath.ToSlash(dir), map[string]any{"path": dir}) || grep.MatchRule("**", map[string]any{}) {
		t.Fatal("Grep MatchRule mismatch")
	}
	if got := grep.GenerateSuggestions(map[string]any{"path": dir}); got[0].RuleContent != filepath.ToSlash(dir)+"/**" {
		t.Fatalf("Grep suggestions mismatch: %#v", got)
	}

	viewer := NewSkillViewer([]skill.Skill{{Name: "planner", Markdown: "# Planner\nUse a plan."}})
	if viewer.Name() != "Skill" || !viewer.IsReadOnly() || !viewer.IsConcurrencySafe() || viewer.InputSchema()["type"] != "object" {
		t.Fatalf("SkillViewer metadata mismatch: %#v", viewer)
	}
	okChunks, okErr := viewer.Execute(context.Background(), map[string]any{"skill": "planner"}, nil)
	ok := collectBuiltinChunks(t, mustExecute(t, okChunks, okErr))
	if len(ok) != 1 || ok[0].State != message.ToolResultSuccess || !strings.Contains(*ok[0].Content.GetTextContent(""), "Use a plan") {
		t.Fatalf("SkillViewer success mismatch: %#v", ok)
	}
	missingChunks, missingErr := viewer.Execute(context.Background(), map[string]any{}, nil)
	missing := collectBuiltinChunks(t, mustExecute(t, missingChunks, missingErr))
	if len(missing) != 1 || missing[0].State != message.ToolResultError || !strings.Contains(*missing[0].Content.GetTextContent(""), "required") {
		t.Fatalf("SkillViewer missing name mismatch: %#v", missing)
	}
	notFoundChunks, notFoundErr := viewer.Execute(context.Background(), map[string]any{"skill": "missing"}, nil)
	notFound := collectBuiltinChunks(t, mustExecute(t, notFoundChunks, notFoundErr))
	if len(notFound) != 1 || notFound[0].State != message.ToolResultError || !strings.Contains(*notFound[0].Content.GetTextContent(""), "not found") {
		t.Fatalf("SkillViewer not found mismatch: %#v", notFound)
	}
}

func mustExecute(t *testing.T, chunks <-chan astool.ToolChunk, err error) <-chan astool.ToolChunk {
	t.Helper()
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return chunks
}

func collectBuiltinChunks(t *testing.T, chunks <-chan astool.ToolChunk) []astool.ToolChunk {
	t.Helper()
	collected := []astool.ToolChunk{}
	for chunk := range chunks {
		collected = append(collected, chunk)
	}
	return collected
}
