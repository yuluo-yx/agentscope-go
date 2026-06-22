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
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func TestWorkspacePersistsMCPsAndRoutesThemThroughGateway(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	gateway := newFakeGateway()
	weather := newPersistedMCP("weather")
	ws, err := NewWorkspace(
		WithHostWorkdir(workdir),
		withRuntime(&fakeRuntime{}),
		WithMCPGateway(gateway),
		WithMCPs(weather),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if initErr := ws.Initialize(ctx); initErr != nil {
		t.Fatalf("Initialize returned error: %v", initErr)
	}
	if !gateway.bootstrapped || len(gateway.added) != 1 || gateway.added[0].Name != "weather" {
		t.Fatalf("gateway should bootstrap and receive seeded MCP: bootstrapped=%t added=%#v", gateway.bootstrapped, gateway.added)
	}
	assertMCPFileNames(t, filepath.Join(workdir, ".mcp"), "weather")

	mcps, err := ws.ListMCPs(ctx)
	if err != nil {
		t.Fatalf("ListMCPs returned error: %v", err)
	}
	if len(mcps) != 1 || mcps[0].Name() != "weather" {
		t.Fatalf("unexpected workspace MCPs: %#v", mcps)
	}
	tools, err := mcps[0].ListTools(ctx)
	if err != nil {
		t.Fatalf("gateway MCP ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__weather__forecast" || !tools[0].IsMCP() {
		t.Fatalf("unexpected gateway MCP tools: %#v", tools)
	}

	news := newPersistedMCP("news")
	if err := ws.AddMCP(ctx, news); err != nil {
		t.Fatalf("AddMCP returned error: %v", err)
	}
	assertMCPFileNames(t, filepath.Join(workdir, ".mcp"), "weather", "news")
	if err := ws.RemoveMCP(ctx, "weather"); err != nil {
		t.Fatalf("RemoveMCP returned error: %v", err)
	}
	if strings.Join(gateway.removed, ",") != "weather" {
		t.Fatalf("gateway should remove weather, got %#v", gateway.removed)
	}
	assertMCPFileNames(t, filepath.Join(workdir, ".mcp"), "news")

	if err := ws.Close(ctx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !gateway.closed {
		t.Fatalf("gateway Close should be called during workspace Close")
	}
}

func TestWorkspaceUsesHostUserForBindMountedWorkdir(t *testing.T) {
	t.Parallel()

	uid := os.Getuid()
	gid := os.Getgid()
	if uid < 0 || gid < 0 {
		t.Skip("host user IDs are unavailable on this platform")
	}

	runtime := &fakeRuntime{}
	ws, err := NewWorkspace(WithHostWorkdir(t.TempDir()), withRuntime(runtime))
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}

	if got, want := runtime.createSpec.User, fmt.Sprintf("%d:%d", uid, gid); got != want {
		t.Fatalf("container should run as host user to keep bind mount writable: got %q want %q", got, want)
	}

	bash := findTool(t, ws, "Bash")
	runTool(t, bash, map[string]any{"command": "pwd"}, nil)
	if len(runtime.runs) != 1 {
		t.Fatalf("expected one runtime run, got %d", len(runtime.runs))
	}
	if got, want := runtime.runs[0].User, fmt.Sprintf("%d:%d", uid, gid); got != want {
		t.Fatalf("exec should run as host user to keep bind mount writable: got %q want %q", got, want)
	}
}

func TestTarFileUsesHostOwnershipWithoutTopLevelMountHeader(t *testing.T) {
	t.Parallel()

	uid := os.Getuid()
	gid := os.Getgid()
	if uid < 0 || gid < 0 {
		t.Skip("host user IDs are unavailable on this platform")
	}

	archive, err := tarFile("/workspace/data/note.txt", []byte("hello"), 0o644)
	if err != nil {
		t.Fatalf("tarFile returned error: %v", err)
	}
	reader := tar.NewReader(archive)
	headers := map[string]*tar.Header{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar reader returned error: %v", err)
		}
		headers[header.Name] = header
	}
	if _, exists := headers["workspace"]; exists {
		t.Fatalf("tar archive should not rewrite the bind mount root header")
	}
	for _, name := range []string{"workspace/data", "workspace/data/note.txt"} {
		header, exists := headers[name]
		if !exists {
			t.Fatalf("missing tar header %q; got %#v", name, headers)
		}
		if header.Uid != uid || header.Gid != gid {
			t.Fatalf("tar header %q should use host ownership: got %d:%d want %d:%d", name, header.Uid, header.Gid, uid, gid)
		}
	}
}

func TestWorkspaceRestoresMCPFileThroughGateway(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	data, err := json.Marshal([]asworkspace.MCPClientConfig{newPersistedMCP("weather").config})
	if err != nil {
		t.Fatalf("marshal .mcp: %v", err)
	}
	if writeErr := os.WriteFile(filepath.Join(workdir, ".mcp"), data, 0o600); writeErr != nil {
		t.Fatalf("write .mcp: %v", writeErr)
	}

	gateway := newFakeGateway()
	ws, err := NewWorkspace(
		WithHostWorkdir(workdir),
		withRuntime(&fakeRuntime{}),
		WithMCPGateway(gateway),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if len(gateway.added) != 1 || gateway.added[0].Name != "weather" {
		t.Fatalf("gateway should register restored .mcp entries: %#v", gateway.added)
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
	createErr  error
	startErr   error
	stopErr    error
	removeErr  error
	closeErr   error
}

func (f *fakeRuntime) Create(_ context.Context, spec containerSpec) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.createSpec = spec
	return "container-1", nil
}

func (f *fakeRuntime) Start(context.Context, string) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = true
	return nil
}

func (f *fakeRuntime) Stop(context.Context, string) error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stopped = true
	return nil
}

func (f *fakeRuntime) Remove(context.Context, string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
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
	if f.closeErr != nil {
		return f.closeErr
	}
	f.closed = true
	return nil
}

type persistedMCP struct {
	config asworkspace.MCPClientConfig
}

func newPersistedMCP(name string) *persistedMCP {
	return &persistedMCP{config: asworkspace.MCPClientConfig{
		Name:         name,
		Type:         asworkspace.MCPClientTypeHTTP,
		Stateful:     false,
		HTTP:         &asworkspace.MCPHTTPConfig{URL: "https://example.invalid/" + name},
		EnabledTools: []string{"forecast"},
	}}
}

func (m *persistedMCP) Name() string {
	return m.config.Name
}

func (m *persistedMCP) IsStateful() bool {
	return m.config.Stateful
}

func (m *persistedMCP) IsConnected() bool {
	return !m.config.Stateful
}

func (m *persistedMCP) Connect(context.Context) error {
	return nil
}

func (m *persistedMCP) Close() error {
	return nil
}

func (m *persistedMCP) ListTools(context.Context) ([]tool.Tool, error) {
	return nil, errors.New("persisted MCP spec should be routed through the gateway")
}

func (m *persistedMCP) MCPClientConfig() (asworkspace.MCPClientConfig, error) {
	config := m.config
	config.EnabledTools = append([]string(nil), m.config.EnabledTools...)
	if m.config.HTTP != nil {
		httpConfig := *m.config.HTTP
		config.HTTP = &httpConfig
	}
	return config, nil
}

type fakeGateway struct {
	bootstrapped bool
	added        []asworkspace.MCPClientConfig
	removed      []string
	closed       bool
	configs      map[string]asworkspace.MCPClientConfig
	bootstrapErr error
	addErr       error
	removeErr    error
	listErr      error
	closeErr     error
}

func newFakeGateway() *fakeGateway {
	return &fakeGateway{configs: map[string]asworkspace.MCPClientConfig{}}
}

func (g *fakeGateway) Bootstrap(context.Context) error {
	if g.bootstrapErr != nil {
		return g.bootstrapErr
	}
	g.bootstrapped = true
	return nil
}

func (g *fakeGateway) AddMCP(_ context.Context, config asworkspace.MCPClientConfig) error {
	if g.addErr != nil {
		return g.addErr
	}
	g.added = append(g.added, config)
	g.configs[config.Name] = config
	return nil
}

func (g *fakeGateway) RemoveMCP(_ context.Context, name string) error {
	if g.removeErr != nil {
		return g.removeErr
	}
	g.removed = append(g.removed, name)
	delete(g.configs, name)
	return nil
}

func (g *fakeGateway) ListMCPs(context.Context) ([]asworkspace.MCPClientConfig, error) {
	if g.listErr != nil {
		return nil, g.listErr
	}
	out := make([]asworkspace.MCPClientConfig, 0, len(g.configs))
	for _, config := range g.configs {
		out = append(out, config)
	}
	return out, nil
}

func (g *fakeGateway) NewMCPClient(config asworkspace.MCPClientConfig, connected bool) asworkspace.MCPClient {
	return &fakeGatewayMCPClient{config: config, connected: connected}
}

func (g *fakeGateway) Close(context.Context) error {
	if g.closeErr != nil {
		return g.closeErr
	}
	g.closed = true
	return nil
}

type fakeGatewayMCPClient struct {
	config    asworkspace.MCPClientConfig
	connected bool
}

func (c *fakeGatewayMCPClient) Name() string {
	return c.config.Name
}

func (c *fakeGatewayMCPClient) IsStateful() bool {
	return true
}

func (c *fakeGatewayMCPClient) IsConnected() bool {
	return c.connected
}

func (c *fakeGatewayMCPClient) Connect(context.Context) error {
	c.connected = true
	return nil
}

func (c *fakeGatewayMCPClient) Close() error {
	c.connected = false
	return nil
}

func (c *fakeGatewayMCPClient) ListTools(context.Context) ([]tool.Tool, error) {
	forecast, err := tool.NewFunctionTool(
		"mcp__"+c.config.Name+"__forecast",
		"Forecast weather.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("sunny")}, nil
		},
		tool.WithFunctionMCP(c.config.Name),
		tool.WithFunctionReadOnly(true),
	)
	if err != nil {
		return nil, err
	}
	return []tool.Tool{forecast}, nil
}

func (c *fakeGatewayMCPClient) MCPClientConfig() (asworkspace.MCPClientConfig, error) {
	return c.config, nil
}

type nonConfigDockerMCP struct {
	name string
}

func (m nonConfigDockerMCP) Name() string { return m.name }

func (nonConfigDockerMCP) IsStateful() bool { return false }

func (nonConfigDockerMCP) IsConnected() bool { return false }

func (nonConfigDockerMCP) Connect(context.Context) error { return nil }

func (nonConfigDockerMCP) Close() error { return nil }

func (nonConfigDockerMCP) ListTools(context.Context) ([]tool.Tool, error) { return nil, nil }

type errorConfigDockerMCP struct {
	name string
	err  error
}

func (m errorConfigDockerMCP) Name() string { return m.name }

func (errorConfigDockerMCP) IsStateful() bool { return false }

func (errorConfigDockerMCP) IsConnected() bool { return false }

func (errorConfigDockerMCP) Connect(context.Context) error { return nil }

func (errorConfigDockerMCP) Close() error { return nil }

func (errorConfigDockerMCP) ListTools(context.Context) ([]tool.Tool, error) { return nil, nil }

func (m errorConfigDockerMCP) MCPClientConfig() (asworkspace.MCPClientConfig, error) {
	return asworkspace.MCPClientConfig{}, m.err
}

func assertMCPFileNames(t *testing.T, path string, expected ...string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .mcp: %v", err)
	}
	var configs []asworkspace.MCPClientConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		t.Fatalf("unmarshal .mcp: %v\n%s", err, string(data))
	}
	names := make([]string, 0, len(configs))
	for _, config := range configs {
		names = append(names, config.Name)
	}
	if strings.Join(names, ",") != strings.Join(expected, ",") {
		t.Fatalf(".mcp names mismatch: got %#v want %#v; json=%s", names, expected, string(data))
	}
}
