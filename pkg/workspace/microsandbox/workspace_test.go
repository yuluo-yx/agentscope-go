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
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

func TestOptionsInitializeAndCloseStopTemporarySandbox(t *testing.T) {
	t.Parallel()

	var nilWorkspace *Workspace
	if _, err := nilWorkspace.ListMCPs(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil ListMCPs should fail clearly, got %v", err)
	}
	if _, err := NewWorkspace(WithContainerWorkdir("relative")); err == nil || !strings.Contains(err.Error(), "container workdir must be absolute") {
		t.Fatalf("NewWorkspace should reject relative container workdir, got %v", err)
	}
	if _, err := NewWorkspace(WithEnv("1BAD", "value")); err == nil || !strings.Contains(err.Error(), "not a valid shell identifier") {
		t.Fatalf("NewWorkspace should reject invalid env name, got %v", err)
	}
	if _, err := NewWorkspace(WithRequestTimeout(0)); err == nil || !strings.Contains(err.Error(), "request timeout must be positive") {
		t.Fatalf("NewWorkspace should reject non-positive request timeout, got %v", err)
	}
	if _, err := NewWorkspace(WithCPUs(0)); err == nil || !strings.Contains(err.Error(), "CPUs must be positive") {
		t.Fatalf("NewWorkspace should reject zero CPU count, got %v", err)
	}
	if _, err := NewWorkspace(WithMemoryMiB(0)); err == nil || !strings.Contains(err.Error(), "memory must be positive") {
		t.Fatalf("NewWorkspace should reject zero memory, got %v", err)
	}

	handle := newFakeHandle("sandbox-1")
	runtime := &fakeRuntime{createHandle: handle}
	hostWorkdir := t.TempDir()
	ws, err := NewWorkspace(
		WithWorkspaceID("workspace-1"),
		WithSandboxName("agentscope-msb-test"),
		WithImage("python:3.12"),
		WithContainerWorkdir("/workspace/project"),
		WithHostWorkdir(hostWorkdir),
		WithEnv("DATASET", "sales"),
		WithCPUs(2),
		WithMemoryMiB(1024),
		WithEnsureInstalled(false),
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
	if !ws.IsAlive() || ws.WorkspaceID() != "workspace-1" || ws.WorkspaceRoot() != "/workspace/project" {
		t.Fatalf("workspace lifecycle mismatch: alive=%v id=%q root=%q", ws.IsAlive(), ws.WorkspaceID(), ws.WorkspaceRoot())
	}
	if runtime.createSpec.Name != "agentscope-msb-test" || runtime.createSpec.Image != "python:3.12" || runtime.createSpec.Workdir != "/workspace/project" {
		t.Fatalf("create spec name/image/workdir mismatch: %#v", runtime.createSpec)
	}
	if runtime.createSpec.Env["DATASET"] != "sales" ||
		runtime.createSpec.CPUs != 2 ||
		runtime.createSpec.MemoryMiB != 1024 ||
		runtime.createSpec.EnsureInstalled ||
		runtime.createSpec.RequestTimeout != 30*time.Second ||
		runtime.createSpec.OpenTimeout != 45*time.Second {
		t.Fatalf("create spec env/resources/timeouts mismatch: %#v", runtime.createSpec)
	}
	if _, err := os.Stat(filepath.Join(hostWorkdir, "data")); err != nil {
		t.Fatalf("Initialize should create host mirror data dir: %v", err)
	}

	instructions, err := ws.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if !strings.Contains(instructions, "Microsandbox") || !strings.Contains(instructions, "/workspace/project") {
		t.Fatalf("instructions should describe Microsandbox workspace and workdir: %s", instructions)
	}

	tools, err := ws.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if got := strings.Join(toolNames(tools), ","); got != "Bash,Edit,Glob,Grep,Read,Write" {
		t.Fatalf("unexpected Microsandbox workspace tools: %s", got)
	}

	if err := ws.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !handle.stopped || handle.detached || !handle.closed || !runtime.closed || ws.IsAlive() {
		t.Fatalf("temporary sandbox should be stopped and runtime closed: stopped=%v detached=%v handleClosed=%v runtimeClosed=%v alive=%v",
			handle.stopped, handle.detached, handle.closed, runtime.closed, ws.IsAlive())
	}
}

func TestCloseKeepsSandboxWhenRequested(t *testing.T) {
	t.Parallel()

	handle := newFakeHandle("keep-me")
	ws, err := NewWorkspace(
		WithKeepSandbox(true),
		withRuntime(&fakeRuntime{createHandle: handle}),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if err := ws.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if handle.stopped || !handle.detached || !handle.closed {
		t.Fatalf("kept sandbox should detach and close without stop: stopped=%v detached=%v closed=%v", handle.stopped, handle.detached, handle.closed)
	}
}

func TestBashAndFileToolsRunInsideMicrosandbox(t *testing.T) {
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
	if got := string(handle.files["/workspace/data/sales.csv"]); got != "region,revenue\nnorth,120\n" {
		t.Fatalf("file not written in Microsandbox: %q", got)
	}

	read := findTool(t, ws, "Read")
	readResponse := runTool(t, read, map[string]any{"file_path": "/workspace/data/sales.csv"}, state)
	if readResponse.State != message.ToolResultSuccess || !strings.Contains(textOutput(readResponse), "north,120") {
		t.Fatalf("read should return sandbox file content: state=%s content=%q", readResponse.State, textOutput(readResponse))
	}

	edit := findTool(t, ws, "Edit")
	editResponse := runTool(t, edit, map[string]any{
		"file_path":  "/workspace/data/sales.csv",
		"old_string": "north",
		"new_string": "east",
	}, state)
	if editResponse.State != message.ToolResultSuccess {
		t.Fatalf("edit failed: %s", textOutput(editResponse))
	}
	if got := string(handle.files["/workspace/data/sales.csv"]); !strings.Contains(got, "east,120") {
		t.Fatalf("file not edited in Microsandbox: %q", got)
	}

	relativeRead := runTool(t, read, map[string]any{"file_path": "../escape.txt"}, state)
	if relativeRead.State != message.ToolResultError || !strings.Contains(textOutput(relativeRead), "escapes workspace root") {
		t.Fatalf("relative path should be confined to workspace root: state=%s content=%q", relativeRead.State, textOutput(relativeRead))
	}
}

func TestSearchToolsAndOffloadMirror(t *testing.T) {
	t.Parallel()

	handle := newFakeHandle("sandbox-search")
	handle.runResults = []runResult{
		{Stdout: "/workspace/main.py\n/workspace/report.md\n", ExitCode: 0},
		{Stdout: "/workspace/main.py:3:print('sales')\n/workspace/report.md:1:sales\n", ExitCode: 0},
	}
	ws := initializedWorkspace(t, &fakeRuntime{createHandle: handle})

	glob := findTool(t, ws, "Glob")
	globResponse := runTool(t, glob, map[string]any{
		"pattern": "*.py",
		"path":    "/workspace",
	}, nil)
	if globResponse.State != message.ToolResultSuccess || textOutput(globResponse) != "/workspace/main.py" {
		t.Fatalf("unexpected glob response: state=%s content=%q", globResponse.State, textOutput(globResponse))
	}
	if len(handle.runs) != 1 || !strings.Contains(handle.runs[0].Command, "find '/workspace' -type f -print") {
		t.Fatalf("glob should run find inside Microsandbox, runs=%#v", handle.runs)
	}

	grep := findTool(t, ws, "Grep")
	grepResponse := runTool(t, grep, map[string]any{
		"pattern":     "sales",
		"path":        "/workspace",
		"glob":        "*.py",
		"output_mode": "files",
	}, nil)
	if grepResponse.State != message.ToolResultSuccess || textOutput(grepResponse) != "/workspace/main.py" {
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
	payload := base64.StdEncoding.EncodeToString([]byte("microsandbox-payload"))
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
	if string(data) != "microsandbox-payload" {
		t.Fatalf("offloaded data mismatch: %q", string(data))
	}
	userMessage, err := message.NewUserMessage("user", "large context")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	contextPath, err := mirrorWS.OffloadContext(context.Background(), "session-1", []*message.Message{userMessage})
	if err != nil {
		t.Fatalf("OffloadContext returned error: %v", err)
	}
	if _, err := os.Stat(contextPath); err != nil {
		t.Fatalf("OffloadContext should write a context file: %v", err)
	}
	resultPath, err := mirrorWS.OffloadToolResult(context.Background(), "session-1", message.NewToolResultBlock(
		"result-1",
		"Bash",
		message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("tool output")}},
		message.ToolResultSuccess,
	))
	if err != nil {
		t.Fatalf("OffloadToolResult returned error: %v", err)
	}
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("ReadFile offloaded tool result returned error: %v", err)
	}
	if string(resultData) != "tool output" {
		t.Fatalf("offloaded tool result mismatch: %q", string(resultData))
	}

	noMirrorWS := initializedWorkspace(t, &fakeRuntime{createHandle: newFakeHandle("sandbox-no-mirror")})
	if _, err := noMirrorWS.OffloadContext(context.Background(), "session-1", nil); err == nil || !strings.Contains(err.Error(), "requires WithHostWorkdir") {
		t.Fatalf("OffloadContext without host mirror should fail clearly, got %v", err)
	}
}

func TestConfigurationValidationDefaultsAndSDKHelpers(t *testing.T) {
	t.Parallel()

	var nilWorkspace *Workspace
	if nilWorkspace.WorkspaceID() != "" || nilWorkspace.WorkspaceRoot() != "" || nilWorkspace.SandboxName() != "" || nilWorkspace.IsAlive() {
		t.Fatalf("nil workspace accessors should return zero values")
	}
	if err := nilWorkspace.Initialize(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil Initialize should fail clearly, got %v", err)
	}
	if _, err := nilWorkspace.GetInstructions(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil GetInstructions should fail clearly, got %v", err)
	}
	if _, err := nilWorkspace.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil ListTools should fail clearly, got %v", err)
	}
	if _, err := nilWorkspace.ListSkills(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil ListSkills should fail clearly, got %v", err)
	}
	if _, err := nilWorkspace.OffloadDataBlock(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil OffloadDataBlock should fail clearly, got %v", err)
	}
	if err := nilWorkspace.AddMCP(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil AddMCP should fail clearly, got %v", err)
	}
	if err := nilWorkspace.AddSkill(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil AddSkill should fail clearly, got %v", err)
	}
	if err := nilWorkspace.RemoveSkill(context.Background(), "skill"); err != nil {
		t.Fatalf("nil RemoveSkill should be a no-op, got %v", err)
	}

	for name, opts := range map[string][]Option{
		"empty image":         {WithImage(" ")},
		"empty host workdir":  {WithHostWorkdir(" ")},
		"bad open timeout":    {WithOpenTimeout(0)},
		"zero cpus":           {WithCPUs(0)},
		"zero memory":         {WithMemoryMiB(0)},
		"nil runtime":         {withRuntime(nil)},
		"empty env name":      {WithEnv(" ", "value")},
		"relative workdir":    {WithContainerWorkdir("relative")},
		"empty container dir": {WithContainerWorkdir(" ")},
	} {
		if _, err := NewWorkspace(opts...); err == nil {
			t.Fatalf("%s should be rejected", name)
		}
	}

	defaultMCP := newTestMCP("default")
	ws, err := New(
		WithWorkspaceID(" "),
		WithSandboxName(" named "),
		WithImage(" python:3.12 "),
		WithContainerWorkdir("/custom/../project"),
		WithInstructions("run inside {workdir}"),
		WithEnsureInstalled(false),
		WithCPUs(4),
		WithMemoryMiB(2048),
		WithMCPs(defaultMCP),
		withRuntime(&fakeRuntime{}),
	)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if ws.WorkspaceID() == "" || ws.SandboxName() != "named" || ws.image != "python:3.12" || ws.containerWorkdir != "/project" {
		t.Fatalf("workspace defaults/options mismatch: id=%q name=%q image=%q workdir=%q", ws.WorkspaceID(), ws.SandboxName(), ws.image, ws.containerWorkdir)
	}
	if ws.ensureInstalled || ws.cpus != 4 || ws.memoryMiB != 2048 || len(ws.defaultMCPs) != 1 {
		t.Fatalf("workspace resource/default MCP options mismatch: %#v", ws.sandboxSpec())
	}
	instructions, err := ws.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if instructions != "run inside /project" {
		t.Fatalf("instructions should substitute workdir, got %q", instructions)
	}
	skills, err := ws.ListSkills(context.Background())
	if err != nil || len(skills) != 0 {
		t.Fatalf("ListSkills without host mirror = %#v, %v", skills, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ws.Initialize(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Initialize canceled error = %v", err)
	}
	if _, err := ws.GetInstructions(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetInstructions canceled error = %v", err)
	}
	if _, err := ws.ListTools(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListTools canceled error = %v", err)
	}
	if _, err := ws.ListMCPs(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListMCPs canceled error = %v", err)
	}
	if err := ws.AddMCP(canceled, defaultMCP); !errors.Is(err, context.Canceled) {
		t.Fatalf("AddMCP canceled error = %v", err)
	}
	if err := ws.RemoveMCP(canceled, "default"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RemoveMCP canceled error = %v", err)
	}
	if err := ws.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close canceled error = %v", err)
	}
}

func TestSDKRuntimeHelpersAndNilBranches(t *testing.T) {
	t.Parallel()

	rt, err := newSDKRuntime()
	if err != nil || rt == nil {
		t.Fatalf("newSDKRuntime = %#v, %v", rt, err)
	}
	var nilRuntime *sdkRuntime
	if _, err := nilRuntime.Create(context.Background(), sandboxSpec{}); err == nil || !strings.Contains(err.Error(), "nil SDK runtime") {
		t.Fatalf("nil Create should fail clearly, got %v", err)
	}
	if err := nilRuntime.Close(); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}

	var nilHandle *sdkHandle
	if nilHandle.ID() != "" {
		t.Fatalf("nil handle ID should be empty")
	}
	if ready, err := nilHandle.IsReady(context.Background()); err != nil || ready {
		t.Fatalf("nil handle IsReady = %v, %v", ready, err)
	}
	if _, err := nilHandle.Run(context.Background(), runRequest{}); err == nil || !strings.Contains(err.Error(), "nil SDK sandbox handle") {
		t.Fatalf("nil Run should fail clearly, got %v", err)
	}
	if _, err := nilHandle.Read(context.Background(), "/tmp/file"); err == nil || !strings.Contains(err.Error(), "nil SDK sandbox handle") {
		t.Fatalf("nil Read should fail clearly, got %v", err)
	}
	if err := nilHandle.Write(context.Background(), "/tmp/file", []byte("x")); err == nil || !strings.Contains(err.Error(), "nil SDK sandbox handle") {
		t.Fatalf("nil Write should fail clearly, got %v", err)
	}
	if err := nilHandle.Stop(context.Background()); err != nil {
		t.Fatalf("nil Stop returned error: %v", err)
	}
	if err := nilHandle.Detach(context.Background()); err != nil {
		t.Fatalf("nil Detach returned error: %v", err)
	}
	if err := nilHandle.Close(); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if err := nilHandle.ensureWorkdir(context.Background()); err == nil || !strings.Contains(err.Error(), "nil SDK sandbox handle") {
		t.Fatalf("nil ensureWorkdir should fail clearly, got %v", err)
	}
	if nilHandle.workdir() != defaultContainerWorkdir || nilHandle.requestTimeout() != defaultRequestTimeout {
		t.Fatalf("nil handle defaults mismatch")
	}

	handle := &sdkHandle{spec: sandboxSpec{Workdir: "/custom", RequestTimeout: 2 * time.Second}}
	if handle.workdir() != "/custom" || handle.requestTimeout() != 2*time.Second {
		t.Fatalf("handle spec defaults mismatch")
	}
	ctx := context.Background()
	noTimeoutCtx, noTimeoutCancel := contextWithOptionalTimeout(ctx, 0)
	noTimeoutCancel()
	if noTimeoutCtx != ctx {
		t.Fatalf("zero timeout should return original context")
	}
	timeoutCtx, timeoutCancel := contextWithOptionalTimeout(ctx, time.Second)
	defer timeoutCancel()
	if _, ok := timeoutCtx.Deadline(); !ok {
		t.Fatalf("positive timeout should set a deadline")
	}
	if nameFromSpec(sandboxSpec{Name: " named ", ID: "id"}) != "named" || nameFromSpec(sandboxSpec{ID: " id "}) != "id" || nameFromSpec(sandboxSpec{}) != "agentscope-microsandbox" {
		t.Fatalf("nameFromSpec fallback mismatch")
	}
	if imageFromSpec(sandboxSpec{}) != defaultImage || imageFromSpec(sandboxSpec{Image: "custom"}) != "custom" {
		t.Fatalf("imageFromSpec fallback mismatch")
	}
	if workdirFromSpec(sandboxSpec{}) != defaultContainerWorkdir || workdirFromSpec(sandboxSpec{Workdir: "/custom"}) != "/custom" {
		t.Fatalf("workdirFromSpec fallback mismatch")
	}
	for input, want := range map[string]string{
		"":             "/",
		"file":         "/",
		"/file":        "/",
		"/data/file":   "/data",
		"/data/nested": "/data",
	} {
		if got := filepathDir(input); got != want {
			t.Fatalf("filepathDir(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMCPPersistenceSkillLifecycleAndRuntimeErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	hostWorkdir := t.TempDir()
	defaultMCP := newTestMCP("weather")
	ws, err := NewWorkspace(
		WithHostWorkdir(hostWorkdir),
		WithMCPs(defaultMCP),
		withRuntime(&fakeRuntime{createHandle: newFakeHandle("sandbox-mcp")}),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	mcps, err := ws.ListMCPs(ctx)
	if err != nil {
		t.Fatalf("ListMCPs returned error: %v", err)
	}
	if len(mcps) != 1 || mcps[0].Name() != "weather" {
		t.Fatalf("initial MCPs mismatch: %#v", mcps)
	}
	mcps[0] = nil
	if ws.mcps[0] == nil {
		t.Fatalf("ListMCPs should return a shallow copy")
	}
	if err := ws.AddMCP(ctx, nil); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("AddMCP nil error = %v", err)
	}
	if err := ws.AddMCP(ctx, defaultMCP); err == nil || !strings.Contains(err.Error(), "duplicate MCP") {
		t.Fatalf("AddMCP duplicate error = %v", err)
	}
	if err := ws.RemoveMCP(ctx, " "); err == nil || !strings.Contains(err.Error(), "MCP name is empty") {
		t.Fatalf("RemoveMCP empty error = %v", err)
	}
	if err := ws.RemoveMCP(ctx, "missing"); err != nil {
		t.Fatalf("RemoveMCP missing returned error: %v", err)
	}
	files := newTestMCP("files")
	if err := ws.AddMCP(ctx, files); err != nil {
		t.Fatalf("AddMCP returned error: %v", err)
	}
	if err := ws.RemoveMCP(ctx, "weather"); err != nil {
		t.Fatalf("RemoveMCP returned error: %v", err)
	}
	if ws.findMCP("weather") != -1 || ws.findMCP("files") != 0 {
		t.Fatalf("findMCP mismatch after remove: %#v", ws.mcps)
	}
	var configs []asworkspace.MCPClientConfig
	data, err := os.ReadFile(filepath.Join(hostWorkdir, ".mcp"))
	if err != nil {
		t.Fatalf("read .mcp returned error: %v", err)
	}
	if err := json.Unmarshal(data, &configs); err != nil {
		t.Fatalf("unmarshal .mcp returned error: %v", err)
	}
	if len(configs) != 1 || configs[0].Name != "files" {
		t.Fatalf("persisted MCP configs mismatch: %#v", configs)
	}

	sourceSkill := writeSkillDir(t, t.TempDir(), "review")
	if err := ws.AddSkill(ctx, sourceSkill); err != nil {
		t.Fatalf("AddSkill returned error: %v", err)
	}
	skills, err := ws.ListSkills(ctx)
	if err != nil || len(skills) != 1 || skills[0].Name != "review" {
		t.Fatalf("ListSkills after AddSkill = %#v, %v", skills, err)
	}
	if err := ws.RemoveSkill(ctx, "missing"); err != nil {
		t.Fatalf("RemoveSkill missing returned error: %v", err)
	}
	if err := ws.RemoveSkill(ctx, "review"); err != nil {
		t.Fatalf("RemoveSkill returned error: %v", err)
	}

	restoreDir := t.TempDir()
	restoreConfig := newTestMCP("manual").config
	encoded, err := json.Marshal([]asworkspace.MCPClientConfig{restoreConfig})
	if err != nil {
		t.Fatalf("marshal restore MCP returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(restoreDir, ".mcp"), encoded, 0o600); err != nil {
		t.Fatalf("write restore .mcp returned error: %v", err)
	}
	restoreWS, err := NewWorkspace(WithHostWorkdir(restoreDir), WithMCPs(defaultMCP), withRuntime(&fakeRuntime{createHandle: newFakeHandle("restore")}))
	if err != nil {
		t.Fatalf("NewWorkspace restore returned error: %v", err)
	}
	if err := restoreWS.Initialize(ctx); err != nil {
		t.Fatalf("Initialize restore returned error: %v", err)
	}
	restored, err := restoreWS.ListMCPs(ctx)
	if err != nil || len(restored) != 1 || restored[0].Name() != "manual" {
		t.Fatalf("restored MCPs mismatch: %#v, %v", restored, err)
	}
	provider, ok := restored[0].(asworkspace.MCPConfigProvider)
	if !ok {
		t.Fatalf("restored MCP should expose config")
	}
	cloned, err := provider.MCPClientConfig()
	if err != nil {
		t.Fatalf("MCPClientConfig returned error: %v", err)
	}
	cloned.HTTP.Headers["X-Test"] = "changed"
	again, err := provider.MCPClientConfig()
	if err != nil || again.HTTP.Headers["X-Test"] != "yes" {
		t.Fatalf("MCPClientConfig should deep clone HTTP headers: %#v, %v", again, err)
	}
	if tools, err := restored[0].ListTools(ctx); err != nil || len(tools) != 0 {
		t.Fatalf("persisted MCP ListTools = %#v, %v", tools, err)
	}
	if err := restored[0].Connect(ctx); err != nil {
		t.Fatalf("persisted MCP Connect returned error: %v", err)
	}
	if err := restored[0].Close(); err != nil {
		t.Fatalf("persisted MCP Close returned error: %v", err)
	}
	var nilPersisted *persistedMCPClient
	if nilPersisted.Name() != "" || nilPersisted.IsStateful() || nilPersisted.IsConnected() {
		t.Fatalf("nil persisted MCP identity/state mismatch")
	}
	if _, err := nilPersisted.MCPClientConfig(); err == nil || !strings.Contains(err.Error(), "nil persisted MCP client") {
		t.Fatalf("nil persisted MCP config error = %v", err)
	}
	if _, err := mcpConfig(nil); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("mcpConfig nil error = %v", err)
	}
	if _, err := mcpConfig(nonConfigMCP{name: "runtime-only"}); err == nil || !strings.Contains(err.Error(), "cannot be persisted") {
		t.Fatalf("mcpConfig non-provider error = %v", err)
	}
	if _, err := mcpConfig(&testMCP{name: "broken", configErr: errors.New("config failed")}); err == nil || !strings.Contains(err.Error(), "config failed") {
		t.Fatalf("mcpConfig provider error = %v", err)
	}

	invalidDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(invalidDir, ".mcp"), []byte("{bad json"), 0o600); err != nil {
		t.Fatalf("write invalid .mcp returned error: %v", err)
	}
	invalidWS, err := NewWorkspace(WithHostWorkdir(invalidDir), WithMCPs(defaultMCP), withRuntime(&fakeRuntime{createHandle: newFakeHandle("invalid")}))
	if err != nil {
		t.Fatalf("NewWorkspace invalid returned error: %v", err)
	}
	if err := invalidWS.Initialize(ctx); err != nil {
		t.Fatalf("Initialize invalid returned error: %v", err)
	}
	fallbackMCPs, err := invalidWS.ListMCPs(ctx)
	if err != nil || len(fallbackMCPs) != 1 || fallbackMCPs[0].Name() != "weather" {
		t.Fatalf("invalid .mcp should fall back to default MCPs: %#v, %v", fallbackMCPs, err)
	}

	if err := os.WriteFile(filepath.Join(hostWorkdir, ".mcp"), []byte(`[]`), 0o600); err != nil {
		t.Fatalf("write reset .mcp returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hostWorkdir, "data", "payload.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatalf("write reset payload returned error: %v", err)
	}
	if err := ws.Reset(ctx); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	for _, path := range []string{filepath.Join(hostWorkdir, ".mcp"), filepath.Join(hostWorkdir, "data"), filepath.Join(hostWorkdir, "skills"), filepath.Join(hostWorkdir, "sessions")} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("Reset should remove %s, statErr=%v", path, statErr)
		}
	}

	createErr := errors.New("create failed")
	if err := (&Workspace{runtime: &fakeRuntime{createErr: createErr}}).Initialize(ctx); !errors.Is(err, createErr) {
		t.Fatalf("Initialize create error = %v", err)
	}
	readyErr := errors.New("ready failed")
	if err := (&Workspace{runtime: &fakeRuntime{createHandle: &fakeHandle{readyErr: readyErr}}}).Initialize(ctx); !errors.Is(err, readyErr) {
		t.Fatalf("Initialize ready error = %v", err)
	}
	notReady := newFakeHandle("not-ready")
	notReady.readySet = true
	notReady.ready = false
	if err := (&Workspace{runtime: &fakeRuntime{createHandle: notReady}}).Initialize(ctx); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("Initialize not-ready error = %v", err)
	}
	stopErr := errors.New("stop failed")
	handleCloseErr := errors.New("handle close failed")
	runtimeCloseErr := errors.New("runtime close failed")
	closeHandle := newFakeHandle("close")
	closeHandle.stopErr = stopErr
	closeHandle.closeErr = handleCloseErr
	closeWS := &Workspace{handle: closeHandle, runtime: &fakeRuntime{closeErr: runtimeCloseErr}, ownsRuntime: true, alive: true}
	err = closeWS.Close(ctx)
	if err == nil || !strings.Contains(err.Error(), "stop failed") || !strings.Contains(err.Error(), "handle close failed") || !strings.Contains(err.Error(), "runtime close failed") {
		t.Fatalf("Close joined error = %v", err)
	}
}

func TestToolMetadataAndErrorBranches(t *testing.T) {
	t.Parallel()

	ws, err := NewWorkspace(withRuntime(&fakeRuntime{}))
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	tools, err := ws.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	for _, current := range tools {
		if current.Name() == "" || current.Description() == "" || current.InputSchema() == nil {
			t.Fatalf("tool metadata incomplete for %#v", current)
		}
		_ = current.IsConcurrencySafe()
		_ = current.IsReadOnly()
		if current.IsExternalTool() || current.IsStateInjected() || current.IsMCP() || current.MCPName() != "" {
			t.Fatalf("workspace tool should not report external/state/MCP metadata")
		}
		if _, err := current.CheckPermissions(context.Background(), map[string]any{}, nil); err != nil {
			t.Fatalf("CheckPermissions returned error for %s: %v", current.Name(), err)
		}
		_ = current.MatchRule("*", map[string]any{"command": "pwd", "file_path": "/tmp/a"})
		_ = current.GenerateSuggestions(map[string]any{"command": "pwd", "file_path": "/tmp/a"})
		response := runTool(t, current, map[string]any{}, nil)
		if response.State != message.ToolResultError || !strings.Contains(textOutput(response), "not initialized") {
			t.Fatalf("uninitialized tool should fail clearly: %s %q", current.Name(), textOutput(response))
		}
	}

	handle := newFakeHandle("sandbox-errors")
	handle.runErr = errors.New("run failed")
	handle.readErr = errors.New("read failed")
	handle.writeErr = errors.New("write failed")
	initialized := initializedWorkspace(t, &fakeRuntime{createHandle: handle})
	bashResponse := runTool(t, findTool(t, initialized, "Bash"), map[string]any{"command": "pwd"}, nil)
	if bashResponse.State != message.ToolResultError || !strings.Contains(textOutput(bashResponse), "run failed") {
		t.Fatalf("bash runtime error = %s %q", bashResponse.State, textOutput(bashResponse))
	}
	readResponse := runTool(t, findTool(t, initialized, "Read"), map[string]any{"file_path": "missing.txt"}, nil)
	if readResponse.State != message.ToolResultError || !strings.Contains(textOutput(readResponse), "read failed") {
		t.Fatalf("read runtime error = %s %q", readResponse.State, textOutput(readResponse))
	}
	writeResponse := runTool(t, findTool(t, initialized, "Write"), map[string]any{"file_path": "out.txt", "content": "x"}, nil)
	if writeResponse.State != message.ToolResultError || !strings.Contains(textOutput(writeResponse), "write failed") {
		t.Fatalf("write runtime error = %s %q", writeResponse.State, textOutput(writeResponse))
	}
	editResponse := runTool(t, findTool(t, initialized, "Edit"), map[string]any{"file_path": "out.txt", "old_string": "same", "new_string": "same"}, nil)
	if editResponse.State != message.ToolResultError || !strings.Contains(textOutput(editResponse), "identical") {
		t.Fatalf("edit identical error = %s %q", editResponse.State, textOutput(editResponse))
	}
	globResponse := runTool(t, findTool(t, initialized, "Glob"), map[string]any{}, nil)
	if globResponse.State != message.ToolResultError || !strings.Contains(textOutput(globResponse), "pattern is required") {
		t.Fatalf("glob missing pattern error = %s %q", globResponse.State, textOutput(globResponse))
	}
	grepResponse := runTool(t, findTool(t, initialized, "Grep"), map[string]any{"pattern": "["}, nil)
	if grepResponse.State != message.ToolResultError || !strings.Contains(textOutput(grepResponse), "invalid regex") {
		t.Fatalf("grep invalid pattern error = %s %q", grepResponse.State, textOutput(grepResponse))
	}

	if got := filterGlobMatches("/workspace", "/workspace/a.go\n/workspace/nested/b.txt\n", "**/*.txt"); len(got) != 1 || got[0] != "/workspace/nested/b.txt" {
		t.Fatalf("filterGlobMatches recursive = %#v", got)
	}
	if !matchGlob("**/*.go", "cmd/main.go") || matchGlob("*.go", "cmd/main.go") {
		t.Fatalf("matchGlob recursive/basic mismatch")
	}
	counts := filterGrepOutput("/workspace/a.go:1:x\n/workspace/a.go:2:y\n/workspace/b.txt:1:z\n", "", "count")
	if len(counts) != 2 || counts[0] != "/workspace/a.go:2" || counts[1] != "/workspace/b.txt:1" {
		t.Fatalf("filterGrepOutput count = %#v", counts)
	}
	if got := limitStrings([]string{"a", "b", "c"}, 2); strings.Join(got, ",") != "a,b" {
		t.Fatalf("limitStrings = %#v", got)
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
	createSpec   sandboxSpec
	closed       bool
	createErr    error
	closeErr     error
}

func (r *fakeRuntime) Create(_ context.Context, spec sandboxSpec) (sandboxHandle, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	if r.createHandle == nil {
		r.createHandle = newFakeHandle(spec.Name)
	}
	r.createSpec = spec
	return r.createHandle, nil
}

func (r *fakeRuntime) Close() error {
	r.closed = true
	return r.closeErr
}

type fakeHandle struct {
	id         string
	files      map[string][]byte
	runResult  runResult
	runResults []runResult
	runs       []runRequest
	stopped    bool
	detached   bool
	closed     bool
	ready      bool
	readySet   bool
	readyErr   error
	runErr     error
	readErr    error
	writeErr   error
	stopErr    error
	detachErr  error
	closeErr   error
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
	if h.readyErr != nil {
		return false, h.readyErr
	}
	if h.readySet {
		return h.ready, nil
	}
	return true, nil
}

func (h *fakeHandle) Run(_ context.Context, req runRequest) (runResult, error) {
	h.runs = append(h.runs, req)
	if h.runErr != nil {
		return runResult{}, h.runErr
	}
	if len(h.runResults) > 0 {
		result := h.runResults[0]
		h.runResults = h.runResults[1:]
		return result, nil
	}
	return h.runResult, nil
}

func (h *fakeHandle) Read(_ context.Context, path string) ([]byte, error) {
	if h.readErr != nil {
		return nil, h.readErr
	}
	data, ok := h.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return append([]byte(nil), data...), nil
}

func (h *fakeHandle) Write(_ context.Context, path string, data []byte) error {
	if h.writeErr != nil {
		return h.writeErr
	}
	h.files[path] = append([]byte(nil), data...)
	return nil
}

func (h *fakeHandle) Stop(context.Context) error {
	h.stopped = true
	return h.stopErr
}

func (h *fakeHandle) Detach(context.Context) error {
	h.detached = true
	return h.detachErr
}

func (h *fakeHandle) Close() error {
	h.closed = true
	return h.closeErr
}

type testMCP struct {
	name      string
	stateful  bool
	config    asworkspace.MCPClientConfig
	configErr error
}

func newTestMCP(name string) *testMCP {
	return &testMCP{
		name: name,
		config: asworkspace.MCPClientConfig{
			Name:     name,
			Type:     asworkspace.MCPClientTypeHTTP,
			Stateful: true,
			HTTP: &asworkspace.MCPHTTPConfig{
				URL:     "http://localhost/" + name,
				Headers: map[string]string{"X-Test": "yes"},
			},
			Stdio: &asworkspace.MCPStdioConfig{
				Command: "server",
				Args:    []string{"--name", name},
				Env:     map[string]string{"TOKEN": "secret"},
			},
			EnabledTools:  []string{"allowed"},
			DisabledTools: []string{"blocked"},
		},
	}
}

func (m *testMCP) Name() string {
	if m == nil {
		return ""
	}
	return m.name
}

func (m *testMCP) IsStateful() bool {
	return m != nil && m.stateful
}

func (m *testMCP) IsConnected() bool {
	return false
}

func (m *testMCP) Connect(context.Context) error {
	return nil
}

func (m *testMCP) Close() error {
	return nil
}

func (m *testMCP) ListTools(context.Context) ([]tool.Tool, error) {
	return []tool.Tool{}, nil
}

func (m *testMCP) MCPClientConfig() (asworkspace.MCPClientConfig, error) {
	if m.configErr != nil {
		return asworkspace.MCPClientConfig{}, m.configErr
	}
	return cloneMCPClientConfig(m.config), nil
}

type nonConfigMCP struct {
	name string
}

func (m nonConfigMCP) Name() string {
	return m.name
}

func (m nonConfigMCP) IsStateful() bool {
	return false
}

func (m nonConfigMCP) IsConnected() bool {
	return false
}

func (m nonConfigMCP) Connect(context.Context) error {
	return nil
}

func (m nonConfigMCP) Close() error {
	return nil
}

func (m nonConfigMCP) ListTools(context.Context) ([]tool.Tool, error) {
	return []tool.Tool{}, nil
}

func writeSkillDir(t *testing.T, root, name string) string {
	t.Helper()

	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll skill dir returned error: %v", err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s skill\n---\nUse this skill.\n", name, name)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile SKILL.md returned error: %v", err)
	}
	return dir
}
