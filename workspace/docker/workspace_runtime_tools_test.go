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

func TestWorkspaceOptionsSpecNilAndContextBranches(t *testing.T) {
	host := t.TempDir()
	runtime := &fakeRuntime{}
	ws, err := NewWorkspace(
		nil,
		WithWorkspaceID(""),
		WithImage("ubuntu:22.04"),
		WithName(" agent-box "),
		WithContainerWorkdir("agent"),
		WithHostWorkdir(host),
		WithEnv("A", "1"),
		WithInstructions("workdir={workdir}"),
		WithKeepContainer(true),
		WithPullImage(true),
		WithStopTimeout(2*time.Second),
		WithNetworkDisabled(true),
		WithMemoryLimit(1024),
		WithNanoCPUs(2),
		withRuntime(runtime),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if ws.WorkspaceID() == "" || (*Workspace)(nil).WorkspaceID() != "" {
		t.Fatal("workspace IDs should be populated and nil-safe")
	}
	if ws.WorkspaceRoot() != "/agent" || (*Workspace)(nil).WorkspaceRoot() != "" {
		t.Fatalf("WorkspaceRoot mismatch: %q", ws.WorkspaceRoot())
	}
	spec := ws.containerSpec()
	if spec.Image != "ubuntu:22.04" || spec.Name != "agent-box" || spec.Workdir != "/agent" || spec.Env["A"] != "1" ||
		!spec.KeepContainer || !spec.PullImage || spec.StopTimeout != 2*time.Second || !spec.NetworkDisabled ||
		spec.MemoryBytes != 1024 || spec.NanoCPUs != 2 || spec.RemoveOnClose {
		t.Fatalf("container spec mismatch: %#v", spec)
	}
	spec.Env["A"] = "mutated"
	if ws.containerSpec().Env["A"] != "1" {
		t.Fatal("containerSpec should clone environment maps")
	}
	instructions, err := ws.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if instructions != "workdir=/agent" {
		t.Fatalf("instructions should substitute workdir, got %q", instructions)
	}

	invalidOptions := []Option{
		WithImage(" "),
		WithContainerWorkdir(" "),
		WithHostWorkdir(" "),
		WithEnv(" ", "x"),
		WithStopTimeout(-time.Second),
		WithMemoryLimit(-1),
		WithNanoCPUs(-1),
		WithMCPGateway(nil),
		withRuntime(nil),
	}
	for _, opt := range invalidOptions {
		if _, err := NewWorkspace(opt); err == nil {
			t.Fatalf("option should be rejected: %#v", opt)
		}
	}

	var nilWorkspace *Workspace
	if err := nilWorkspace.Initialize(context.Background()); err == nil {
		t.Fatal("nil Initialize should fail")
	}
	if err := nilWorkspace.Close(context.Background()); err != nil {
		t.Fatalf("nil Close should be a no-op: %v", err)
	}
	if err := nilWorkspace.Reset(context.Background()); err != nil {
		t.Fatalf("nil Reset should be a no-op: %v", err)
	}
	if _, err := nilWorkspace.GetInstructions(context.Background()); err == nil {
		t.Fatal("nil GetInstructions should fail")
	}
	if _, err := nilWorkspace.ListTools(context.Background()); err == nil {
		t.Fatal("nil ListTools should fail")
	}
	if _, err := nilWorkspace.ListMCPs(context.Background()); err == nil {
		t.Fatal("nil ListMCPs should fail")
	}
	if _, err := nilWorkspace.OffloadContext(context.Background(), "s", nil); err == nil {
		t.Fatal("nil OffloadContext should fail")
	}
	if _, err := nilWorkspace.OffloadToolResult(context.Background(), "s", nil); err == nil {
		t.Fatal("nil OffloadToolResult should fail")
	}
	if _, err := nilWorkspace.OffloadDataBlock(context.Background(), nil); err == nil {
		t.Fatal("nil OffloadDataBlock should fail")
	}
	if err := nilWorkspace.AddSkill(context.Background(), "x"); err == nil {
		t.Fatal("nil AddSkill should fail")
	}
	if err := nilWorkspace.RemoveSkill(context.Background(), "x"); err != nil {
		t.Fatalf("nil RemoveSkill should be a no-op: %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ws.Initialize(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Initialize should return context error: %v", err)
	}
	if _, err := ws.GetInstructions(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetInstructions should return context error: %v", err)
	}
	if _, err := ws.ListTools(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListTools should return context error: %v", err)
	}
	if _, err := ws.ListMCPs(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListMCPs should return context error: %v", err)
	}
	if err := ws.AddMCP(canceled, newPersistedMCP("weather")); !errors.Is(err, context.Canceled) {
		t.Fatalf("AddMCP should return context error: %v", err)
	}
	if err := ws.RemoveMCP(canceled, "weather"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RemoveMCP should return context error: %v", err)
	}
}

func TestWorkspaceLifecycleMCPAndGatewayErrorBranches(t *testing.T) {
	ctx := context.Background()

	createErr := errors.New("create failed")
	createWS, err := NewWorkspace(withRuntime(&fakeRuntime{createErr: createErr}))
	if err != nil {
		t.Fatalf("NewWorkspace create error branch returned error: %v", err)
	}
	if err := createWS.Initialize(ctx); !errors.Is(err, createErr) {
		t.Fatalf("Initialize create error = %v", err)
	}

	startErr := errors.New("start failed")
	startWS, err := NewWorkspace(withRuntime(&fakeRuntime{startErr: startErr}))
	if err != nil {
		t.Fatalf("NewWorkspace start error branch returned error: %v", err)
	}
	if err := startWS.Initialize(ctx); !errors.Is(err, startErr) {
		t.Fatalf("Initialize start error = %v", err)
	}

	gatewayErr := errors.New("gateway failed")
	gatewayWS, err := NewWorkspace(withRuntime(&fakeRuntime{}), WithMCPGateway(&fakeGateway{bootstrapErr: gatewayErr, configs: map[string]asworkspace.MCPClientConfig{}}))
	if err != nil {
		t.Fatalf("NewWorkspace gateway error branch returned error: %v", err)
	}
	if err := gatewayWS.Initialize(ctx); !errors.Is(err, gatewayErr) {
		t.Fatalf("Initialize gateway error = %v", err)
	}

	ws, err := NewWorkspace(WithHostWorkdir(t.TempDir()), withRuntime(&fakeRuntime{}), WithMCPGateway(newFakeGateway()))
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize should be idempotent: %v", err)
	}
	if err := ws.AddMCP(ctx, nil); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("AddMCP nil error = %v", err)
	}
	if err := ws.AddMCP(ctx, nonConfigDockerMCP{name: "runtime"}); err == nil || !strings.Contains(err.Error(), "cannot be persisted") {
		t.Fatalf("AddMCP non-config error = %v", err)
	}
	news := newPersistedMCP("news")
	if err := ws.AddMCP(ctx, news); err != nil {
		t.Fatalf("AddMCP news returned error: %v", err)
	}
	if err := ws.AddMCP(ctx, news); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("AddMCP duplicate error = %v", err)
	}
	if err := ws.RemoveMCP(ctx, " "); err == nil || !strings.Contains(err.Error(), "MCP name is empty") {
		t.Fatalf("RemoveMCP empty error = %v", err)
	}
	if err := ws.RemoveMCP(ctx, "missing"); err != nil {
		t.Fatalf("RemoveMCP missing should be a no-op: %v", err)
	}

	addErrGateway := newFakeGateway()
	addErrGateway.addErr = gatewayErr
	addErrWS, err := NewWorkspace(WithHostWorkdir(t.TempDir()), withRuntime(&fakeRuntime{}), WithMCPGateway(addErrGateway))
	if err != nil {
		t.Fatalf("NewWorkspace add error branch returned error: %v", err)
	}
	if err := addErrWS.Initialize(ctx); err != nil {
		t.Fatalf("Initialize add error branch returned error: %v", err)
	}
	if err := addErrWS.AddMCP(ctx, newPersistedMCP("bad")); !errors.Is(err, gatewayErr) {
		t.Fatalf("AddMCP gateway error = %v", err)
	}

	removeErrGateway := newFakeGateway()
	removeErrGateway.removeErr = gatewayErr
	removeErrWS, err := NewWorkspace(WithHostWorkdir(t.TempDir()), withRuntime(&fakeRuntime{}), WithMCPGateway(removeErrGateway), WithMCPs(newPersistedMCP("bad")))
	if err != nil {
		t.Fatalf("NewWorkspace remove error branch returned error: %v", err)
	}
	if err := removeErrWS.Initialize(ctx); err != nil {
		t.Fatalf("Initialize remove error branch returned error: %v", err)
	}
	if err := removeErrWS.RemoveMCP(ctx, "bad"); !errors.Is(err, gatewayErr) {
		t.Fatalf("RemoveMCP gateway error = %v", err)
	}

	closeGateway := newFakeGateway()
	closeGateway.closeErr = errors.New("gateway close failed")
	closeRuntime := &fakeRuntime{stopErr: errors.New("stop failed"), removeErr: errors.New("remove failed"), closeErr: errors.New("runtime close failed")}
	closeWS, err := NewWorkspace(withRuntime(closeRuntime), WithMCPGateway(closeGateway))
	if err != nil {
		t.Fatalf("NewWorkspace close error branch returned error: %v", err)
	}
	if err := closeWS.Initialize(ctx); err != nil {
		t.Fatalf("Initialize close error branch returned error: %v", err)
	}
	err = closeWS.Close(ctx)
	if err == nil || !strings.Contains(err.Error(), "gateway close failed") || !strings.Contains(err.Error(), "stop failed") || !strings.Contains(err.Error(), "remove failed") || !strings.Contains(err.Error(), "runtime close failed") {
		t.Fatalf("Close should join cleanup errors, got %v", err)
	}

	if _, err := mcpConfig(nil); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("mcpConfig nil error = %v", err)
	}
	providerErr := errors.New("config failed")
	if _, err := mcpConfig(errorConfigDockerMCP{name: "bad", err: providerErr}); !errors.Is(err, providerErr) {
		t.Fatalf("mcpConfig provider error = %v", err)
	}
	if cloneStringMap(nil) == nil {
		t.Fatalf("docker cloneStringMap returns an empty map for nil input")
	}
}

func TestDockerToolMetadataRulesAndGlobGrepBranches(t *testing.T) {
	uninitialized := &Workspace{runtime: &fakeRuntime{}, containerWorkdir: defaultContainerWorkdir}
	uninitializedResp := runTool(t, newBashTool(uninitialized), map[string]any{"command": "pwd"}, nil)
	if uninitializedResp.State != message.ToolResultError || !strings.Contains(textOutput(uninitializedResp), "not initialized") {
		t.Fatalf("uninitialized workspace should return error chunk: %#v", uninitializedResp)
	}

	ws := initializedWorkspace(t, &fakeRuntime{})
	read := findTool(t, ws, "Read")
	if read.Description() == "" || read.InputSchema()["type"] != "object" || !read.IsConcurrencySafe() || !read.IsReadOnly() ||
		read.IsExternalTool() || read.IsStateInjected() || read.IsMCP() || read.MCPName() != "" {
		t.Fatalf("docker tool metadata mismatch: %#v", read)
	}
	decision, err := read.CheckPermissions(context.Background(), map[string]any{"file_path": "/workspace/a.go"}, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("Read CheckPermissions mismatch: %#v err=%v", decision, err)
	}
	if !read.MatchRule("/workspace/**", map[string]any{"file_path": "/workspace/a.go"}) {
		t.Fatal("Read MatchRule should delegate to built-in tool")
	}
	if got := read.GenerateSuggestions(map[string]any{"file_path": "/workspace/a.go"}); len(got) != 1 || got[0].ToolName != "Read" {
		t.Fatalf("Read suggestions mismatch: %#v", got)
	}

	globRuntime := &fakeRuntime{runResult: runResult{Stdout: "/workspace/a.go\n/workspace/sub/b.txt\n", ExitCode: 0}}
	globWS := initializedWorkspace(t, globRuntime)
	globResp := runTool(t, findTool(t, globWS, "Glob"), map[string]any{"pattern": "*.go", "path": "/workspace"}, nil)
	if globResp.State != message.ToolResultSuccess || !strings.Contains(textOutput(globResp), "/workspace/a.go") || strings.Contains(textOutput(globResp), "b.txt") {
		t.Fatalf("Glob success mismatch: %#v", globResp)
	}
	noGlobResp := runTool(t, findTool(t, globWS, "Glob"), map[string]any{"pattern": "*.md", "path": "/workspace"}, nil)
	if noGlobResp.State != message.ToolResultSuccess || !strings.Contains(textOutput(noGlobResp), "No files") {
		t.Fatalf("Glob no-match mismatch: %#v", noGlobResp)
	}
	globRuntime.runResult = runResult{Stderr: "find failed", ExitCode: 2}
	globErr := runTool(t, findTool(t, globWS, "Glob"), map[string]any{"pattern": "*.go", "path": "/workspace"}, nil)
	if globErr.State != message.ToolResultError || !strings.Contains(textOutput(globErr), "find failed") {
		t.Fatalf("Glob runtime error mismatch: %#v", globErr)
	}
	missingPattern := runTool(t, findTool(t, globWS, "Glob"), map[string]any{"pattern": ""}, nil)
	if missingPattern.State != message.ToolResultError {
		t.Fatalf("Glob missing pattern mismatch: %#v", missingPattern)
	}

	grepRuntime := &fakeRuntime{runResult: runResult{Stdout: "/workspace/a.go:1:Needle\n/workspace/b.txt:2:needle\n/workspace/a.go:3:Needle\n", ExitCode: 0}}
	grepWS := initializedWorkspace(t, grepRuntime)
	countResp := runTool(t, findTool(t, grepWS, "Grep"), map[string]any{"pattern": "Needle", "path": "/workspace", "glob": "*.go", "output_mode": "count"}, nil)
	if countResp.State != message.ToolResultSuccess || strings.TrimSpace(textOutput(countResp)) != "/workspace/a.go:2" {
		t.Fatalf("Grep count mismatch: %q", textOutput(countResp))
	}
	files := filterGrepOutput(grepRuntime.runResult.Stdout, "", "files")
	if len(files) != 2 || files[0] != "/workspace/a.go" {
		t.Fatalf("filterGrepOutput files mismatch: %#v", files)
	}
	if limited := limitStrings([]string{"a", "b", "c"}, 2); len(limited) != 2 {
		t.Fatalf("limitStrings mismatch: %#v", limited)
	}
	grepRuntime.runResult = runResult{ExitCode: 1}
	grepNoMatch := runTool(t, findTool(t, grepWS, "Grep"), map[string]any{"pattern": "Needle"}, nil)
	if grepNoMatch.State != message.ToolResultSuccess || !strings.Contains(textOutput(grepNoMatch), "No matches") {
		t.Fatalf("Grep no match mismatch: %#v", grepNoMatch)
	}
	grepRuntime.runResult = runResult{Stderr: "grep failed", ExitCode: 2}
	grepErr := runTool(t, findTool(t, grepWS, "Grep"), map[string]any{"pattern": "Needle"}, nil)
	if grepErr.State != message.ToolResultError || !strings.Contains(textOutput(grepErr), "grep failed") {
		t.Fatalf("Grep runtime error mismatch: %#v", grepErr)
	}
	invalidRegex := runTool(t, findTool(t, grepWS, "Grep"), map[string]any{"pattern": "["}, nil)
	if invalidRegex.State != message.ToolResultError || !strings.Contains(textOutput(invalidRegex), "invalid regex") {
		t.Fatalf("Grep invalid regex mismatch: %#v", invalidRegex)
	}

	if got := shellQuote("a'b"); got != "'a'\"'\"'b'" {
		t.Fatalf("shellQuote mismatch: %q", got)
	}
	if matches := filterGlobMatches("/workspace", "/workspace/a.go\n\n/workspace/sub/b.txt\n", "**/*.txt"); len(matches) != 1 || matches[0] != "/workspace/sub/b.txt" {
		t.Fatalf("filterGlobMatches mismatch: %#v", matches)
	}
	if !matchGlob("**/*.go", "sub/a.go") || !matchGlob("*.go", "a.go") || matchGlob("*.go", "a.txt") {
		t.Fatal("matchGlob mismatch")
	}
	if _, err := requireContainerPath(map[string]any{}, "file_path"); err == nil {
		t.Fatal("missing container path should fail")
	}
	if _, err := requireContainerPath(map[string]any{"file_path": "relative"}, "file_path"); err == nil {
		t.Fatal("relative container path should fail")
	}
}

func TestWorkspaceLocalMirrorOffloadSkillsAndReset(t *testing.T) {
	ctx := context.Background()
	host := t.TempDir()
	ws, err := NewWorkspace(WithHostWorkdir(host), withRuntime(&fakeRuntime{}))
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	user, err := message.NewUserMessage("user", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	if ref, err := ws.OffloadContext(ctx, "session-1", []*message.Message{user}); err != nil || ref == "" {
		t.Fatalf("OffloadContext mismatch: ref=%q err=%v", ref, err)
	}
	result := message.NewToolResultBlock("call-1", "Read", message.ToolResultOutput{Raw: "raw output"}, message.ToolResultSuccess)
	if ref, err := ws.OffloadToolResult(ctx, "session-1", result); err != nil || ref == "" {
		t.Fatalf("OffloadToolResult mismatch: ref=%q err=%v", ref, err)
	}
	dataBlock := message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"))
	offloaded, err := ws.OffloadDataBlock(ctx, dataBlock)
	if err != nil {
		t.Fatalf("OffloadDataBlock returned error: %v", err)
	}
	if _, ok := offloaded.Source.(*message.URLSource); !ok {
		t.Fatalf("OffloadDataBlock should return URL source, got %#v", offloaded.Source)
	}

	sourceSkill := filepath.Join(t.TempDir(), "planner")
	if err := os.MkdirAll(sourceSkill, 0o700); err != nil {
		t.Fatalf("mkdir skill source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkill, "SKILL.md"), []byte("---\nname: planner\ndescription: Plan work\n---\nUse a plan.\n"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := ws.AddSkill(ctx, sourceSkill); err != nil {
		t.Fatalf("AddSkill returned error: %v", err)
	}
	skills, err := ws.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills returned error: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "planner" {
		t.Fatalf("ListSkills mismatch: %#v", skills)
	}
	if err := ws.RemoveSkill(ctx, "planner"); err != nil {
		t.Fatalf("RemoveSkill returned error: %v", err)
	}

	for _, dir := range []string{"data", "skills", "sessions"} {
		if err := os.MkdirAll(filepath.Join(host, dir), 0o700); err != nil {
			t.Fatalf("mkdir reset dir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(host, ".mcp"), []byte("[]"), 0o600); err != nil {
		t.Fatalf("write .mcp: %v", err)
	}
	if err := ws.Reset(ctx); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	for _, path := range []string{filepath.Join(host, "data"), filepath.Join(host, "skills"), filepath.Join(host, "sessions"), filepath.Join(host, ".mcp")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("Reset should remove %s, stat err=%v", path, err)
		}
	}

	noHost, err := NewWorkspace(withRuntime(&fakeRuntime{}))
	if err != nil {
		t.Fatalf("NewWorkspace without host returned error: %v", err)
	}
	if _, err := noHost.OffloadContext(ctx, "session-1", nil); err == nil {
		t.Fatal("OffloadContext without host workdir should fail")
	}
	if _, err := noHost.OffloadToolResult(ctx, "session-1", result); err == nil {
		t.Fatal("OffloadToolResult without host workdir should fail")
	}
	if _, err := noHost.OffloadDataBlock(ctx, dataBlock); err == nil {
		t.Fatal("OffloadDataBlock without host workdir should fail")
	}
	if err := noHost.AddSkill(ctx, sourceSkill); err == nil {
		t.Fatal("AddSkill without host workdir should fail")
	}
	if err := noHost.RemoveSkill(ctx, "planner"); err == nil {
		t.Fatal("RemoveSkill without host workdir should fail")
	}
	if skills, err := noHost.ListSkills(ctx); err != nil || len(skills) != 0 {
		t.Fatalf("ListSkills without host should be empty: %#v err=%v", skills, err)
	}
}

func TestMCPConfigPersistenceAndCloseErrorBranches(t *testing.T) {
	config := asworkspace.MCPClientConfig{
		Name:             "persisted",
		Type:             asworkspace.MCPClientTypeStdio,
		Stateful:         true,
		Stdio:            &asworkspace.MCPStdioConfig{Command: "cmd", Args: []string{"a"}, Env: map[string]string{"A": "1"}},
		HTTP:             &asworkspace.MCPHTTPConfig{URL: "https://example.invalid", Headers: map[string]string{"H": "v"}},
		EnabledTools:     []string{"read"},
		DisabledTools:    []string{"write"},
		ExecutionTimeout: time.Second,
	}
	persisted := &persistedMCPClient{config: config}
	if persisted.Name() != "persisted" || !persisted.IsStateful() || persisted.IsConnected() {
		t.Fatalf("persisted MCP metadata mismatch")
	}
	if err := persisted.Connect(context.Background()); err == nil {
		t.Fatal("persisted MCP Connect should fail without gateway")
	}
	if err := persisted.Close(); err != nil {
		t.Fatalf("persisted MCP Close should be a no-op: %v", err)
	}
	if _, err := persisted.ListTools(context.Background()); err == nil {
		t.Fatal("persisted MCP ListTools should fail without gateway")
	}
	cloned, err := persisted.MCPClientConfig()
	if err != nil {
		t.Fatalf("MCPClientConfig returned error: %v", err)
	}
	cloned.Stdio.Args[0] = "mutated"
	cloned.Stdio.Env["A"] = "mutated"
	cloned.HTTP.Headers["H"] = "mutated"
	cloned.EnabledTools[0] = "mutated"
	again, err := persisted.MCPClientConfig()
	if err != nil {
		t.Fatalf("second MCPClientConfig returned error: %v", err)
	}
	if again.Stdio.Args[0] != "a" || again.Stdio.Env["A"] != "1" || again.HTTP.Headers["H"] != "v" || again.EnabledTools[0] != "read" {
		t.Fatalf("MCP config should be deeply cloned: %#v", again)
	}
	if _, err := mcpConfig(nil); err == nil {
		t.Fatal("nil MCP config should fail")
	}
	if _, err := mcpConfig(nonConfigMCP{name: "runtime-only"}); err == nil {
		t.Fatal("runtime-only MCP config should fail")
	}
	if got := cloneStringMap(nil); got == nil {
		t.Fatal("cloneStringMap(nil) returns an empty map in this package")
	}
	if cleanContainerPath("app//work") != "/app/work" || cleanContainerPath(" ") != "" {
		t.Fatal("cleanContainerPath mismatch")
	}
	if durationSecondsPtr(500*time.Millisecond) == nil || *durationSecondsPtr(500 * time.Millisecond) != 1 || durationSecondsPtr(0) != nil {
		t.Fatal("durationSecondsPtr mismatch")
	}
	if envList(nil) != nil || len(envList(map[string]string{"A": "1"})) != 1 {
		t.Fatal("engine envList mismatch")
	}
	labeled := labels(containerSpec{ID: "id", ExtraLabels: map[string]string{"extra": "value"}})
	if labeled["agentscope-go.workspace.id"] != "id" || labeled["extra"] != "value" {
		t.Fatalf("labels mismatch: %#v", labeled)
	}
	if err := errorsJoin([]error{nil}); err != nil {
		t.Fatalf("errorsJoin nil-only should be nil: %v", err)
	}
	if err := errorsJoin([]error{errors.New("one"), nil, errors.New("two")}); err == nil || !strings.Contains(err.Error(), "one") || !strings.Contains(err.Error(), "two") {
		t.Fatalf("errorsJoin mismatch: %v", err)
	}

	ws := &Workspace{
		containerID: "container-1",
		runtime:     errorRuntime{},
		ownsRuntime: true,
		mcpGateway:  errorGateway{},
		alive:       true,
	}
	err = ws.Close(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gateway close") || !strings.Contains(err.Error(), "stop") || !strings.Contains(err.Error(), "remove") || !strings.Contains(err.Error(), "runtime close") {
		t.Fatalf("Close should join cleanup errors, got %v", err)
	}
	if ws.IsAlive() {
		t.Fatal("Close should mark workspace not alive even when cleanup fails")
	}
}

type nonConfigMCP struct {
	name string
}

func (m nonConfigMCP) Name() string                                   { return m.name }
func (m nonConfigMCP) IsStateful() bool                               { return false }
func (m nonConfigMCP) IsConnected() bool                              { return false }
func (m nonConfigMCP) Connect(context.Context) error                  { return nil }
func (m nonConfigMCP) Close() error                                   { return nil }
func (m nonConfigMCP) ListTools(context.Context) ([]tool.Tool, error) { return nil, nil }

type errorRuntime struct{}

func (errorRuntime) Create(context.Context, containerSpec) (string, error) {
	return "", errors.New("create")
}
func (errorRuntime) Start(context.Context, string) error  { return errors.New("start") }
func (errorRuntime) Stop(context.Context, string) error   { return errors.New("stop") }
func (errorRuntime) Remove(context.Context, string) error { return errors.New("remove") }
func (errorRuntime) Run(context.Context, string, runRequest) (runResult, error) {
	return runResult{}, errors.New("run")
}

func (errorRuntime) ReadFile(context.Context, string, string) ([]byte, error) {
	return nil, errors.New("read")
}

func (errorRuntime) WriteFile(context.Context, string, string, []byte, int64) error {
	return errors.New("write")
}
func (errorRuntime) Close() error { return errors.New("runtime close") }

type errorGateway struct{}

func (errorGateway) Bootstrap(context.Context) error { return errors.New("bootstrap") }
func (errorGateway) AddMCP(context.Context, asworkspace.MCPClientConfig) error {
	return errors.New("add")
}
func (errorGateway) RemoveMCP(context.Context, string) error { return errors.New("remove gateway") }
func (errorGateway) ListMCPs(context.Context) ([]asworkspace.MCPClientConfig, error) {
	return nil, errors.New("list")
}

func (errorGateway) NewMCPClient(asworkspace.MCPClientConfig, bool) asworkspace.MCPClient {
	return nil
}
func (errorGateway) Close(context.Context) error { return errors.New("gateway close") }
