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

package daytona

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

func TestOptionsInitializeAndCloseDeleteTemporarySandbox(t *testing.T) {
	t.Parallel()

	if _, err := NewWorkspace(WithContainerWorkdir("relative")); err == nil || !strings.Contains(err.Error(), "container workdir must be absolute") {
		t.Fatalf("NewWorkspace should reject relative container workdir, got %v", err)
	}
	if _, err := NewWorkspace(WithEnv("1BAD", "value")); err == nil || !strings.Contains(err.Error(), "not a valid shell identifier") {
		t.Fatalf("NewWorkspace should reject invalid env name, got %v", err)
	}
	if _, err := NewWorkspace(WithRequestTimeout(0)); err == nil || !strings.Contains(err.Error(), "request timeout must be positive") {
		t.Fatalf("NewWorkspace should reject non-positive request timeout, got %v", err)
	}

	handle := newFakeHandle("sandbox-1")
	runtime := &fakeRuntime{createHandle: handle}
	hostWorkdir := t.TempDir()
	ws, err := NewWorkspace(
		WithWorkspaceID("workspace-1"),
		WithImage("python:3.12"),
		WithContainerWorkdir("/home/daytona/project"),
		WithHostWorkdir(hostWorkdir),
		WithEnv("DATASET", "sales"),
		WithRequestTimeout(30*time.Second),
		WithOpenTimeout(45*time.Second),
		withRuntime(runtime),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}

	if err := ws.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if !ws.IsAlive() || ws.WorkspaceID() != "workspace-1" || ws.WorkspaceRoot() != "/home/daytona/project" {
		t.Fatalf("workspace lifecycle mismatch: alive=%v id=%q root=%q", ws.IsAlive(), ws.WorkspaceID(), ws.WorkspaceRoot())
	}
	if runtime.createSpec.Image != "python:3.12" || runtime.createSpec.Workdir != "/home/daytona/project" {
		t.Fatalf("create spec image/workdir mismatch: %#v", runtime.createSpec)
	}
	if runtime.createSpec.Env["DATASET"] != "sales" || runtime.createSpec.RequestTimeout != 30*time.Second || runtime.createSpec.OpenTimeout != 45*time.Second {
		t.Fatalf("create spec env/timeouts mismatch: %#v", runtime.createSpec)
	}
	if _, err := os.Stat(filepath.Join(hostWorkdir, "data")); err != nil {
		t.Fatalf("Initialize should create host mirror data dir: %v", err)
	}

	instructions, err := ws.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if !strings.Contains(instructions, "Daytona") || !strings.Contains(instructions, "/home/daytona/project") {
		t.Fatalf("instructions should describe Daytona workspace and workdir: %s", instructions)
	}

	tools, err := ws.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if got := strings.Join(toolNames(tools), ","); got != "Bash,Edit,Glob,Grep,Read,Write" {
		t.Fatalf("unexpected Daytona workspace tools: %s", got)
	}

	if err := ws.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !handle.deleted || handle.disconnected || !runtime.closed || ws.IsAlive() {
		t.Fatalf("temporary sandbox should be deleted and runtime closed: deleted=%v disconnected=%v runtimeClosed=%v alive=%v", handle.deleted, handle.disconnected, runtime.closed, ws.IsAlive())
	}
}

func TestCloseKeepsSandboxWhenRequestedOrConnectedExisting(t *testing.T) {
	t.Parallel()

	keptHandle := newFakeHandle("keep-me")
	keepRuntime := &fakeRuntime{createHandle: keptHandle}
	keepWS, err := NewWorkspace(
		WithKeepSandbox(true),
		withRuntime(keepRuntime),
	)
	if err != nil {
		t.Fatalf("NewWorkspace keep returned error: %v", err)
	}
	if err := keepWS.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize keep returned error: %v", err)
	}
	if err := keepWS.Close(context.Background()); err != nil {
		t.Fatalf("Close keep returned error: %v", err)
	}
	if keptHandle.deleted || !keptHandle.disconnected {
		t.Fatalf("kept sandbox should disconnect without delete: deleted=%v disconnected=%v", keptHandle.deleted, keptHandle.disconnected)
	}

	existingHandle := newFakeHandle("existing-1")
	getRuntime := &fakeRuntime{getHandle: existingHandle}
	existingWS, err := NewWorkspace(
		WithSandboxID("existing-1"),
		WithAPIURL("https://example.daytona.invalid"),
		withRuntime(getRuntime),
	)
	if err != nil {
		t.Fatalf("NewWorkspace existing returned error: %v", err)
	}
	if err := existingWS.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize existing returned error: %v", err)
	}
	if getRuntime.getRef != "existing-1" {
		t.Fatalf("existing workspace should connect by sandbox id, got %q", getRuntime.getRef)
	}
	if getRuntime.getSpec.APIURL != "https://example.daytona.invalid" {
		t.Fatalf("existing workspace should pass API config when connecting, got %#v", getRuntime.getSpec)
	}
	if err := existingWS.Close(context.Background()); err != nil {
		t.Fatalf("Close existing returned error: %v", err)
	}
	if existingHandle.deleted || !existingHandle.disconnected {
		t.Fatalf("existing sandbox should disconnect without delete: deleted=%v disconnected=%v", existingHandle.deleted, existingHandle.disconnected)
	}
}

func TestBashAndFileToolsRunInsideDaytonaSandbox(t *testing.T) {
	t.Parallel()

	handle := newFakeHandle("sandbox-tools")
	handle.runResult = runResult{Stdout: "inside\n", ExitCode: 0}
	ws := initializedWorkspace(t, &fakeRuntime{createHandle: handle})
	state := asstate.NewAgentState()

	bash := findTool(t, ws, "Bash")
	bashResponse := runTool(t, bash, map[string]any{
		"command":    "pwd",
		"timeout_ms": 1500,
	}, state)
	if bashResponse.State != message.ToolResultSuccess || textOutput(bashResponse) != "inside\n" {
		t.Fatalf("unexpected bash response: state=%s content=%q", bashResponse.State, textOutput(bashResponse))
	}
	if len(handle.runs) != 1 || handle.runs[0].Command != "pwd" || handle.runs[0].Workdir != defaultContainerWorkdir || handle.runs[0].Timeout != 1500*time.Millisecond {
		t.Fatalf("bash run request mismatch: %#v", handle.runs)
	}

	write := findTool(t, ws, "Write")
	writeResponse := runTool(t, write, map[string]any{
		"file_path": "data/sales.csv",
		"content":   "region,revenue\nnorth,120\n",
	}, state)
	if writeResponse.State != message.ToolResultSuccess {
		t.Fatalf("write failed: %s", textOutput(writeResponse))
	}
	if got := string(handle.files["/home/daytona/data/sales.csv"]); got != "region,revenue\nnorth,120\n" {
		t.Fatalf("file not written in Daytona sandbox: %q", got)
	}

	read := findTool(t, ws, "Read")
	readResponse := runTool(t, read, map[string]any{"file_path": "/home/daytona/data/sales.csv"}, state)
	if readResponse.State != message.ToolResultSuccess || !strings.Contains(textOutput(readResponse), "north,120") {
		t.Fatalf("read should return sandbox file content: state=%s content=%q", readResponse.State, textOutput(readResponse))
	}

	edit := findTool(t, ws, "Edit")
	editResponse := runTool(t, edit, map[string]any{
		"file_path":  "/home/daytona/data/sales.csv",
		"old_string": "north",
		"new_string": "east",
	}, state)
	if editResponse.State != message.ToolResultSuccess {
		t.Fatalf("edit failed: %s", textOutput(editResponse))
	}
	if got := string(handle.files["/home/daytona/data/sales.csv"]); !strings.Contains(got, "east,120") {
		t.Fatalf("file not edited in Daytona sandbox: %q", got)
	}
}

func TestSearchToolsAndOffloadMirror(t *testing.T) {
	t.Parallel()

	handle := newFakeHandle("sandbox-search")
	handle.runResults = []runResult{
		{Stdout: "/home/daytona/main.py\n/home/daytona/report.md\n", ExitCode: 0},
		{Stdout: "/home/daytona/main.py:3:print('sales')\n/home/daytona/report.md:1:sales\n", ExitCode: 0},
	}
	ws := initializedWorkspace(t, &fakeRuntime{createHandle: handle})

	glob := findTool(t, ws, "Glob")
	globResponse := runTool(t, glob, map[string]any{
		"pattern": "*.py",
		"path":    "/home/daytona",
	}, nil)
	if globResponse.State != message.ToolResultSuccess || textOutput(globResponse) != "/home/daytona/main.py" {
		t.Fatalf("unexpected glob response: state=%s content=%q", globResponse.State, textOutput(globResponse))
	}
	if len(handle.runs) != 1 || !strings.Contains(handle.runs[0].Command, "find '/home/daytona' -type f -print") {
		t.Fatalf("glob should run find inside Daytona sandbox, runs=%#v", handle.runs)
	}

	grep := findTool(t, ws, "Grep")
	grepResponse := runTool(t, grep, map[string]any{
		"pattern":     "sales",
		"path":        "/home/daytona",
		"glob":        "*.py",
		"output_mode": "files",
	}, nil)
	if grepResponse.State != message.ToolResultSuccess || textOutput(grepResponse) != "/home/daytona/main.py" {
		t.Fatalf("unexpected grep response: state=%s content=%q", grepResponse.State, textOutput(grepResponse))
	}

	hostWorkdir := t.TempDir()
	mirrorWS, err := NewWorkspace(
		WithHostWorkdir(hostWorkdir),
		withRuntime(&fakeRuntime{createHandle: newFakeHandle("sandbox-mirror")}),
	)
	if err != nil {
		t.Fatalf("NewWorkspace mirror returned error: %v", err)
	}
	if err := mirrorWS.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize mirror returned error: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString([]byte("daytona-payload"))
	block := message.NewDataBlock(
		message.NewBase64Source(payload, "text/plain"),
		message.WithDataBlockName("note"),
	)
	offloaded, err := mirrorWS.OffloadDataBlock(context.Background(), block)
	if err != nil {
		t.Fatalf("OffloadDataBlock returned error: %v", err)
	}
	source, ok := offloaded.Source.(*message.URLSource)
	if !ok {
		t.Fatalf("OffloadDataBlock should return URL source: %#v", offloaded.Source)
	}
	path := strings.TrimPrefix(source.URL, "file://")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile offloaded data returned error: %v", err)
	}
	if string(data) != "daytona-payload" {
		t.Fatalf("offloaded data mismatch: %q", string(data))
	}

	noMirrorWS := initializedWorkspace(t, &fakeRuntime{createHandle: newFakeHandle("sandbox-no-mirror")})
	if _, err := noMirrorWS.OffloadContext(context.Background(), "session-1", nil); err == nil || !strings.Contains(err.Error(), "requires WithHostWorkdir") {
		t.Fatalf("OffloadContext without host mirror should fail clearly, got %v", err)
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

func textOutput(response *tool.ToolResponse) string {
	if response == nil {
		return ""
	}
	var builder strings.Builder
	for _, block := range response.Content {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func toolNames(tools []asworkspace.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return names
}

type fakeRuntime struct {
	createHandle *fakeHandle
	getHandle    *fakeHandle
	createSpec   sandboxSpec
	getSpec      sandboxSpec
	getRef       string
	closed       bool
}

func (r *fakeRuntime) Create(_ context.Context, spec sandboxSpec) (sandboxHandle, error) {
	if r.createHandle == nil {
		r.createHandle = newFakeHandle(spec.ID)
	}
	r.createSpec = spec
	return r.createHandle, nil
}

func (r *fakeRuntime) Get(_ context.Context, spec sandboxSpec, sandboxIDOrName string) (sandboxHandle, error) {
	r.getSpec = spec
	r.getRef = sandboxIDOrName
	if r.getHandle == nil {
		r.getHandle = newFakeHandle(sandboxIDOrName)
	}
	return r.getHandle, nil
}

func (r *fakeRuntime) Close() error {
	r.closed = true
	return nil
}

type fakeHandle struct {
	id           string
	files        map[string][]byte
	runResult    runResult
	runResults   []runResult
	runs         []runRequest
	deleted      bool
	disconnected bool
}

func newFakeHandle(id string) *fakeHandle {
	return &fakeHandle{
		id:    id,
		files: map[string][]byte{},
	}
}

func (h *fakeHandle) ID() string {
	return h.id
}

func (h *fakeHandle) IsReady(context.Context) (bool, error) {
	return true, nil
}

func (h *fakeHandle) Run(_ context.Context, req runRequest) (runResult, error) {
	h.runs = append(h.runs, req)
	if len(h.runResults) > 0 {
		result := h.runResults[0]
		h.runResults = h.runResults[1:]
		return result, nil
	}
	return h.runResult, nil
}

func (h *fakeHandle) Read(_ context.Context, path string) ([]byte, error) {
	data, ok := h.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return append([]byte(nil), data...), nil
}

func (h *fakeHandle) Write(_ context.Context, path string, data []byte) error {
	h.files[path] = append([]byte(nil), data...)
	return nil
}

func (h *fakeHandle) Delete(context.Context) error {
	h.deleted = true
	return nil
}

func (h *fakeHandle) Disconnect(context.Context) error {
	h.disconnected = true
	return nil
}
