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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	"github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

func TestWorkspaceOptionsSpecNilAndCanceledBranches(t *testing.T) {
	t.Parallel()

	hostWorkdir := t.TempDir()
	runtime := &fakeRuntime{handle: newFakeHandle("sandbox-options")}
	ws, err := New(
		nil,
		WithWorkspaceID(""),
		WithTemplateName("python-sandbox-template"),
		WithNamespace(""),
		WithContainerWorkdir("/agent//workspace"),
		WithHostWorkdir(hostWorkdir),
		WithInstructions("workdir={workdir}"),
		WithPortForward(),
		WithGateway("gateway", "gateway-ns"),
		WithAPIURL("http://sandbox.local"),
		WithServerPort(8080),
		WithEnv("A_1", "value"),
		WithMaxUploadSize(123),
		WithMaxDownloadSize(456),
		WithRequestTimeout(time.Second),
		WithOpenTimeout(2*time.Second),
		withRuntime(runtime),
	)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if ws.WorkspaceID() == "" || ws.namespace != defaultNamespace {
		t.Fatalf("workspace defaults mismatch: id=%q namespace=%q", ws.WorkspaceID(), ws.namespace)
	}
	spec := ws.sandboxSpec()
	if spec.Workdir != "/agent/workspace" || spec.Mode != connectionModeDirectURL || spec.APIURL != "http://sandbox.local" {
		t.Fatalf("sandbox spec connection/workdir mismatch: %#v", spec)
	}
	if spec.ServerPort != 8080 || spec.Env["A_1"] != "value" || spec.RequestTimeout != time.Second || spec.OpenTimeout != 2*time.Second {
		t.Fatalf("sandbox spec values mismatch: %#v", spec)
	}
	if spec.MaxUploadSize != 123 || spec.MaxDownloadSize != 456 {
		t.Fatalf("sandbox spec size limits mismatch: %#v", spec)
	}
	spec.Env["A_1"] = "changed"
	if ws.env["A_1"] != "value" {
		t.Fatalf("sandboxSpec should clone env map, got workspace env %#v", ws.env)
	}
	instructions, err := ws.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if instructions != "workdir=/agent/workspace" {
		t.Fatalf("instructions substitution mismatch: %q", instructions)
	}

	invalidOptions := []struct {
		name string
		opt  Option
		want string
	}{
		{name: "empty container workdir", opt: WithContainerWorkdir(" "), want: "container workdir is empty"},
		{name: "relative container workdir", opt: WithContainerWorkdir("relative"), want: "container workdir must be absolute"},
		{name: "empty host workdir", opt: WithHostWorkdir(" "), want: "host workdir is empty"},
		{name: "empty api url", opt: WithAPIURL(" "), want: "API URL is empty"},
		{name: "empty gateway name", opt: WithGateway(" ", "ns"), want: "gateway name is empty"},
		{name: "empty gateway namespace", opt: WithGateway("gw", " "), want: "gateway namespace is empty"},
		{name: "negative server port", opt: WithServerPort(-1), want: "server port must be non-negative"},
		{name: "empty env name", opt: WithEnv(" ", "x"), want: "env name is empty"},
		{name: "bad env name", opt: WithEnv("1BAD", "x"), want: "not a valid shell identifier"},
		{name: "zero request timeout", opt: WithRequestTimeout(0), want: "request timeout must be positive"},
		{name: "zero open timeout", opt: WithOpenTimeout(0), want: "open timeout must be positive"},
		{name: "negative max upload", opt: WithMaxUploadSize(-1), want: "max upload size must be non-negative"},
		{name: "negative max download", opt: WithMaxDownloadSize(-1), want: "max download size must be non-negative"},
		{name: "nil runtime", opt: withRuntime(nil), want: "runtime is nil"},
	}
	for _, tt := range invalidOptions {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWorkspace(WithTemplateName("python-sandbox-template"), tt.opt)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewWorkspace error = %v, want containing %q", err, tt.want)
			}
		})
	}

	var nilWS *Workspace
	if nilWS.WorkspaceID() != "" || nilWS.IsAlive() {
		t.Fatalf("nil workspace identity/lifecycle mismatch")
	}
	if err := nilWS.Initialize(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil Initialize error = %v", err)
	}
	if err := nilWS.Close(context.Background()); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if err := nilWS.Reset(context.Background()); err != nil {
		t.Fatalf("nil Reset returned error: %v", err)
	}
	if _, err := nilWS.GetInstructions(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil GetInstructions error = %v", err)
	}
	if _, err := nilWS.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil ListTools error = %v", err)
	}
	if _, err := nilWS.ListMCPs(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil ListMCPs error = %v", err)
	}
	if _, err := nilWS.ListSkills(context.Background()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil ListSkills error = %v", err)
	}
	if _, err := nilWS.OffloadContext(context.Background(), "session", nil); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil OffloadContext error = %v", err)
	}
	if _, err := nilWS.OffloadToolResult(context.Background(), "session", nil); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil OffloadToolResult error = %v", err)
	}
	if _, err := nilWS.OffloadDataBlock(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil OffloadDataBlock error = %v", err)
	}
	if err := nilWS.AddMCP(context.Background(), newPersistedMCP("nil")); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil AddMCP error = %v", err)
	}
	if err := nilWS.RemoveMCP(context.Background(), "nil"); err != nil {
		t.Fatalf("nil RemoveMCP returned error: %v", err)
	}
	if err := nilWS.AddSkill(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "nil workspace") {
		t.Fatalf("nil AddSkill error = %v", err)
	}
	if err := nilWS.RemoveSkill(context.Background(), "skill"); err != nil {
		t.Fatalf("nil RemoveSkill returned error: %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	canceledWS := &Workspace{}
	if err := canceledWS.Initialize(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Initialize canceled error = %v", err)
	}
	if err := canceledWS.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close canceled error = %v", err)
	}
	if _, err := canceledWS.GetInstructions(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetInstructions canceled error = %v", err)
	}
	if _, err := canceledWS.ListTools(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListTools canceled error = %v", err)
	}
	if _, err := canceledWS.ListMCPs(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListMCPs canceled error = %v", err)
	}
	if _, err := canceledWS.ListSkills(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListSkills canceled error = %v", err)
	}
	if err := canceledWS.AddMCP(canceled, newPersistedMCP("canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("AddMCP canceled error = %v", err)
	}
	if err := canceledWS.RemoveMCP(canceled, "canceled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RemoveMCP canceled error = %v", err)
	}
}

func TestSandboxToolMetadataRulesAndHelperBranches(t *testing.T) {
	t.Parallel()

	uninitialized := newBashTool(&Workspace{handle: newFakeHandle("inactive")})
	if response := runTool(t, uninitialized, map[string]any{"command": "pwd"}, nil); response.State != message.ToolResultError || !strings.Contains(textOutput(response), "not initialized") {
		t.Fatalf("uninitialized tool response mismatch: state=%s content=%q", response.State, textOutput(response))
	}

	ws := initializedWorkspace(t, &fakeRuntime{handle: newFakeHandle("sandbox-tools")})
	readTool := findTool(t, ws, "Read")
	sandboxRead, ok := readTool.(*sandboxTool)
	if !ok {
		t.Fatalf("Read tool type = %T, want *sandboxTool", readTool)
	}
	if sandboxRead.Description() == "" || sandboxRead.InputSchema() == nil {
		t.Fatalf("sandbox tool metadata should be populated")
	}
	if !sandboxRead.IsReadOnly() || !sandboxRead.IsConcurrencySafe() || sandboxRead.IsExternalTool() || sandboxRead.IsStateInjected() || sandboxRead.IsMCP() || sandboxRead.MCPName() != "" {
		t.Fatalf("sandbox tool flags mismatch")
	}
	decision, err := sandboxRead.CheckPermissions(context.Background(), map[string]any{"file_path": "/tmp/readme.md"}, permission.NewContext(permission.ModeBypass))
	if err != nil {
		t.Fatalf("CheckPermissions returned error: %v", err)
	}
	if decision == nil || decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("CheckPermissions decision mismatch: %#v", decision)
	}
	if !sandboxRead.MatchRule("/tmp/*", map[string]any{"file_path": "/tmp/readme.md"}) {
		t.Fatalf("Read MatchRule should delegate to builtin rule matcher")
	}
	if len(sandboxRead.GenerateSuggestions(map[string]any{"file_path": "/tmp/readme.md"})) == 0 {
		t.Fatalf("GenerateSuggestions should delegate to builtin suggestions")
	}

	if got := cleanSandboxPath("app//work"); got != "/app/work" {
		t.Fatalf("cleanSandboxPath = %q", got)
	}
	if got := shellQuote("a'b"); got != `'a'"'"'b'` {
		t.Fatalf("shellQuote = %q", got)
	}
	if cloneStringMap(nil) != nil {
		t.Fatalf("cloneStringMap(nil) should return nil")
	}
	env := cloneStringMap(map[string]string{"KEY": "value"})
	env["KEY"] = "changed"
	if env["KEY"] == "value" {
		t.Fatalf("test mutation sanity check failed")
	}
	input := map[string]any{
		"int":    int(7),
		"int64":  int64(8),
		"float":  float64(9),
		"string": "10",
		"bad":    "nope",
	}
	if intValue(input, "int", 1) != 7 || intValue(input, "int64", 1) != 8 || intValue(input, "float", 1) != 9 || intValue(input, "string", 1) != 10 || intValue(input, "bad", 11) != 11 || intValue(input, "missing", 12) != 12 {
		t.Fatalf("intValue conversion mismatch")
	}
	if timeoutValue(map[string]any{"timeout_ms": 0}, "timeout_ms", time.Second, 5*time.Second) != time.Second {
		t.Fatalf("timeoutValue should use fallback for non-positive values")
	}
	if timeoutValue(map[string]any{"timeout_ms": 10_000}, "timeout_ms", time.Second, 5*time.Second) != 5*time.Second {
		t.Fatalf("timeoutValue should clamp to max")
	}
	if lines := splitLinesPreserve(""); len(lines) != 0 {
		t.Fatalf("splitLinesPreserve empty = %#v", lines)
	}
	if baseDir, err := globBaseDir(map[string]any{}, "/home/user"); err != nil || baseDir != "/home/user" {
		t.Fatalf("globBaseDir default = %q, %v", baseDir, err)
	}
	if _, err := globBaseDir(map[string]any{"path": "bad\x00path"}, "/home/user"); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("globBaseDir should reject NUL path, got %v", err)
	}
	if searchPath, err := grepSearchPath(map[string]any{"path": "src"}, "/home/user"); err != nil || searchPath != "/home/user/src" {
		t.Fatalf("grepSearchPath relative = %q, %v", searchPath, err)
	}
	if !matchGlob("**/*.go", "cmd/app/main.go") || !matchGlob("**/*.go", "main.go") || matchGlob("*.go", "cmd/app/main.go") {
		t.Fatalf("matchGlob recursive semantics mismatch")
	}
	if got := strings.Join(filterGlobMatches("/home/user", "/home/user/main.go\n/home/user/readme.md\n", "*.go"), ","); got != "/home/user/main.go" {
		t.Fatalf("filterGlobMatches = %q", got)
	}
	if got := strings.Join(filterGrepOutput("/home/user/a.go:1:main\n/home/user/a.go:2:main\n/home/user/b.txt:1:main\n", "*.go", "count"), ","); got != "/home/user/a.go:2" {
		t.Fatalf("filterGrepOutput count = %q", got)
	}
	if got := strings.Join(limitStrings([]string{"a", "b", "c"}, 2), ","); got != "a,b" {
		t.Fatalf("limitStrings = %q", got)
	}
}

func TestWorkspaceMCPMirrorSkillsAndResetBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	noHost := initializedWorkspace(t, &fakeRuntime{handle: newFakeHandle("sandbox-no-host")})
	if _, err := noHost.OffloadToolResult(ctx, "session", nil); err == nil || !strings.Contains(err.Error(), "requires WithHostWorkdir") {
		t.Fatalf("OffloadToolResult without host workdir error = %v", err)
	}
	if err := noHost.AddSkill(ctx, t.TempDir()); err == nil || !strings.Contains(err.Error(), "requires WithHostWorkdir") {
		t.Fatalf("AddSkill without host workdir error = %v", err)
	}
	if err := noHost.RemoveSkill(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "requires WithHostWorkdir") {
		t.Fatalf("RemoveSkill without host workdir error = %v", err)
	}
	skills, err := noHost.ListSkills(ctx)
	if err != nil || len(skills) != 0 {
		t.Fatalf("ListSkills without host workdir = %#v, %v", skills, err)
	}

	workdir := t.TempDir()
	ws, err := NewWorkspace(
		WithTemplateName("python-sandbox-template"),
		WithHostWorkdir(workdir),
		withRuntime(&fakeRuntime{handle: newFakeHandle("sandbox-host")}),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if err := ws.AddMCP(ctx, nil); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("AddMCP nil error = %v", err)
	}
	weather := newPersistedMCP("weather")
	if err := ws.AddMCP(ctx, weather); err != nil {
		t.Fatalf("AddMCP returned error: %v", err)
	}
	if err := ws.AddMCP(ctx, weather); err == nil || !strings.Contains(err.Error(), "duplicate MCP") {
		t.Fatalf("AddMCP duplicate error = %v", err)
	}
	if index := ws.findMCP("weather"); index != 0 {
		t.Fatalf("findMCP(weather) = %d", index)
	}
	if err := ws.RemoveMCP(ctx, " "); err == nil || !strings.Contains(err.Error(), "MCP name is empty") {
		t.Fatalf("RemoveMCP empty error = %v", err)
	}
	if err := ws.RemoveMCP(ctx, "missing"); err != nil {
		t.Fatalf("RemoveMCP missing returned error: %v", err)
	}
	if err := ws.RemoveMCP(ctx, "weather"); err != nil {
		t.Fatalf("RemoveMCP returned error: %v", err)
	}
	assertMCPFileNames(t, filepath.Join(workdir, ".mcp"))

	userMsg, err := message.NewUserMessage("user", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	contextPath, err := ws.OffloadContext(ctx, "session-1", []*message.Message{userMsg})
	if err != nil {
		t.Fatalf("OffloadContext returned error: %v", err)
	}
	if _, err := os.Stat(contextPath); err != nil {
		t.Fatalf("OffloadContext should write a local file at %q: %v", contextPath, err)
	}
	resultPath, err := ws.OffloadToolResult(ctx, "session-1", message.NewToolResultBlock("call-1", "Bash", message.ToolResultOutput{Raw: "ok"}, message.ToolResultSuccess))
	if err != nil {
		t.Fatalf("OffloadToolResult returned error: %v", err)
	}
	if _, err := os.Stat(resultPath); err != nil {
		t.Fatalf("OffloadToolResult should write a local file at %q: %v", resultPath, err)
	}

	sourceSkill := filepath.Join(t.TempDir(), "skill-source")
	writeAgentsandboxSkill(t, sourceSkill, "review", "Review code")
	if err := ws.AddSkill(ctx, sourceSkill); err != nil {
		t.Fatalf("AddSkill returned error: %v", err)
	}
	skills, err = ws.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills returned error: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "review" {
		t.Fatalf("ListSkills after AddSkill = %#v", skills)
	}
	if err := ws.RemoveSkill(ctx, "missing"); err != nil {
		t.Fatalf("RemoveSkill missing returned error: %v", err)
	}
	if err := ws.RemoveSkill(ctx, "review"); err != nil {
		t.Fatalf("RemoveSkill returned error: %v", err)
	}
	skills, err = ws.ListSkills(ctx)
	if err != nil || len(skills) != 0 {
		t.Fatalf("ListSkills after RemoveSkill = %#v, %v", skills, err)
	}

	if err := os.WriteFile(filepath.Join(workdir, ".mcp"), []byte(`[{"name":"manual","type":"http","http":{"url":"http://localhost/manual"}}]`), 0o600); err != nil {
		t.Fatalf("write .mcp returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "data", "payload.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatalf("write data returned error: %v", err)
	}
	if err := ws.Reset(ctx); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	for _, path := range []string{filepath.Join(workdir, ".mcp"), filepath.Join(workdir, "data"), filepath.Join(workdir, "skills"), filepath.Join(workdir, "sessions")} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("Reset should remove %s, statErr=%v", path, statErr)
		}
	}
}

func TestPersistedMCPInitializeAndCloseErrorBranches(t *testing.T) {
	t.Parallel()

	config := asworkspace.MCPClientConfig{
		Name:     "persisted",
		Type:     asworkspace.MCPClientTypeStdio,
		Stateful: true,
		Stdio: &asworkspace.MCPStdioConfig{
			Command: "server",
			Args:    []string{"--flag"},
			Env:     map[string]string{"TOKEN": "secret"},
		},
		HTTP: &asworkspace.MCPHTTPConfig{
			URL:     "http://localhost/mcp",
			Headers: map[string]string{"X-Test": "yes"},
		},
		EnabledTools:  []string{"allowed"},
		DisabledTools: []string{"blocked"},
	}
	persisted := &persistedMCPClient{config: config}
	if persisted.Name() != "persisted" || !persisted.IsStateful() || persisted.IsConnected() {
		t.Fatalf("persisted MCP identity/state mismatch")
	}
	if err := persisted.Connect(context.Background()); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if err := persisted.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	tools, err := persisted.ListTools(context.Background())
	if err != nil || len(tools) != 0 {
		t.Fatalf("ListTools = %#v, %v", tools, err)
	}
	cloned, err := persisted.MCPClientConfig()
	if err != nil {
		t.Fatalf("MCPClientConfig returned error: %v", err)
	}
	cloned.Stdio.Args[0] = "changed"
	cloned.Stdio.Env["TOKEN"] = "changed"
	cloned.HTTP.Headers["X-Test"] = "changed"
	cloned.EnabledTools[0] = "changed"
	if persisted.config.Stdio.Args[0] != "--flag" || persisted.config.Stdio.Env["TOKEN"] != "secret" || persisted.config.HTTP.Headers["X-Test"] != "yes" || persisted.config.EnabledTools[0] != "allowed" {
		t.Fatalf("MCPClientConfig should deep clone config, got %#v", persisted.config)
	}

	var nilPersisted *persistedMCPClient
	if nilPersisted.Name() != "" || nilPersisted.IsStateful() {
		t.Fatalf("nil persisted MCP identity mismatch")
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

	ctx := context.Background()
	if err := (&Workspace{runtime: errorSandboxRuntime{createErr: errors.New("create failed")}}).Initialize(ctx); err == nil || !strings.Contains(err.Error(), "create failed") {
		t.Fatalf("Initialize create error = %v", err)
	}
	if err := (&Workspace{runtime: errorSandboxRuntime{handle: errorSandboxHandle{readyErr: errors.New("ready failed")}}}).Initialize(ctx); err == nil || !strings.Contains(err.Error(), "ready failed") {
		t.Fatalf("Initialize ready error = %v", err)
	}
	if err := (&Workspace{runtime: errorSandboxRuntime{handle: errorSandboxHandle{id: "not-ready", ready: false}}}).Initialize(ctx); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("Initialize not-ready error = %v", err)
	}
	ws := &Workspace{
		handle:      errorSandboxHandle{id: "close", closeErr: errors.New("handle close failed")},
		runtime:     errorSandboxRuntime{closeErr: errors.New("runtime close failed")},
		ownsRuntime: true,
		alive:       true,
	}
	err = ws.Close(ctx)
	if err == nil || !strings.Contains(err.Error(), "handle close failed") || !strings.Contains(err.Error(), "runtime close failed") {
		t.Fatalf("Close joined error = %v", err)
	}
	if ws.IsAlive() || ws.handle != nil {
		t.Fatalf("Close should clear lifecycle state")
	}
	keepWS := &Workspace{
		handle:      errorSandboxHandle{id: "disconnect", disconnectErr: errors.New("disconnect failed")},
		keepSandbox: true,
		alive:       true,
	}
	if err := keepWS.Close(ctx); err == nil || !strings.Contains(err.Error(), "disconnect failed") {
		t.Fatalf("Close keep sandbox error = %v", err)
	}
}

func writeAgentsandboxSkill(t *testing.T, dir, name, description string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll skill returned error: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile skill returned error: %v", err)
	}
}

type nonConfigMCP struct {
	name string
}

func (m nonConfigMCP) Name() string { return m.name }

func (nonConfigMCP) IsStateful() bool { return false }

func (nonConfigMCP) IsConnected() bool { return false }

func (nonConfigMCP) Connect(context.Context) error { return nil }

func (nonConfigMCP) Close() error { return nil }

func (nonConfigMCP) ListTools(context.Context) ([]tool.Tool, error) { return nil, nil }

type errorSandboxRuntime struct {
	handle    sandboxHandle
	createErr error
	closeErr  error
}

func (r errorSandboxRuntime) Create(context.Context, sandboxSpec) (sandboxHandle, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	return r.handle, nil
}

func (r errorSandboxRuntime) Close() error {
	return r.closeErr
}

type errorSandboxHandle struct {
	id            string
	ready         bool
	readyErr      error
	runErr        error
	readErr       error
	writeErr      error
	closeErr      error
	disconnectErr error
}

func (h errorSandboxHandle) ID() string {
	return h.id
}

func (h errorSandboxHandle) IsReady(context.Context) (bool, error) {
	if h.readyErr != nil {
		return false, h.readyErr
	}
	return h.ready, nil
}

func (h errorSandboxHandle) Run(context.Context, runRequest) (runResult, error) {
	return runResult{}, h.runErr
}

func (h errorSandboxHandle) Read(context.Context, string) ([]byte, error) {
	return nil, h.readErr
}

func (h errorSandboxHandle) Write(context.Context, string, []byte) error {
	return h.writeErr
}

func (h errorSandboxHandle) Close(context.Context) error {
	return h.closeErr
}

func (h errorSandboxHandle) Disconnect(context.Context) error {
	return h.disconnectErr
}
