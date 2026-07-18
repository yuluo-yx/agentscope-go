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

package agentsandbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

func TestOptionsValidateTemplateAndWorkdir(t *testing.T) {
	t.Parallel()

	if _, err := NewWorkspace(WithNamespace("default")); err == nil || !strings.Contains(err.Error(), "template name is empty") {
		t.Fatalf("NewWorkspace should reject missing template name, got %v", err)
	}
	if _, err := NewWorkspace(
		WithTemplateName("python-sandbox-template"),
		WithContainerWorkdir("relative"),
	); err == nil || !strings.Contains(err.Error(), "container workdir must be absolute") {
		t.Fatalf("NewWorkspace should reject relative container workdir, got %v", err)
	}

	workdir := t.TempDir()
	ws, err := NewWorkspace(
		WithWorkspaceID("workspace-1"),
		WithTemplateName("python-sandbox-template"),
		WithNamespace("default"),
		WithContainerWorkdir("/home/user/project"),
		WithHostWorkdir(workdir),
		WithRequestTimeout(30*time.Second),
		WithOpenTimeout(45*time.Second),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if ws.WorkspaceID() != "workspace-1" {
		t.Fatalf("workspace ID mismatch: %q", ws.WorkspaceID())
	}
	if ws.hostWorkdir != filepath.Clean(workdir) {
		t.Fatalf("host workdir should be absolute and clean: got %q want %q", ws.hostWorkdir, filepath.Clean(workdir))
	}
}

func TestNormalizeSandboxPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		workdir string
		want    string
		wantErr string
	}{
		{name: "absolute", input: "/tmp/notes.txt", workdir: "/home/user", want: "/tmp/notes.txt"},
		{name: "relative", input: "notes/todo.txt", workdir: "/home/user", want: "/home/user/notes/todo.txt"},
		{name: "clean", input: "/home/user/../user/app.txt", workdir: "/home/user", want: "/home/user/app.txt"},
		{name: "empty", input: " ", workdir: "/home/user", wantErr: "path is empty"},
		{name: "nul", input: "bad\x00path", workdir: "/home/user", wantErr: "NUL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSandboxPath(tt.input, tt.workdir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("normalizeSandboxPath error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeSandboxPath returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeSandboxPath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSandboxTemplateManifestEnablesHeadlessService(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tools", "agentsandbox", "python-sandbox-template.yml"))
	if err != nil {
		t.Fatalf("read sandbox template manifest: %v", err)
	}

	var manifest struct {
		Kind string `yaml:"kind"`
		Spec struct {
			Service *bool `yaml:"service"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse sandbox template manifest: %v", err)
	}
	if manifest.Kind != "SandboxTemplate" {
		t.Fatalf("sandbox template manifest kind = %q, want SandboxTemplate", manifest.Kind)
	}
	if manifest.Spec.Service == nil || !*manifest.Spec.Service {
		t.Fatalf("sandbox template manifest must set spec.service=true so the router can resolve sandbox Service DNS")
	}
}

func TestWorkspaceInitializesRuntimeAndListsSandboxTools(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{handle: newFakeHandle("sandbox-1")}
	ws, err := NewWorkspace(
		WithWorkspaceID("workspace-1"),
		WithTemplateName("python-sandbox-template"),
		WithNamespace("default"),
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
	if runtime.createSpec.TemplateName != "python-sandbox-template" || runtime.createSpec.Namespace != "default" || runtime.createSpec.Workdir != "/agent" {
		t.Fatalf("runtime create spec mismatch: %#v", runtime.createSpec)
	}

	instructions, err := ws.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if !strings.Contains(instructions, "Kubernetes-based") || !strings.Contains(instructions, "/agent") {
		t.Fatalf("instructions should describe Agent Sandbox workspace and workdir: %s", instructions)
	}

	tools, err := ws.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if got := toolNames(tools); strings.Join(got, ",") != "Bash,Edit,Glob,Grep,Read,Write" {
		t.Fatalf("unexpected Agent Sandbox workspace tools: %#v", got)
	}
}

func TestWorkspaceCloseDeletesEphemeralSandboxAndDisconnectsKeptSandbox(t *testing.T) {
	t.Parallel()

	deleted := newFakeHandle("delete-me")
	deleteRuntime := &fakeRuntime{handle: deleted}
	deleteWS := initializedWorkspace(t, deleteRuntime)
	if err := deleteWS.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !deleted.closed || deleted.disconnected {
		t.Fatalf("ephemeral sandbox should close, closed=%v disconnected=%v", deleted.closed, deleted.disconnected)
	}

	kept := newFakeHandle("keep-me")
	keepRuntime := &fakeRuntime{handle: kept}
	keepWS, err := NewWorkspace(
		WithTemplateName("python-sandbox-template"),
		WithKeepSandbox(true),
		withRuntime(keepRuntime),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := keepWS.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if err := keepWS.Close(context.Background()); err != nil {
		t.Fatalf("Close keep sandbox returned error: %v", err)
	}
	if kept.closed || !kept.disconnected {
		t.Fatalf("kept sandbox should disconnect, closed=%v disconnected=%v", kept.closed, kept.disconnected)
	}
}

func TestBashToolRunsInsideSandboxWorkdir(t *testing.T) {
	t.Parallel()

	handle := newFakeHandle("sandbox-1")
	handle.runResult = runResult{Stdout: "inside\n", ExitCode: 0}
	ws := initializedWorkspace(t, &fakeRuntime{handle: handle})
	bash := findTool(t, ws, "Bash")
	response := runTool(t, bash, map[string]any{"command": "pwd", "timeout_ms": 1500}, nil)

	if response.State != message.ToolResultSuccess || textOutput(response) != "inside\n" {
		t.Fatalf("unexpected bash response: state=%s content=%q", response.State, textOutput(response))
	}
	if len(handle.runs) != 1 {
		t.Fatalf("expected one sandbox run, got %d", len(handle.runs))
	}
	req := handle.runs[0]
	if req.Command != "pwd" || req.Workdir != defaultContainerWorkdir || req.Timeout != 1500*time.Millisecond {
		t.Fatalf("run request mismatch: %#v", req)
	}
}

func TestFileToolsReadWriteAndEditInsideSandbox(t *testing.T) {
	t.Parallel()

	handle := newFakeHandle("sandbox-1")
	ws := initializedWorkspace(t, &fakeRuntime{handle: handle})
	state := asstate.NewAgentState()

	write := findTool(t, ws, "Write")
	writeResponse := runTool(t, write, map[string]any{
		"file_path": "/home/user/notes/todo.txt",
		"content":   "hello sandbox\n",
	}, state)
	if writeResponse.State != message.ToolResultSuccess {
		t.Fatalf("write failed: %s", textOutput(writeResponse))
	}
	if got := string(handle.files["/home/user/notes/todo.txt"]); got != "hello sandbox\n" {
		t.Fatalf("file not written in sandbox: %q", got)
	}

	read := findTool(t, ws, "Read")
	readResponse := runTool(t, read, map[string]any{"file_path": "/home/user/notes/todo.txt"}, state)
	if !strings.Contains(textOutput(readResponse), "hello sandbox") {
		t.Fatalf("read should return sandbox file content: %q", textOutput(readResponse))
	}

	edit := findTool(t, ws, "Edit")
	editResponse := runTool(t, edit, map[string]any{
		"file_path":  "/home/user/notes/todo.txt",
		"old_string": "sandbox",
		"new_string": "runtime",
	}, state)
	if editResponse.State != message.ToolResultSuccess {
		t.Fatalf("edit failed: %s", textOutput(editResponse))
	}
	if got := string(handle.files["/home/user/notes/todo.txt"]); got != "hello runtime\n" {
		t.Fatalf("file not edited in sandbox: %q", got)
	}
}

func TestSearchToolsRunInsideSandbox(t *testing.T) {
	t.Parallel()

	handle := newFakeHandle("sandbox-1")
	handle.runResults = []runResult{
		{Stdout: "/home/user/main.go\n/home/user/README.md\n", ExitCode: 0},
		{Stdout: "/home/user/main.go:3:func main\n/home/user/README.md:1:main\n", ExitCode: 0},
	}
	ws := initializedWorkspace(t, &fakeRuntime{handle: handle})

	glob := findTool(t, ws, "Glob")
	globResponse := runTool(t, glob, map[string]any{
		"pattern": "*.go",
		"path":    "/home/user",
	}, nil)
	if globResponse.State != message.ToolResultSuccess || textOutput(globResponse) != "/home/user/main.go" {
		t.Fatalf("unexpected glob response: state=%s content=%q", globResponse.State, textOutput(globResponse))
	}
	if len(handle.runs) != 1 || !strings.Contains(handle.runs[0].Command, "find '/home/user' -type f -print") {
		t.Fatalf("glob should run find inside sandbox, runs=%#v", handle.runs)
	}

	grep := findTool(t, ws, "Grep")
	grepResponse := runTool(t, grep, map[string]any{
		"pattern":     "main",
		"path":        "/home/user",
		"glob":        "*.go",
		"output_mode": "files",
	}, nil)
	if grepResponse.State != message.ToolResultSuccess || textOutput(grepResponse) != "/home/user/main.go" {
		t.Fatalf("unexpected grep response: state=%s content=%q", grepResponse.State, textOutput(grepResponse))
	}
	if len(handle.runs) != 2 || !strings.Contains(handle.runs[1].Command, "grep -R -n -E -- 'main' '/home/user'") {
		t.Fatalf("grep should run grep inside sandbox, runs=%#v", handle.runs)
	}
}

func TestOffloadRequiresHostWorkdirAndUsesLocalMirror(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws := initializedWorkspace(t, &fakeRuntime{handle: newFakeHandle("sandbox-1")})
	_, err := ws.OffloadContext(ctx, "session-1", nil)
	if err == nil || !strings.Contains(err.Error(), "requires WithHostWorkdir") {
		t.Fatalf("OffloadContext without host workdir should fail clearly, got %v", err)
	}

	workdir := t.TempDir()
	mirrorWS, newErr := NewWorkspace(
		WithTemplateName("python-sandbox-template"),
		WithHostWorkdir(workdir),
		withRuntime(&fakeRuntime{handle: newFakeHandle("sandbox-2")}),
	)
	if newErr != nil {
		t.Fatalf("NewWorkspace returned error: %v", newErr)
	}
	if initErr := mirrorWS.Initialize(ctx); initErr != nil {
		t.Fatalf("Initialize returned error: %v", initErr)
	}
	payload := base64.StdEncoding.EncodeToString([]byte("agent-sandbox-payload"))
	block := message.NewDataBlock(
		message.NewBase64Source(payload, "text/plain"),
		message.WithDataBlockName("note"),
	)
	offloaded, offloadErr := mirrorWS.OffloadDataBlock(ctx, block)
	if offloadErr != nil {
		t.Fatalf("OffloadDataBlock returned error: %v", offloadErr)
	}
	source, ok := offloaded.Source.(*message.URLSource)
	if !ok {
		t.Fatalf("OffloadDataBlock should return URL source: %#v", offloaded.Source)
	}
	path := strings.TrimPrefix(source.URL, "file://")
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile offloaded data returned error: %v", readErr)
	}
	if string(data) != "agent-sandbox-payload" {
		t.Fatalf("offloaded data mismatch: %q", string(data))
	}
}

func TestWorkspacePersistsAndRestoresMCPs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	weather := newPersistedMCP("weather")
	ws, err := NewWorkspace(
		WithTemplateName("python-sandbox-template"),
		WithHostWorkdir(workdir),
		WithMCPs(weather),
		withRuntime(&fakeRuntime{handle: newFakeHandle("sandbox-1")}),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if initErr := ws.Initialize(ctx); initErr != nil {
		t.Fatalf("Initialize returned error: %v", initErr)
	}
	assertMCPFileNames(t, filepath.Join(workdir, ".mcp"), "weather")

	restored, err := NewWorkspace(
		WithTemplateName("python-sandbox-template"),
		WithHostWorkdir(workdir),
		withRuntime(&fakeRuntime{handle: newFakeHandle("sandbox-2")}),
	)
	if err != nil {
		t.Fatalf("NewWorkspace restored returned error: %v", err)
	}
	if initErr := restored.Initialize(ctx); initErr != nil {
		t.Fatalf("Initialize restored returned error: %v", initErr)
	}
	mcps, err := restored.ListMCPs(ctx)
	if err != nil {
		t.Fatalf("ListMCPs returned error: %v", err)
	}
	if len(mcps) != 1 || mcps[0].Name() != "weather" {
		t.Fatalf("restored MCPs mismatch: %#v", mcps)
	}
}

func initializedWorkspace(t *testing.T, runtime *fakeRuntime) *Workspace {
	t.Helper()

	ws, err := NewWorkspace(
		WithTemplateName("python-sandbox-template"),
		withRuntime(runtime),
	)
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

func toolNames(tools []tool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return names
}

type fakeRuntime struct {
	handle     *fakeHandle
	createSpec sandboxSpec
	closed     bool
}

func (r *fakeRuntime) Create(_ context.Context, spec sandboxSpec) (sandboxHandle, error) {
	if r.handle == nil {
		r.handle = newFakeHandle(spec.ID)
	}
	r.createSpec = spec
	return r.handle, nil
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
	closed       bool
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

func (h *fakeHandle) Close(context.Context) error {
	h.closed = true
	return nil
}

func (h *fakeHandle) Disconnect(context.Context) error {
	h.disconnected = true
	return nil
}

func newPersistedMCP(name string) *persistedMCP {
	return &persistedMCP{config: asworkspace.MCPClientConfig{
		Name:     name,
		Type:     asworkspace.MCPClientTypeHTTP,
		Stateful: true,
		HTTP:     &asworkspace.MCPHTTPConfig{URL: "http://localhost/" + name},
	}}
}

type persistedMCP struct {
	config asworkspace.MCPClientConfig
}

func (m *persistedMCP) Name() string { return m.config.Name }

func (m *persistedMCP) IsStateful() bool { return m.config.Stateful }

func (m *persistedMCP) IsConnected() bool { return false }

func (m *persistedMCP) Connect(context.Context) error { return nil }

func (m *persistedMCP) Close() error { return nil }

func (m *persistedMCP) ListTools(context.Context) ([]tool.Tool, error) {
	return []tool.Tool{}, nil
}

func (m *persistedMCP) MCPClientConfig() (asworkspace.MCPClientConfig, error) {
	return m.config, nil
}

func assertMCPFileNames(t *testing.T, path string, names ...string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .mcp returned error: %v", err)
	}
	var configs []asworkspace.MCPClientConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		t.Fatalf("unmarshal .mcp returned error: %v", err)
	}
	got := make([]string, 0, len(configs))
	for _, config := range configs {
		got = append(got, config.Name)
	}
	if strings.Join(got, ",") != strings.Join(names, ",") {
		t.Fatalf(".mcp names mismatch: got %#v want %#v", got, names)
	}
}
