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

package docker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func TestWorkspaceInitializesRuntimeAndListsSandboxTools(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{}
	ws, err := NewWorkspace(
		WithWorkspaceID("workspace-1"),
		WithImage("ubuntu:test"),
		WithContainerWorkdir("/agent"),
		withRuntime(runtime),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}

	if initErr := ws.Initialize(context.Background()); initErr != nil {
		t.Fatalf("Initialize returned error: %v", initErr)
	}
	if !ws.IsAlive() || ws.WorkspaceID() != "workspace-1" {
		t.Fatalf("workspace lifecycle mismatch: alive=%v id=%q", ws.IsAlive(), ws.WorkspaceID())
	}
	if runtime.createSpec.Image != "ubuntu:test" || runtime.createSpec.Workdir != "/agent" {
		t.Fatalf("runtime create spec mismatch: %#v", runtime.createSpec)
	}
	if !runtime.started {
		t.Fatalf("runtime container was not started")
	}

	instructions, err := ws.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if !strings.Contains(instructions, "Docker-based workspace") || !strings.Contains(instructions, "/agent") {
		t.Fatalf("instructions should describe Docker workspace and workdir: %s", instructions)
	}

	tools, err := ws.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if got := toolNames(tools); strings.Join(got, ",") != "Bash,Edit,Glob,Grep,Read,Write" {
		t.Fatalf("unexpected Docker workspace tools: %#v", got)
	}
}

func TestBashToolRunsInsideRuntime(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{runResult: runResult{Stdout: "inside\n", ExitCode: 0}}
	ws := initializedWorkspace(t, runtime)
	bash := findTool(t, ws, "Bash")
	response := runTool(t, bash, map[string]any{"command": "pwd", "timeout_ms": 1500}, nil)

	if response.State != message.ToolResultSuccess || textOutput(response) != "inside\n" {
		t.Fatalf("unexpected bash response: state=%s content=%q", response.State, textOutput(response))
	}
	if len(runtime.runs) != 1 {
		t.Fatalf("expected one runtime run, got %d", len(runtime.runs))
	}
	req := runtime.runs[0]
	if req.Command != "pwd" || req.Workdir != "/workspace" || req.Timeout != 1500*time.Millisecond {
		t.Fatalf("run request mismatch: %#v", req)
	}
}

func TestFileToolsReadWriteAndEditInsideRuntime(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{files: map[string]string{}}
	ws := initializedWorkspace(t, runtime)
	state := asstate.NewAgentState()

	write := findTool(t, ws, "Write")
	writeResponse := runTool(t, write, map[string]any{
		"file_path": "/workspace/notes/todo.txt",
		"content":   "hello docker\n",
	}, state)
	if writeResponse.State != message.ToolResultSuccess {
		t.Fatalf("write failed: %s", textOutput(writeResponse))
	}
	if runtime.files["/workspace/notes/todo.txt"] != "hello docker\n" {
		t.Fatalf("file not written in runtime: %#v", runtime.files)
	}

	read := findTool(t, ws, "Read")
	readResponse := runTool(t, read, map[string]any{"file_path": "/workspace/notes/todo.txt"}, state)
	if !strings.Contains(textOutput(readResponse), "hello docker") {
		t.Fatalf("read should return container file content: %q", textOutput(readResponse))
	}

	edit := findTool(t, ws, "Edit")
	editResponse := runTool(t, edit, map[string]any{
		"file_path":  "/workspace/notes/todo.txt",
		"old_string": "docker",
		"new_string": "sandbox",
	}, state)
	if editResponse.State != message.ToolResultSuccess {
		t.Fatalf("edit failed: %s", textOutput(editResponse))
	}
	if runtime.files["/workspace/notes/todo.txt"] != "hello sandbox\n" {
		t.Fatalf("file not edited in runtime: %#v", runtime.files)
	}
}

func TestWorkspaceCloseStopsAndRemovesEphemeralContainer(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{}
	ws := initializedWorkspace(t, runtime)
	if err := ws.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if ws.IsAlive() {
		t.Fatalf("workspace should not be alive after Close")
	}
	if !runtime.stopped || !runtime.removed || !runtime.closed {
		t.Fatalf("runtime cleanup mismatch: stopped=%v removed=%v closed=%v", runtime.stopped, runtime.removed, runtime.closed)
	}
}

func initializedWorkspace(t *testing.T, runtime *fakeRuntime) *Workspace {
	t.Helper()

	ws, err := NewWorkspace(withRuntime(runtime))
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	return ws
}

func findTool(t *testing.T, ws *Workspace, name string) tool.Tool {
	t.Helper()

	tools, err := ws.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	for _, current := range tools {
		if current.Name() == name {
			return current
		}
	}
	t.Fatalf("missing tool %s", name)
	return nil
}

func runTool(t *testing.T, current tool.Tool, input map[string]any, state *asstate.AgentState) *tool.ToolResponse {
	t.Helper()

	chunks, err := current.Execute(context.Background(), input, state)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			t.Fatalf("AppendChunk returned error: %v", err)
		}
	}
	return response
}

func toolNames(tools []tool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return names
}

func textOutput(response *tool.ToolResponse) string {
	var builder strings.Builder
	for _, block := range response.Content {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

type fakeRuntime struct {
	createSpec containerSpec
	started    bool
	stopped    bool
	removed    bool
	closed     bool
	runs       []runRequest
	runResult  runResult
	files      map[string]string
}

func (f *fakeRuntime) Create(_ context.Context, spec containerSpec) (string, error) {
	f.createSpec = spec
	return "container-1", nil
}

func (f *fakeRuntime) Start(context.Context, string) error {
	f.started = true
	return nil
}

func (f *fakeRuntime) Stop(context.Context, string) error {
	f.stopped = true
	return nil
}

func (f *fakeRuntime) Remove(context.Context, string) error {
	f.removed = true
	return nil
}

func (f *fakeRuntime) Run(_ context.Context, _ string, req runRequest) (runResult, error) {
	f.runs = append(f.runs, req)
	return f.runResult, nil
}

func (f *fakeRuntime) ReadFile(_ context.Context, _, path string) ([]byte, error) {
	if f.files == nil {
		return nil, errors.New("file not found")
	}
	content, ok := f.files[path]
	if !ok {
		return nil, errors.New("file not found")
	}
	return []byte(content), nil
}

func (f *fakeRuntime) WriteFile(_ context.Context, _, path string, data []byte, _ int64) error {
	if f.files == nil {
		f.files = map[string]string{}
	}
	f.files[path] = string(data)
	return nil
}

func (f *fakeRuntime) Close() error {
	f.closed = true
	return nil
}
