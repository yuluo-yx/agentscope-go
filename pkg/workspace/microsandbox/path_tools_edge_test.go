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

package microsandbox

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

func TestPathHelpersHandleEdgeCases(t *testing.T) {
	t.Parallel()

	if got := cleanSandboxPath(""); got != "" {
		t.Fatalf("cleanSandboxPath empty = %q, want empty", got)
	}
	if got := cleanSandboxPath("."); got != "" {
		t.Fatalf("cleanSandboxPath dot = %q, want empty", got)
	}
	if got := cleanSandboxPath("relative/../file.txt"); got != "/file.txt" {
		t.Fatalf("cleanSandboxPath relative = %q, want /file.txt", got)
	}

	relative, err := normalizeSandboxPath(" report.txt ", "/workspace/project")
	if err != nil || relative != "/workspace/project/report.txt" {
		t.Fatalf("normalizeSandboxPath relative = %q, %v", relative, err)
	}
	absolute, err := normalizeSandboxPath("/tmp/../data.txt", "/workspace/project")
	if err != nil || absolute != "/data.txt" {
		t.Fatalf("normalizeSandboxPath absolute = %q, %v", absolute, err)
	}
	defaultRoot, err := normalizeSandboxPath("report.txt", "")
	if err != nil || defaultRoot != "/workspace/report.txt" {
		t.Fatalf("normalizeSandboxPath default root = %q, %v", defaultRoot, err)
	}
	if _, err := normalizeSandboxPath(" ", "/workspace/project"); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("normalizeSandboxPath empty error = %v", err)
	}
	if _, err := normalizeSandboxPath("../escape.txt", "/workspace/project"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("normalizeSandboxPath escape error = %v", err)
	}

	input := map[string]any{
		"int":      3,
		"int64":    int64(4),
		"float64":  5.9,
		"string":   "6",
		"invalid":  "x",
		"negative": -1,
		"huge":     60_000,
		"json":     json.Number("250"),
	}
	for key, want := range map[string]int{"int": 3, "int64": 4, "float64": 5, "string": 6} {
		if got := intValue(input, key, 99); got != want {
			t.Fatalf("intValue(%s) = %d, want %d", key, got, want)
		}
	}
	if got := intValue(input, "missing", 99); got != 99 {
		t.Fatalf("intValue missing = %d, want fallback", got)
	}
	if got := intValue(input, "invalid", 99); got != 99 {
		t.Fatalf("intValue invalid = %d, want fallback", got)
	}
	if got := timeoutValue(input, "negative", 150*time.Millisecond, time.Second); got != 150*time.Millisecond {
		t.Fatalf("timeoutValue negative = %s, want fallback", got)
	}
	if got := timeoutValue(input, "huge", 150*time.Millisecond, time.Second); got != time.Second {
		t.Fatalf("timeoutValue huge = %s, want max", got)
	}
	if got := timeoutValue(input, "json", 150*time.Millisecond, time.Second); got != 250*time.Millisecond {
		t.Fatalf("timeoutValue json = %s, want 250ms", got)
	}

	if got := splitLinesPreserve(""); len(got) != 0 {
		t.Fatalf("splitLinesPreserve empty = %#v", got)
	}
	if got := splitLinesPreserve("a\nb"); len(got) != 2 || got[0] != "a\n" || got[1] != "b" {
		t.Fatalf("splitLinesPreserve without trailing newline = %#v", got)
	}
	if got := splitLinesPreserve("a\n"); len(got) != 1 || got[0] != "a\n" {
		t.Fatalf("splitLinesPreserve trailing newline = %#v", got)
	}
}

func TestWorkspaceToolsHandleAdditionalBranches(t *testing.T) {
	t.Parallel()

	handle := newFakeHandle("sandbox-edge-tools")
	longLine := strings.Repeat("x", maxReadLineCharacters+1)
	handle.files["/workspace/notes.txt"] = []byte(longLine + "\nsecond\n")
	handle.files["/workspace/repeated.txt"] = []byte("foo\nfoo\n")
	ws := initializedWorkspace(t, &fakeRuntime{createHandle: handle})

	bash := findTool(t, ws, "Bash")
	emptyCommand := runTool(t, bash, map[string]any{"command": " \t"}, nil)
	if emptyCommand.State != message.ToolResultError || !strings.Contains(textOutput(emptyCommand), "command is required") {
		t.Fatalf("empty bash command = %s %q", emptyCommand.State, textOutput(emptyCommand))
	}
	handle.runResult = runResult{Stdout: "partial\n", Stderr: "failed\n", ExitCode: 2}
	failedCommand := runTool(t, bash, map[string]any{"command": "false"}, nil)
	if failedCommand.State != message.ToolResultError || !strings.Contains(textOutput(failedCommand), "exit code 2") {
		t.Fatalf("failed bash command = %s %q", failedCommand.State, textOutput(failedCommand))
	}

	read := findTool(t, ws, "Read")
	truncated := runTool(t, read, map[string]any{
		"file_path": "notes.txt",
		"offset":    0,
		"limit":     -1,
	}, nil)
	if truncated.State != message.ToolResultSuccess || !strings.Contains(textOutput(truncated), "[truncated]") {
		t.Fatalf("truncated read = %s %q", truncated.State, textOutput(truncated))
	}
	pastEnd := runTool(t, read, map[string]any{
		"file_path": "notes.txt",
		"offset":    100,
		"limit":     1,
	}, nil)
	if pastEnd.State != message.ToolResultSuccess || textOutput(pastEnd) != "" {
		t.Fatalf("past-end read = %s %q", pastEnd.State, textOutput(pastEnd))
	}

	edit := findTool(t, ws, "Edit")
	missingOldString := runTool(t, edit, map[string]any{
		"file_path":  "notes.txt",
		"old_string": "missing",
		"new_string": "value",
	}, nil)
	if missingOldString.State != message.ToolResultError || !strings.Contains(textOutput(missingOldString), "not found") {
		t.Fatalf("missing edit target = %s %q", missingOldString.State, textOutput(missingOldString))
	}
	ambiguousEdit := runTool(t, edit, map[string]any{
		"file_path":  "repeated.txt",
		"old_string": "foo",
		"new_string": "bar",
	}, nil)
	if ambiguousEdit.State != message.ToolResultError || !strings.Contains(textOutput(ambiguousEdit), "appears 2 times") {
		t.Fatalf("ambiguous edit = %s %q", ambiguousEdit.State, textOutput(ambiguousEdit))
	}
	replaceAll := runTool(t, edit, map[string]any{
		"file_path":   "repeated.txt",
		"old_string":  "foo",
		"new_string":  "bar",
		"replace_all": true,
	}, nil)
	if replaceAll.State != message.ToolResultSuccess || string(handle.files["/workspace/repeated.txt"]) != "bar\nbar\n" {
		t.Fatalf("replace-all edit = %s %q file=%q", replaceAll.State, textOutput(replaceAll), handle.files["/workspace/repeated.txt"])
	}

	glob := findTool(t, ws, "Glob")
	handle.runResult = runResult{Stdout: "/workspace/a.txt\n", ExitCode: 0}
	noGlobMatches := runTool(t, glob, map[string]any{"pattern": "*.go"}, nil)
	if noGlobMatches.State != message.ToolResultSuccess || !strings.Contains(textOutput(noGlobMatches), "No files found") {
		t.Fatalf("glob no matches = %s %q", noGlobMatches.State, textOutput(noGlobMatches))
	}
	handle.runResult = runResult{Stderr: "find failed\n", ExitCode: 1}
	globFailure := runTool(t, glob, map[string]any{"pattern": "*"}, nil)
	if globFailure.State != message.ToolResultError || !strings.Contains(textOutput(globFailure), "find failed") {
		t.Fatalf("glob failure = %s %q", globFailure.State, textOutput(globFailure))
	}

	grep := findTool(t, ws, "Grep")
	missingPattern := runTool(t, grep, map[string]any{"pattern": " "}, nil)
	if missingPattern.State != message.ToolResultError || !strings.Contains(textOutput(missingPattern), "pattern is required") {
		t.Fatalf("grep missing pattern = %s %q", missingPattern.State, textOutput(missingPattern))
	}
	handle.runResult = runResult{ExitCode: 1}
	noGrepMatches := runTool(t, grep, map[string]any{"pattern": "needle"}, nil)
	if noGrepMatches.State != message.ToolResultSuccess || !strings.Contains(textOutput(noGrepMatches), "No matches found") {
		t.Fatalf("grep no matches = %s %q", noGrepMatches.State, textOutput(noGrepMatches))
	}
	handle.runResult = runResult{Stderr: "grep failed\n", ExitCode: 2}
	grepFailure := runTool(t, grep, map[string]any{"pattern": "needle", "case_insensitive": true}, nil)
	if grepFailure.State != message.ToolResultError || !strings.Contains(textOutput(grepFailure), "grep failed") {
		t.Fatalf("grep failure = %s %q", grepFailure.State, textOutput(grepFailure))
	}

	if got, err := globBaseDir(map[string]any{}, "/workspace"); err != nil || got != "/workspace" {
		t.Fatalf("globBaseDir default = %q, %v", got, err)
	}
	if got, err := grepSearchPath(map[string]any{}, "/workspace"); err != nil || got != "/workspace" {
		t.Fatalf("grepSearchPath default = %q, %v", got, err)
	}

	write := findTool(t, ws, "Write")
	writePathError := runTool(t, write, map[string]any{"file_path": " ", "content": "x"}, nil)
	if writePathError.State != message.ToolResultError || !strings.Contains(textOutput(writePathError), "required") {
		t.Fatalf("write path error = %s %q", writePathError.State, textOutput(writePathError))
	}

	readErrHandle := newFakeHandle("sandbox-read-error")
	readErrHandle.readErr = errors.New("read failed")
	readErrWS := initializedWorkspace(t, &fakeRuntime{createHandle: readErrHandle})
	editReadError := runTool(t, findTool(t, readErrWS, "Edit"), map[string]any{
		"file_path":  "notes.txt",
		"old_string": "a",
		"new_string": "b",
	}, nil)
	if editReadError.State != message.ToolResultError || !strings.Contains(textOutput(editReadError), "read failed") {
		t.Fatalf("edit read error = %s %q", editReadError.State, textOutput(editReadError))
	}

	writeErrHandle := newFakeHandle("sandbox-write-error")
	writeErrHandle.files["/workspace/notes.txt"] = []byte("old")
	writeErrHandle.writeErr = errors.New("write failed")
	writeErrWS := initializedWorkspace(t, &fakeRuntime{createHandle: writeErrHandle})
	editWriteError := runTool(t, findTool(t, writeErrWS, "Edit"), map[string]any{
		"file_path":  "notes.txt",
		"old_string": "old",
		"new_string": "new",
	}, nil)
	if editWriteError.State != message.ToolResultError || !strings.Contains(textOutput(editWriteError), "write failed") {
		t.Fatalf("edit write error = %s %q", editWriteError.State, textOutput(editWriteError))
	}

	runErrHandle := newFakeHandle("sandbox-run-error")
	runErrHandle.runErr = errors.New("run failed")
	runErrWS := initializedWorkspace(t, &fakeRuntime{createHandle: runErrHandle})
	globRunError := runTool(t, findTool(t, runErrWS, "Glob"), map[string]any{"pattern": "*"}, nil)
	if globRunError.State != message.ToolResultError || !strings.Contains(textOutput(globRunError), "run failed") {
		t.Fatalf("glob run error = %s %q", globRunError.State, textOutput(globRunError))
	}
	grepRunError := runTool(t, findTool(t, runErrWS, "Grep"), map[string]any{"pattern": "needle"}, nil)
	if grepRunError.State != message.ToolResultError || !strings.Contains(textOutput(grepRunError), "run failed") {
		t.Fatalf("grep run error = %s %q", grepRunError.State, textOutput(grepRunError))
	}

	handle.runResult = runResult{Stdout: "/workspace/app.py:1:needle\n", ExitCode: 0}
	grepFilteredOut := runTool(t, grep, map[string]any{"pattern": "needle", "glob": "*.go"}, nil)
	if grepFilteredOut.State != message.ToolResultSuccess || !strings.Contains(textOutput(grepFilteredOut), "No matches found") {
		t.Fatalf("grep filtered out = %s %q", grepFilteredOut.State, textOutput(grepFilteredOut))
	}
}
