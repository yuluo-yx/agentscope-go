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

package opensandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
	"github.com/yuluo-yx/agentscope-go/pkg/workspace/internal/sandboxed"
)

type testMCPClient struct {
	config workspace.MCPClientConfig
	tools  []tool.Tool
}

func (c *testMCPClient) Name() string { return c.config.Name }

func (c *testMCPClient) IsStateful() bool { return c.config.Stateful }

func (*testMCPClient) IsConnected() bool { return false }

func (*testMCPClient) Connect(context.Context) error { return nil }

func (*testMCPClient) Close() error { return nil }

func (c *testMCPClient) ListTools(context.Context) ([]tool.Tool, error) {
	return append([]tool.Tool(nil), c.tools...), nil
}

func (c *testMCPClient) MCPClientConfig() (workspace.MCPClientConfig, error) {
	return c.config, nil
}

func TestOptionsPopulateConfigAndCloneMutableInputs(t *testing.T) {
	runtime := &fakeSandboxRuntime{}
	limits := ResourceLimits{"cpu": "500m", "memory": "512Mi"}
	metadata := map[string]string{"team": "agentscope"}
	policy := &NetworkPolicy{
		DefaultAction: "deny",
		Egress:        []NetworkRule{{Action: "allow", Target: "example.com"}},
	}
	entrypoint := []string{"python3", "-m", "service"}
	mcp := &testMCPClient{config: workspace.MCPClientConfig{Name: "seed"}}
	config := defaultConfig()
	options := []Option{
		WithWorkspaceID(" stable-id "),
		WithImage(" python:3.12-slim "),
		WithAPIKey(" key "),
		WithDomain(" sandbox.example.test "),
		WithProtocol(" HTTPS "),
		WithRequestTimeout(11 * time.Second),
		WithTimeout(12 * time.Minute),
		WithGatewayPort(6600),
		WithEnv("TOKEN", "secret"),
		WithSandboxMetadata(metadata),
		WithResourceLimits(limits),
		WithEntrypoint(entrypoint...),
		WithNetworkPolicy(policy),
		WithExtraPythonPackages("httpx==0.28.1", " pydantic "),
		WithInstructions("run in {workdir}"),
		WithMCPs(mcp),
		WithSkillPaths("/tmp/skill"),
		withRuntime(runtime),
	}
	for _, option := range options {
		if err := option(&config); err != nil {
			t.Fatalf("option returned error: %v", err)
		}
	}

	limits["cpu"] = "9"
	metadata["team"] = "changed"
	policy.Egress[0].Target = "changed.example"
	entrypoint[0] = "changed"
	if config.id != "stable-id" || config.image != "python:3.12-slim" ||
		config.apiKey != "key" || config.domain != "sandbox.example.test" || config.protocol != "https" {
		t.Fatalf("string options mismatch: %#v", config)
	}
	if config.requestTimeout != 11*time.Second || config.sandboxTimeout != 12*time.Minute || config.gatewayPort != 6600 {
		t.Fatalf("timeout or port options mismatch: %#v", config)
	}
	if config.env["TOKEN"] != "secret" || config.metadata["team"] != "agentscope" {
		t.Fatalf("map options mismatch: env=%#v metadata=%#v", config.env, config.metadata)
	}
	if config.resourceLimits["cpu"] != "500m" || config.entrypoint[0] != "python3" ||
		config.networkPolicy.Egress[0].Target != "example.com" {
		t.Fatalf("mutable option values were not cloned: %#v", config)
	}
	if !reflect.DeepEqual(config.extraPythonPackages, []string{"httpx==0.28.1", "pydantic"}) ||
		config.instructions != "run in {workdir}" || config.runtime != runtime {
		t.Fatalf("remaining options mismatch: %#v", config)
	}
	if !reflect.DeepEqual(config.defaultMCPs, []workspace.MCPClient{mcp}) ||
		!reflect.DeepEqual(config.skillPaths, []string{"/tmp/skill"}) {
		t.Fatalf("seed options mismatch: MCPs=%#v skills=%#v", config.defaultMCPs, config.skillPaths)
	}
}

func TestNewWorkspaceConstructsProviderSpec(t *testing.T) {
	defaultWorkspace, err := NewWorkspace()
	if err != nil {
		t.Fatalf("default NewWorkspace returned error: %v", err)
	}
	if _, ok := defaultWorkspace.provider.runtime.(*sdkRuntime); !ok {
		t.Fatalf("default runtime = %T, want *sdkRuntime", defaultWorkspace.provider.runtime)
	}

	runtime := &fakeSandboxRuntime{}
	metadata := map[string]string{metadataWorkspaceID: "caller-value", "owner": "test"}
	workspaceInstance, err := New(
		WithWorkspaceID("workspace-id"),
		WithImage("custom-image"),
		WithDomain("sandbox.example.test"),
		WithProtocol("https"),
		WithAPIKey("api-key"),
		WithRequestTimeout(20*time.Second),
		WithTimeout(7*time.Minute),
		WithEnv("MODE", "test"),
		WithSandboxMetadata(metadata),
		WithResourceLimits(ResourceLimits{"cpu": "1"}),
		WithEntrypoint("sleep", "infinity"),
		WithNetworkPolicy(&NetworkPolicy{DefaultAction: "deny"}),
		WithExtraPythonPackages("httpx==0.28.1"),
		withRuntime(runtime),
	)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if workspaceInstance.WorkspaceID() != "workspace-id" || workspaceInstance.WorkspaceRoot() != defaultWorkdir ||
		workspaceInstance.SandboxID() != "" || workspaceInstance.IsAlive() {
		t.Fatalf("new workspace state mismatch: %#v", workspaceInstance)
	}
	spec := workspaceInstance.provider.spec
	if spec.ID != "workspace-id" || spec.Image != "custom-image" || spec.Timeout != 7*time.Minute {
		t.Fatalf("provider spec mismatch: %#v", spec)
	}
	if spec.Connection.Domain != "sandbox.example.test" || spec.Connection.Protocol != "https" ||
		spec.Connection.APIKey != "api-key" || spec.Connection.RequestTimeout != 20*time.Second {
		t.Fatalf("connection config mismatch: %#v", spec.Connection)
	}
	if spec.Metadata[metadataWorkspaceID] != "workspace-id" || spec.Metadata["owner"] != "test" {
		t.Fatalf("reserved metadata was not enforced: %#v", spec.Metadata)
	}
	if spec.Env["MODE"] != "test" || spec.ResourceLimits["cpu"] != "1" ||
		!reflect.DeepEqual(spec.Entrypoint, []string{"sleep", "infinity"}) || spec.NetworkPolicy.DefaultAction != "deny" {
		t.Fatalf("create options mismatch: %#v", spec)
	}
	requirements := bootstrapCommands([]string{"httpx==0.28.1"})[5]
	if !slices.Contains(requirements, pythonMCP) ||
		!slices.Contains(requirements, "httpx==0.28.1") ||
		requirements[len(requirements)-1] != "httpx==0.28.1" {
		t.Fatalf("bootstrap requirements mismatch: %#v", requirements)
	}
	agentScopeInstall := bootstrapCommands(nil)[6]
	if !slices.Contains(agentScopeInstall, pythonAgentScope) || slices.Contains(agentScopeInstall, "--no-deps") {
		t.Fatalf("AgentScope install must resolve declared dependencies: %#v", agentScopeInstall)
	}
}

func TestOptionsRejectInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		option Option
	}{
		{name: "empty workspace id", option: WithWorkspaceID(" ")},
		{name: "empty image", option: WithImage(" ")},
		{name: "invalid protocol", option: WithProtocol("ftp")},
		{name: "zero request timeout", option: WithRequestTimeout(0)},
		{name: "negative sandbox timeout", option: WithTimeout(-time.Second)},
		{name: "zero gateway port", option: WithGatewayPort(0)},
		{name: "oversized gateway port", option: WithGatewayPort(65536)},
		{name: "invalid environment name", option: WithEnv("BAD-NAME", "value")},
		{name: "nul environment value", option: WithEnv("GOOD", "bad\x00value")},
		{name: "empty metadata key", option: WithSandboxMetadata(map[string]string{" ": "value"})},
		{name: "nul metadata key", option: WithSandboxMetadata(map[string]string{"bad\x00key": "value"})},
		{name: "nul metadata value", option: WithSandboxMetadata(map[string]string{"key": "bad\x00value"})},
		{name: "nul entrypoint", option: WithEntrypoint("bad\x00argument")},
		{name: "empty python requirement", option: WithExtraPythonPackages(" ")},
		{name: "python option injection", option: WithExtraPythonPackages("--index-url")},
		{name: "nul python requirement", option: WithExtraPythonPackages("bad\x00requirement")},
		{name: "empty instructions", option: WithInstructions(" ")},
		{name: "nil MCP", option: WithMCPs(nil)},
		{name: "empty skill path", option: WithSkillPaths(" ")},
		{name: "nil runtime", option: withRuntime(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewWorkspace(tt.option); err == nil {
				t.Fatal("NewWorkspace should return an error")
			}
		})
	}
}

func TestCloneHelpersAndValidation(t *testing.T) {
	if cloneResourceLimits(nil) != nil || cloneNetworkPolicy(nil) != nil || cloneStringMap(nil) != nil {
		t.Fatal("nil clone inputs should remain nil")
	}
	limits := ResourceLimits{"cpu": "1"}
	limitsClone := cloneResourceLimits(limits)
	limitsClone["cpu"] = "2"
	if limits["cpu"] != "1" {
		t.Fatal("resource limits clone aliases the source")
	}
	policy := &NetworkPolicy{Egress: []NetworkRule{{Action: "allow", Target: "example.com"}}}
	policyClone := cloneNetworkPolicy(policy)
	policyClone.Egress[0].Target = "changed"
	if policy.Egress[0].Target != "example.com" {
		t.Fatal("network policy clone aliases the source")
	}
	values := map[string]string{"key": "value"}
	valuesClone := cloneStringMap(values)
	valuesClone["key"] = "changed"
	if values["key"] != "value" {
		t.Fatal("string map clone aliases the source")
	}

	valid := defaultConfig()
	if err := validateConfig(valid); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
	invalid := []config{
		func() config { value := valid; value.id = ""; return value }(),
		func() config { value := valid; value.image = ""; return value }(),
		func() config { value := valid; value.protocol = "ftp"; return value }(),
		func() config { value := valid; value.requestTimeout = 0; return value }(),
		func() config { value := valid; value.sandboxTimeout = 0; return value }(),
		func() config { value := valid; value.gatewayPort = 0; return value }(),
	}
	for index, value := range invalid {
		if err := validateConfig(value); err == nil {
			t.Fatalf("invalid config %d should return an error", index)
		}
	}
}

type resetBackend struct {
	files       map[string][]byte
	execCalls   [][]string
	deleted     []string
	mkdirCalls  [][]string
	writeCalls  []string
	readCalls   []string
	defaultExit int
}

func (b *resetBackend) Exec(
	_ context.Context,
	argv []string,
	_ sandboxed.ExecOptions,
) (sandboxed.ExecResult, error) {
	b.execCalls = append(b.execCalls, append([]string(nil), argv...))
	if len(argv) >= 3 && argv[0] == "test" && argv[1] == "-f" {
		if _, exists := b.files[argv[2]]; exists {
			return sandboxed.ExecResult{}, nil
		}
		return sandboxed.ExecResult{ExitCode: 1}, nil
	}
	if len(argv) >= 3 && argv[0] == "mkdir" && argv[1] == "-p" {
		b.mkdirCalls = append(b.mkdirCalls, append([]string(nil), argv[2:]...))
		return sandboxed.ExecResult{}, nil
	}
	if len(argv) >= 5 && argv[0] == "sh" && argv[1] == "-c" && strings.Contains(argv[2], "find") {
		for _, target := range argv[4:] {
			b.deleted = append(b.deleted, target)
			for filename := range b.files {
				if filename == target || strings.HasPrefix(filename, strings.TrimRight(target, "/")+"/") {
					delete(b.files, filename)
				}
			}
		}
		return sandboxed.ExecResult{}, nil
	}
	return sandboxed.ExecResult{ExitCode: b.defaultExit}, nil
}

func (b *resetBackend) ReadFile(_ context.Context, filename string) ([]byte, error) {
	b.readCalls = append(b.readCalls, filename)
	data, exists := b.files[filename]
	if !exists {
		return nil, fmt.Errorf("file %q not found", filename)
	}
	return append([]byte(nil), data...), nil
}

func (b *resetBackend) WriteFile(_ context.Context, filename string, data []byte) error {
	if b.files == nil {
		b.files = map[string][]byte{}
	}
	b.writeCalls = append(b.writeCalls, filename)
	b.files[filename] = append([]byte(nil), data...)
	return nil
}

type resetProvider struct {
	backend sandboxed.Backend
	opens   int
	closes  int
}

func (p *resetProvider) Open(context.Context) (sandboxed.Backend, error) {
	p.opens++
	return p.backend, nil
}

func (p *resetProvider) Close(context.Context) error {
	p.closes++
	return nil
}

type resetGateway struct {
	configs        []workspace.MCPClientConfig
	bootstrapCalls int
	removed        []string
	closeCalls     int
}

func (g *resetGateway) Bootstrap(context.Context) error {
	g.bootstrapCalls++
	return nil
}

func (g *resetGateway) AddMCP(_ context.Context, config workspace.MCPClientConfig) error {
	g.configs = append(g.configs, config)
	return nil
}

func (g *resetGateway) RemoveMCP(_ context.Context, name string) error {
	g.removed = append(g.removed, name)
	for index, config := range g.configs {
		if config.Name == name {
			g.configs = append(g.configs[:index], g.configs[index+1:]...)
			break
		}
	}
	return nil
}

func (g *resetGateway) ListMCPs(context.Context) ([]workspace.MCPClientConfig, error) {
	return append([]workspace.MCPClientConfig(nil), g.configs...), nil
}

func (*resetGateway) NewMCPClient(config workspace.MCPClientConfig, _ bool) workspace.MCPClient {
	return &testMCPClient{config: config}
}

func (g *resetGateway) Close(context.Context) error {
	g.closeCalls++
	return nil
}

type resetCodec struct{}

func (resetCodec) Marshal(configs []workspace.MCPClientConfig) ([]byte, error) {
	return json.Marshal(configs)
}

func (resetCodec) Unmarshal(data []byte) ([]workspace.MCPClientConfig, error) {
	var configs []workspace.MCPClientConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

func TestWorkspaceResetKeepsRuntimeAndGatewayAlive(t *testing.T) {
	backend := &resetBackend{files: map[string][]byte{}}
	provider := &resetProvider{backend: backend}
	seedConfig := workspace.MCPClientConfig{
		Name:     "seed",
		Type:     workspace.MCPClientTypeStdio,
		Stateful: true,
		Stdio: &workspace.MCPStdioConfig{
			Command: "python3",
		},
	}
	gateway := &resetGateway{configs: []workspace.MCPClientConfig{seedConfig}}
	core, err := sandboxed.New(sandboxed.Config{
		ID:          "workspace-id",
		Workdir:     defaultWorkdir,
		GatewayHome: defaultGatewayHome,
		GatewayPort: defaultGatewayPort,
		Provider:    provider,
		GatewayFactory: func(sandboxed.Backend, int, time.Duration) (sandboxed.Gateway, error) {
			return gateway, nil
		},
		MCPCodec:    resetCodec{},
		DefaultMCPs: []workspace.MCPClient{&testMCPClient{config: seedConfig}},
	})
	if err != nil {
		t.Fatalf("sandboxed.New returned error: %v", err)
	}
	workspaceInstance := &Workspace{core: core}
	if err := workspaceInstance.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	backend.files["/workspace/data/data.bin"] = []byte("data")
	backend.files["/workspace/skills/example/SKILL.md"] = []byte("skill")
	backend.files["/workspace/sessions/session/context.jsonl"] = []byte("context")

	if err := workspaceInstance.Reset(context.Background()); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	if !workspaceInstance.IsAlive() || provider.opens != 1 || provider.closes != 0 || gateway.closeCalls != 0 {
		t.Fatalf("Reset changed runtime lifecycle: alive=%v opens=%d closes=%d gateway closes=%d",
			workspaceInstance.IsAlive(), provider.opens, provider.closes, gateway.closeCalls)
	}
	if !reflect.DeepEqual(gateway.removed, []string{"seed"}) {
		t.Fatalf("removed MCPs = %#v", gateway.removed)
	}
	if !reflect.DeepEqual(backend.deleted, []string{
		"/workspace/data",
		"/workspace/skills",
		"/workspace/sessions",
	}) {
		t.Fatalf("deleted trees = %#v", backend.deleted)
	}
	for filename := range backend.files {
		if strings.HasPrefix(filename, "/workspace/data/") ||
			strings.HasPrefix(filename, "/workspace/skills/") ||
			strings.HasPrefix(filename, "/workspace/sessions/") {
			t.Fatalf("Reset retained workspace state file %q", filename)
		}
	}
	if string(backend.files["/workspace/.mcp"]) != "[]" {
		t.Fatalf("MCP file = %q, want []", backend.files["/workspace/.mcp"])
	}
	mcps, err := workspaceInstance.ListMCPs(context.Background())
	if err != nil || len(mcps) != 0 {
		t.Fatalf("ListMCPs after Reset = %#v, %v", mcps, err)
	}
	tools, err := workspaceInstance.ListTools(context.Background())
	if err != nil || len(tools) != 6 {
		t.Fatalf("ListTools after Reset = %d tools, %v", len(tools), err)
	}
	instructions, err := workspaceInstance.GetInstructions(context.Background())
	if err != nil || !strings.Contains(instructions, defaultWorkdir) {
		t.Fatalf("GetInstructions after Reset = %q, %v", instructions, err)
	}
	skills, err := workspaceInstance.ListSkills(context.Background())
	if err != nil || len(skills) != 0 {
		t.Fatalf("ListSkills after Reset = %#v, %v", skills, err)
	}
	addedConfig := workspace.MCPClientConfig{
		Name:     "added",
		Type:     workspace.MCPClientTypeStdio,
		Stateful: true,
		Stdio: &workspace.MCPStdioConfig{
			Command: "python3",
		},
	}
	if err := workspaceInstance.AddMCP(
		context.Background(),
		&testMCPClient{config: addedConfig},
	); err != nil {
		t.Fatalf("AddMCP after Reset returned error: %v", err)
	}
	mcps, err = workspaceInstance.ListMCPs(context.Background())
	if err != nil || len(mcps) != 1 || mcps[0].Name() != "added" {
		t.Fatalf("ListMCPs after AddMCP = %#v, %v", mcps, err)
	}
	if err := workspaceInstance.RemoveMCP(context.Background(), "added"); err != nil {
		t.Fatalf("RemoveMCP after Reset returned error: %v", err)
	}
	if err := workspaceInstance.Reset(context.Background()); err != nil {
		t.Fatalf("second Reset should be idempotent: %v", err)
	}
}

func TestWorkspaceNilForwarders(t *testing.T) {
	var workspaceInstance *Workspace
	ctx := context.Background()
	if workspaceInstance.WorkspaceID() != "" || workspaceInstance.WorkspaceRoot() != "" ||
		workspaceInstance.SandboxID() != "" || workspaceInstance.IsAlive() {
		t.Fatal("nil workspace identity methods returned non-zero values")
	}
	if err := workspaceInstance.Close(ctx); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if err := workspaceInstance.Reset(ctx); err != nil {
		t.Fatalf("nil Reset returned error: %v", err)
	}
	if err := workspaceInstance.RemoveMCP(ctx, "name"); err != nil {
		t.Fatalf("nil RemoveMCP returned error: %v", err)
	}
	if err := workspaceInstance.RemoveSkill(ctx, "name"); err != nil {
		t.Fatalf("nil RemoveSkill returned error: %v", err)
	}
	if err := workspaceInstance.Initialize(ctx); err == nil {
		t.Fatal("nil Initialize should return an error")
	}
	if _, err := workspaceInstance.GetInstructions(ctx); err == nil {
		t.Fatal("nil GetInstructions should return an error")
	}
	if _, err := workspaceInstance.ListTools(ctx); err == nil {
		t.Fatal("nil ListTools should return an error")
	}
	if _, err := workspaceInstance.ListMCPs(ctx); err == nil {
		t.Fatal("nil ListMCPs should return an error")
	}
	if _, err := workspaceInstance.ListSkills(ctx); err == nil {
		t.Fatal("nil ListSkills should return an error")
	}
	if err := workspaceInstance.AddMCP(ctx, nil); err == nil {
		t.Fatal("nil AddMCP should return an error")
	}
	if err := workspaceInstance.AddSkill(ctx, "skill"); err == nil {
		t.Fatal("nil AddSkill should return an error")
	}
	if _, err := workspaceInstance.OffloadContext(ctx, "session", nil); err == nil {
		t.Fatal("nil OffloadContext should return an error")
	}
	if _, err := workspaceInstance.OffloadToolResult(ctx, "session", nil); err == nil {
		t.Fatal("nil OffloadToolResult should return an error")
	}
	if _, err := workspaceInstance.OffloadDataBlock(ctx, nil); err == nil {
		t.Fatal("nil OffloadDataBlock should return an error")
	}
}

func TestWorkspaceResetRejectsCanceledOrUninitializedState(t *testing.T) {
	workspaceInstance, err := NewWorkspace(withRuntime(&fakeSandboxRuntime{}))
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := workspaceInstance.Reset(context.Background()); err == nil {
		t.Fatal("Reset before Initialize should return an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := workspaceInstance.Reset(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reset canceled error = %v, want context.Canceled", err)
	}
}
