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

package sandboxed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
)

func TestRemoteToolsMetadataAndLifecycle(t *testing.T) {
	t.Parallel()

	var nilWorkspace *Workspace
	if _, err := nilWorkspace.ListTools(context.Background()); err == nil {
		t.Fatal("nil workspace ListTools should fail")
	}
	w, backend, _, _, _ := readyWorkspace(t)
	if _, err := w.ListTools(canceledContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ListTools error = %v", err)
	}
	tools, err := w.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 6 {
		t.Fatalf("ListTools returned %d tools", len(tools))
	}
	wantFlags := map[string][2]bool{
		"Bash":  {false, false},
		"Edit":  {false, false},
		"Glob":  {true, true},
		"Grep":  {true, true},
		"Read":  {true, true},
		"Write": {false, false},
	}
	for _, current := range tools {
		flags, exists := wantFlags[current.Name()]
		if !exists {
			t.Fatalf("unexpected tool %q", current.Name())
		}
		if current.IsReadOnly() != flags[0] || current.IsConcurrencySafe() != flags[1] {
			t.Fatalf("tool %q flags = %t/%t", current.Name(), current.IsReadOnly(), current.IsConcurrencySafe())
		}
		if !strings.Contains(current.Description(), "remote workspace sandbox") || current.InputSchema() == nil {
			t.Fatalf("tool %q metadata is incomplete", current.Name())
		}
		if current.IsExternalTool() || current.IsStateInjected() || current.IsMCP() || current.MCPName() != "" {
			t.Fatalf("tool %q has unexpected capability metadata", current.Name())
		}
	}

	remote := tools[0].(*remoteTool)
	if _, err := remote.CheckPermissions(context.Background(), map[string]any{"command": "pwd"}, &permission.Context{}); err != nil {
		t.Fatalf("CheckPermissions returned error: %v", err)
	}
	_ = remote.MatchRule("pwd", map[string]any{"command": "pwd"})
	_ = remote.GenerateSuggestions(map[string]any{"command": "pwd"})
	stateValue, text := toolChunkResult(remote.Execute(context.Background(), map[string]any{"command": "pwd"}, nil))
	if stateValue != message.ToolResultSuccess || text != "ok\n" || len(backend.callsFor("/bin/bash")) != 1 {
		t.Fatalf("bound remote tool result = %s %q", stateValue, text)
	}

	stateValue, text = toolChunkResult((*remoteTool)(nil).Execute(context.Background(), nil, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "not initialized") {
		t.Fatalf("nil remote tool result = %s %q", stateValue, text)
	}
	stateValue, text = toolChunkResult((&remoteTool{}).Execute(context.Background(), nil, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "not initialized") {
		t.Fatalf("unbound remote tool result = %s %q", stateValue, text)
	}
	w.alive = false
	stateValue, text = toolChunkResult(remote.Execute(context.Background(), map[string]any{"command": "pwd"}, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "not initialized") {
		t.Fatalf("closed workspace tool result = %s %q", stateValue, text)
	}
}

func TestRemoteBashExecutableSpec(t *testing.T) {
	t.Parallel()

	w, backend, _, _, _ := readyWorkspace(t)
	stateValue, text := toolChunkResult(executeBash(context.Background(), w, nil, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "command is required") {
		t.Fatalf("missing command result = %s %q", stateValue, text)
	}

	backend.execHook = func(_ context.Context, argv []string, options ExecOptions) (ExecResult, error, bool) {
		if len(argv) > 0 && argv[0] == "/bin/bash" {
			if len(argv) != 3 || argv[1] != "-lc" || argv[2] != "echo ok" || options.CWD != "/work" || options.Timeout != maxBashTimeout {
				t.Fatalf("unexpected Bash execution: %#v %#v", argv, options)
			}
			return ExecResult{ExitCode: 0, Stdout: []byte("ok\n"), Stderr: []byte("warning\n")}, nil, true
		}
		return ExecResult{}, nil, false
	}
	stateValue, text = toolChunkResult(executeBash(context.Background(), w, map[string]any{
		"command": " echo ok ", "timeout_ms": float64(maxBashTimeout/time.Millisecond + 1),
	}, nil))
	if stateValue != message.ToolResultSuccess || text != "ok\nwarning\n" {
		t.Fatalf("Bash success result = %s %q", stateValue, text)
	}

	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{}, errors.New("backend offline"), true
	}
	stateValue, text = toolChunkResult(executeBash(context.Background(), w, map[string]any{"command": "pwd"}, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "backend offline") {
		t.Fatalf("Bash backend error result = %s %q", stateValue, text)
	}

	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{ExitCode: 9, Stdout: []byte("out"), Stderr: []byte("err")}, nil, true
	}
	stateValue, text = toolChunkResult(executeBash(context.Background(), w, map[string]any{"command": "false"}, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "exit code 9") || !strings.Contains(text, "outerr") {
		t.Fatalf("Bash failure result = %s %q", stateValue, text)
	}
}

func TestRemoteReadAndWriteExecutableSpec(t *testing.T) {
	t.Parallel()

	w, backend, _, _, _ := readyWorkspace(t)
	longLine := strings.Repeat("界", maxReadLineCharacters+1)
	backend.files["/work/read.txt"] = []byte("first\nsecond\n" + longLine + "\n")

	stateValue, text := toolChunkResult(executeRead(context.Background(), w, map[string]any{
		"file_path": "read.txt", "offset": int64(2), "limit": float64(2),
	}, nil))
	if stateValue != message.ToolResultSuccess || !strings.Contains(text, "     2\tsecond") || !strings.Contains(text, "[truncated]") {
		t.Fatalf("Read result = %s %q", stateValue, text)
	}
	stateValue, text = toolChunkResult(executeRead(context.Background(), w, map[string]any{
		"file_path": "read.txt", "offset": -2, "limit": -1,
	}, nil))
	if stateValue != message.ToolResultSuccess || !strings.Contains(text, "     1\tfirst") {
		t.Fatalf("Read fallback pagination = %s %q", stateValue, text)
	}

	for _, input := range []map[string]any{
		{},
		{"file_path": "../../etc/passwd"},
		{"file_path": "bad\x00path"},
	} {
		stateValue, _ = toolChunkResult(executeRead(context.Background(), w, input, nil))
		if stateValue != message.ToolResultError {
			t.Fatalf("Read input %#v should fail", input)
		}
	}
	backend.readHook = func(context.Context, string) ([]byte, error, bool) {
		return nil, errors.New("read denied"), true
	}
	stateValue, text = toolChunkResult(executeRead(context.Background(), w, map[string]any{"file_path": "read.txt"}, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "read denied") {
		t.Fatalf("Read backend error = %s %q", stateValue, text)
	}
	backend.readHook = nil

	stateValue, text = toolChunkResult(executeWrite(context.Background(), w, map[string]any{
		"file_path": "nested/out.txt", "content": "one\ntwo",
	}, nil))
	if stateValue != message.ToolResultSuccess || !strings.Contains(text, "2 lines") || string(backend.file("/work/nested/out.txt")) != "one\ntwo" {
		t.Fatalf("Write result = %s %q", stateValue, text)
	}
	backend.writeHook = func(context.Context, string, []byte) (error, bool) {
		return errors.New("write denied"), true
	}
	stateValue, text = toolChunkResult(executeWrite(context.Background(), w, map[string]any{"file_path": "out.txt"}, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "write denied") {
		t.Fatalf("Write backend error = %s %q", stateValue, text)
	}
	stateValue, _ = toolChunkResult(executeWrite(context.Background(), w, map[string]any{"file_path": "../escape"}, nil))
	if stateValue != message.ToolResultError {
		t.Fatal("Write path escape should fail")
	}
}

func TestRemoteEditExecutableSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		input     map[string]any
		readErr   error
		writeErr  error
		wantState message.ToolResultState
		wantText  string
		wantFile  string
	}{
		{name: "identical", content: "old", input: map[string]any{"file_path": "a.txt", "old_string": "old", "new_string": "old"}, wantState: message.ToolResultError, wantText: "identical", wantFile: "old"},
		{name: "read error", input: map[string]any{"file_path": "a.txt", "old_string": "old", "new_string": "new"}, readErr: errors.New("read failed"), wantState: message.ToolResultError, wantText: "read failed"},
		{name: "not found", content: "value", input: map[string]any{"file_path": "a.txt", "old_string": "old", "new_string": "new"}, wantState: message.ToolResultError, wantText: "not found", wantFile: "value"},
		{name: "ambiguous", content: "old old", input: map[string]any{"file_path": "a.txt", "old_string": "old", "new_string": "new"}, wantState: message.ToolResultError, wantText: "appears 2 times", wantFile: "old old"},
		{name: "single", content: "old old", input: map[string]any{"file_path": "a.txt", "old_string": "old", "new_string": "new", "replace_all": false}, wantState: message.ToolResultError, wantText: "appears 2 times", wantFile: "old old"},
		{name: "replace all", content: "old old", input: map[string]any{"file_path": "a.txt", "old_string": "old", "new_string": "new", "replace_all": true}, wantState: message.ToolResultSuccess, wantText: "all 2 occurrences", wantFile: "new new"},
		{name: "one occurrence", content: "old value", input: map[string]any{"file_path": "a.txt", "old_string": "old", "new_string": "new"}, wantState: message.ToolResultSuccess, wantText: "1 occurrence", wantFile: "new value"},
		{name: "write error", content: "old", input: map[string]any{"file_path": "a.txt", "old_string": "old", "new_string": "new"}, writeErr: errors.New("write failed"), wantState: message.ToolResultError, wantText: "write failed", wantFile: "old"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w, backend, _, _, _ := readyWorkspace(t)
			backend.files["/work/a.txt"] = []byte(test.content)
			if test.readErr != nil {
				backend.readHook = func(context.Context, string) ([]byte, error, bool) { return nil, test.readErr, true }
			}
			if test.writeErr != nil {
				backend.writeHook = func(context.Context, string, []byte) (error, bool) { return test.writeErr, true }
			}
			stateValue, text := toolChunkResult(executeEdit(context.Background(), w, test.input, nil))
			if stateValue != test.wantState || !strings.Contains(text, test.wantText) {
				t.Fatalf("Edit result = %s %q", stateValue, text)
			}
			if test.wantFile != "" && string(backend.file("/work/a.txt")) != test.wantFile {
				t.Fatalf("Edit file = %q, want %q", backend.file("/work/a.txt"), test.wantFile)
			}
		})
	}

	w, _, _, _, _ := readyWorkspace(t)
	stateValue, _ := toolChunkResult(executeEdit(context.Background(), w, map[string]any{"file_path": "../escape", "old_string": "a", "new_string": "b"}, nil))
	if stateValue != message.ToolResultError {
		t.Fatal("Edit path escape should fail")
	}
}

func TestRemoteGlobExecutableSpec(t *testing.T) {
	t.Parallel()

	w, backend, _, _, _ := readyWorkspace(t)
	backend.files["/work/z.go"] = []byte("package z")
	backend.files["/work/nested/a.go"] = []byte("package a")
	backend.files["/work/nested/a.txt"] = []byte("text")

	stateValue, text := toolChunkResult(executeGlob(context.Background(), w, map[string]any{"pattern": "**/*.go"}, nil))
	if stateValue != message.ToolResultSuccess || !strings.Contains(text, "/work/nested/a.go") || !strings.Contains(text, "/work/z.go") {
		t.Fatalf("Glob result = %s %q", stateValue, text)
	}
	stateValue, text = toolChunkResult(executeGlob(context.Background(), w, map[string]any{"pattern": "*.md"}, nil))
	if stateValue != message.ToolResultSuccess || !strings.Contains(text, "No files found") {
		t.Fatalf("Glob no-match result = %s %q", stateValue, text)
	}
	for _, input := range []map[string]any{{}, {"pattern": "*", "path": "../../etc"}} {
		stateValue, _ = toolChunkResult(executeGlob(context.Background(), w, input, nil))
		if stateValue != message.ToolResultError {
			t.Fatalf("Glob input %#v should fail", input)
		}
	}
	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{}, errors.New("find failed"), true
	}
	stateValue, text = toolChunkResult(executeGlob(context.Background(), w, map[string]any{"pattern": "*"}, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "find failed") {
		t.Fatalf("Glob exec error = %s %q", stateValue, text)
	}
	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{ExitCode: 2, Stderr: []byte("find denied")}, nil, true
	}
	stateValue, text = toolChunkResult(executeGlob(context.Background(), w, map[string]any{"pattern": "*"}, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "find denied") {
		t.Fatalf("Glob nonzero result = %s %q", stateValue, text)
	}
}

func TestRemoteGrepExecutableSpec(t *testing.T) {
	t.Parallel()

	w, backend, _, _, _ := readyWorkspace(t)
	backend.files["/work/a.go"] = []byte("Alpha\nbeta\nAlpha")
	backend.files["/work/b.txt"] = []byte("alpha\nnone")

	stateValue, text := toolChunkResult(executeGrep(context.Background(), w, map[string]any{
		"pattern": "alpha", "case_insensitive": true, "head_limit": "1",
	}, nil))
	if stateValue != message.ToolResultSuccess || strings.Count(text, "\n") != 0 || !strings.Contains(strings.ToLower(text), "alpha") {
		t.Fatalf("Grep content result = %s %q", stateValue, text)
	}
	stateValue, text = toolChunkResult(executeGrep(context.Background(), w, map[string]any{
		"pattern": "Alpha", "glob": "*.go", "output_mode": "files",
	}, nil))
	if stateValue != message.ToolResultSuccess || text != "/work/a.go" {
		t.Fatalf("Grep files result = %s %q", stateValue, text)
	}
	stateValue, text = toolChunkResult(executeGrep(context.Background(), w, map[string]any{
		"pattern": "Alpha", "output_mode": "count",
	}, nil))
	if stateValue != message.ToolResultSuccess || text != "/work/a.go:2" {
		t.Fatalf("Grep count result = %s %q", stateValue, text)
	}
	stateValue, text = toolChunkResult(executeGrep(context.Background(), w, map[string]any{"pattern": "missing"}, nil))
	if stateValue != message.ToolResultSuccess || !strings.Contains(text, "No matches") {
		t.Fatalf("Grep no-match result = %s %q", stateValue, text)
	}
	for _, input := range []map[string]any{
		{},
		{"pattern": "["},
		{"pattern": "ok", "path": "../../etc"},
	} {
		stateValue, _ = toolChunkResult(executeGrep(context.Background(), w, input, nil))
		if stateValue != message.ToolResultError {
			t.Fatalf("Grep input %#v should fail", input)
		}
	}

	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{}, errors.New("grep failed"), true
	}
	stateValue, text = toolChunkResult(executeGrep(context.Background(), w, map[string]any{"pattern": "ok"}, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "grep failed") {
		t.Fatalf("Grep exec error = %s %q", stateValue, text)
	}
	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{ExitCode: 2, Stderr: []byte("grep denied")}, nil, true
	}
	stateValue, text = toolChunkResult(executeGrep(context.Background(), w, map[string]any{"pattern": "ok"}, nil))
	if stateValue != message.ToolResultError || !strings.Contains(text, "grep denied") {
		t.Fatalf("Grep nonzero result = %s %q", stateValue, text)
	}
	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{ExitCode: 0, Stdout: []byte("/work/a.go:1:Alpha\n")}, nil, true
	}
	stateValue, text = toolChunkResult(executeGrep(context.Background(), w, map[string]any{"pattern": "Alpha", "glob": "*.md"}, nil))
	if stateValue != message.ToolResultSuccess || !strings.Contains(text, "No matches") {
		t.Fatalf("Grep filtered-empty result = %s %q", stateValue, text)
	}
}

func TestRemoteToolHelpers(t *testing.T) {
	t.Parallel()

	if got, err := searchBaseDir(nil, "/work"); err != nil || got != "/work" {
		t.Fatalf("searchBaseDir default = %q, %v", got, err)
	}
	paths := []struct {
		name  string
		value string
		want  string
		err   bool
	}{
		{name: "relative", value: "a/../b", want: "/work/b"},
		{name: "absolute", value: "/tmp/../work/x", want: "/work/x"},
		{name: "empty", err: true},
		{name: "nul", value: "a\x00b", err: true},
		{name: "escape", value: "../outside", err: true},
	}
	for _, test := range paths {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeSandboxPath(test.value, "/work")
			if test.err && err == nil || !test.err && (err != nil || got != test.want) {
				t.Fatalf("normalizeSandboxPath(%q) = %q, %v", test.value, got, err)
			}
		})
	}

	input := map[string]any{
		"text": "value", "bool": true, "int": 3, "int64": int64(4), "float": float64(5.9), "string": "6", "bad": struct{}{},
	}
	if stringValue(input, "text") != "value" || stringValue(input, "missing") != "" || !boolValue(input, "bool") || boolValue(input, "missing") {
		t.Fatal("stringValue/boolValue returned unexpected values")
	}
	for key, want := range map[string]int{"int": 3, "int64": 4, "float": 5, "string": 6, "bad": 9, "missing": 9} {
		if got := intValue(input, key, 9); got != want {
			t.Fatalf("intValue(%q) = %d, want %d", key, got, want)
		}
	}
	if timeoutValue(map[string]any{"t": -1}, "t", time.Second, 2*time.Second) != time.Second ||
		timeoutValue(map[string]any{"t": 1500}, "t", time.Second, 2*time.Second) != 1500*time.Millisecond ||
		timeoutValue(map[string]any{"t": 3000}, "t", time.Second, 2*time.Second) != 2*time.Second {
		t.Fatal("timeoutValue returned unexpected duration")
	}
	if len(splitLinesPreserve("")) != 0 || len(splitLinesPreserve("a\nb\n")) != 2 || len(splitLinesPreserve("a\nb")) != 2 {
		t.Fatal("splitLinesPreserve returned unexpected lines")
	}

	if !matchGlob("*.go", "a.go") || !matchGlob("**/*.go", "a.go") || !matchGlob("src/**/test*.go", "src/a/test_one.go") || matchGlob("*.go", "a.txt") {
		t.Fatal("matchGlob returned unexpected result")
	}
	if got := filterGlobMatches("/work", "\n/work/b.go\n/work/a.txt\n", "*.go"); len(got) != 1 || got[0] != "/work/b.go" {
		t.Fatalf("filterGlobMatches = %#v", got)
	}
	lines := "/work/a.go:1:x\n/work/a.go:2:y\n/work/b.txt:1:x\n"
	if got := filterGrepOutput(lines, "*.go", "files"); len(got) != 1 || got[0] != "/work/a.go" {
		t.Fatalf("filterGrepOutput files = %#v", got)
	}
	if got := filterGrepOutput(lines, "[", "content"); len(got) != 0 {
		t.Fatalf("filterGrepOutput invalid glob = %#v", got)
	}
	if grepOutputMode(nil) != "content" || grepOutputMode(map[string]any{"output_mode": "files"}) != "files" {
		t.Fatal("grepOutputMode returned unexpected mode")
	}
	values := []string{"a", "b"}
	if len(limitStrings(values, 1)) != 1 || len(limitStrings(values, 0)) != 2 || len(limitStrings(values, 3)) != 2 {
		t.Fatal("limitStrings returned unexpected slice")
	}

	stateValue, text := toolChunkResult(successText("ok"), nil)
	if stateValue != message.ToolResultSuccess || text != "ok" {
		t.Fatalf("successText = %s %q", stateValue, text)
	}
	stateValue, text = toolChunkResult(errorText("bad"), nil)
	if stateValue != message.ToolResultError || text != "bad" {
		t.Fatalf("errorText = %s %q", stateValue, text)
	}
}

var _ tool.Tool = (*remoteTool)(nil)
