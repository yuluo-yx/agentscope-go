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
	"fmt"
	"path"
	"strings"

	sdk "github.com/alibaba/OpenSandbox/sdks/sandbox/go"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/skill"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
	"github.com/yuluo-yx/agentscope-go/pkg/workspace/gateway"
	"github.com/yuluo-yx/agentscope-go/pkg/workspace/internal/sandboxed"
)

const defaultInstructions = `<workspace>
You have an OpenSandbox workspace. All workspace tools execute inside the remote sandbox at {workdir}.

Layout:
- data/ for offloaded files
- skills/ for reusable skills
- sessions/ for offloaded context and tool results

Close pauses the sandbox so the same workspace ID can resume its files later. Reset keeps the sandbox and gateway alive while clearing workspace-owned state.
</workspace>`

// Workspace is an OpenSandbox Workspace that can resume by workspace ID.
type Workspace struct {
	core     *sandboxed.Workspace
	provider *provider
}

// New is shorthand for NewWorkspace.
func New(options ...Option) (*Workspace, error) {
	return NewWorkspace(options...)
}

// NewWorkspace creates an OpenSandbox Workspace without making a remote request.
func NewWorkspace(options ...Option) (*Workspace, error) {
	config := defaultConfig()
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.runtime == nil {
		config.runtime = &sdkRuntime{}
	}

	connection := sdk.ConnectionConfig{
		Domain:         config.domain,
		Protocol:       config.protocol,
		APIKey:         config.apiKey,
		RequestTimeout: config.requestTimeout,
	}
	metadata := cloneStringMap(config.metadata)
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata[metadataWorkspaceID] = config.id
	provider := &provider{
		runtime: config.runtime,
		spec: sandboxSpec{
			ID:             config.id,
			Image:          config.image,
			Connection:     connection,
			Timeout:        config.sandboxTimeout,
			Env:            cloneStringMap(config.env),
			Metadata:       metadata,
			ResourceLimits: cloneResourceLimits(config.resourceLimits),
			Entrypoint:     append([]string(nil), config.entrypoint...),
			NetworkPolicy:  cloneNetworkPolicy(config.networkPolicy),
		},
	}
	core, err := sandboxed.New(sandboxed.Config{
		ID:                config.id,
		Workdir:           defaultWorkdir,
		GatewayHome:       defaultGatewayHome,
		GatewayPort:       config.gatewayPort,
		Instructions:      config.instructions,
		Provider:          provider,
		GatewayFactory:    sandboxed.NewPythonGateway,
		MCPCodec:          gateway.PythonMCPCodec{},
		BootstrapCommands: bootstrapCommands(config.extraPythonPackages),
		DefaultMCPs:       config.defaultMCPs,
		SkillPaths:        config.skillPaths,
	})
	if err != nil {
		return nil, err
	}
	return &Workspace{core: core, provider: provider}, nil
}

func defaultConfig() config {
	return config{
		id:             utils.NewID(),
		image:          defaultImage,
		protocol:       defaultProtocol,
		requestTimeout: defaultRequestTimeout,
		sandboxTimeout: defaultSandboxTimeout,
		gatewayPort:    defaultGatewayPort,
		env:            map[string]string{},
		metadata:       map[string]string{},
		instructions:   defaultInstructions,
	}
}

func validateConfig(config config) error {
	if strings.TrimSpace(config.id) == "" {
		return fmt.Errorf("workspace/opensandbox: workspace id is empty")
	}
	if strings.TrimSpace(config.image) == "" {
		return fmt.Errorf("workspace/opensandbox: image is empty")
	}
	if config.protocol != "http" && config.protocol != "https" {
		return fmt.Errorf("workspace/opensandbox: protocol must be http or https")
	}
	if config.requestTimeout <= 0 {
		return fmt.Errorf("workspace/opensandbox: request timeout must be positive")
	}
	if config.sandboxTimeout <= 0 {
		return fmt.Errorf("workspace/opensandbox: timeout must be positive")
	}
	if config.gatewayPort <= 0 || config.gatewayPort > 65535 {
		return fmt.Errorf("workspace/opensandbox: gateway port must be between 1 and 65535")
	}
	return nil
}

func bootstrapCommands(extraRequirements []string) [][]string {
	gatewayVenv := path.Join(defaultGatewayHome, ".venv")
	gatewayPython := path.Join(gatewayVenv, "bin", "python")
	requirements := []string{
		"uv", "pip", "install", "--python", gatewayPython,
		"mcp", "uvicorn", "fastapi",
	}
	requirements = append(requirements, extraRequirements...)
	return [][]string{
		{"apt-get", "update", "-qq"},
		{
			"apt-get", "install", "-y", "--no-install-recommends",
			"curl", "ca-certificates", "procps", "ripgrep",
		},
		{
			"curl", "-LsSf", "-o", "/tmp/agentscope-uv-install.sh",
			"https://astral.sh/uv/install.sh",
		},
		{
			"env", "UV_INSTALL_DIR=/usr/local/bin", "INSTALLER_NO_MODIFY_PATH=1",
			"sh", "/tmp/agentscope-uv-install.sh",
		},
		{"uv", "venv", gatewayVenv},
		requirements,
		{"uv", "pip", "install", "--python", gatewayPython, pythonAgentScope},
	}
}

// WorkspaceID returns the stable workspace ID.
func (w *Workspace) WorkspaceID() string {
	if w == nil || w.core == nil {
		return ""
	}
	return w.core.WorkspaceID()
}

// WorkspaceRoot returns the working directory inside OpenSandbox.
func (w *Workspace) WorkspaceRoot() string {
	if w == nil || w.core == nil {
		return ""
	}
	return w.core.WorkspaceRoot()
}

// SandboxID returns the OpenSandbox sandbox ID connected by Initialize.
func (w *Workspace) SandboxID() string {
	if w == nil || w.provider == nil {
		return ""
	}
	return w.provider.SandboxID()
}

// IsAlive reports whether the Workspace is initialized and not closed.
func (w *Workspace) IsAlive() bool {
	return w != nil && w.core != nil && w.core.IsAlive()
}

// Initialize creates, connects to, or resumes OpenSandbox and starts the remote gateway.
func (w *Workspace) Initialize(ctx context.Context) error {
	if w == nil || w.core == nil {
		return fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.Initialize(ctx)
}

// Close pauses the remote Sandbox and releases the local SDK handle.
func (w *Workspace) Close(ctx context.Context) error {
	if w == nil || w.core == nil {
		return nil
	}
	return w.core.Close(ctx)
}

// Reset keeps the Sandbox and gateway alive while clearing Workspace-owned state.
func (w *Workspace) Reset(ctx context.Context) error {
	if w == nil || w.core == nil {
		return nil
	}
	return w.core.Reset(ctx)
}

// GetInstructions returns the OpenSandbox Workspace guidance.
func (w *Workspace) GetInstructions(ctx context.Context) (string, error) {
	if w == nil || w.core == nil {
		return "", fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.GetInstructions(ctx)
}

// ListTools returns built-in tools that execute inside OpenSandbox.
func (w *Workspace) ListTools(ctx context.Context) ([]tool.Tool, error) {
	if w == nil || w.core == nil {
		return nil, fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.ListTools(ctx)
}

// ListMCPs returns MCP clients proxied by the remote gateway.
func (w *Workspace) ListMCPs(ctx context.Context) ([]workspace.MCPClient, error) {
	if w == nil || w.core == nil {
		return nil, fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.ListMCPs(ctx)
}

// ListSkills returns Skills stored in the remote Workspace.
func (w *Workspace) ListSkills(ctx context.Context) ([]skill.Skill, error) {
	if w == nil || w.core == nil {
		return nil, fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.ListSkills(ctx)
}

// AddMCP registers an MCP with the remote gateway and persists it.
func (w *Workspace) AddMCP(ctx context.Context, client workspace.MCPClient) error {
	if w == nil || w.core == nil {
		return fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.AddMCP(ctx, client)
}

// RemoveMCP removes an MCP from the remote gateway and `.mcp` file.
func (w *Workspace) RemoveMCP(ctx context.Context, name string) error {
	if w == nil || w.core == nil {
		return nil
	}
	return w.core.RemoveMCP(ctx, name)
}

// AddSkill copies a local Skill directory into the remote Workspace.
func (w *Workspace) AddSkill(ctx context.Context, sourceDir string) error {
	if w == nil || w.core == nil {
		return fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.AddSkill(ctx, sourceDir)
}

// RemoveSkill removes a remote Skill by name.
func (w *Workspace) RemoveSkill(ctx context.Context, name string) error {
	if w == nil || w.core == nil {
		return nil
	}
	return w.core.RemoveSkill(ctx, name)
}

// OffloadContext writes context into the remote sessions directory.
func (w *Workspace) OffloadContext(
	ctx context.Context,
	sessionID string,
	messages []*message.Message,
) (string, error) {
	if w == nil || w.core == nil {
		return "", fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.OffloadContext(ctx, sessionID, messages)
}

// OffloadToolResult writes a tool result into the remote sessions directory.
func (w *Workspace) OffloadToolResult(
	ctx context.Context,
	sessionID string,
	result *message.ToolResultBlock,
) (string, error) {
	if w == nil || w.core == nil {
		return "", fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.OffloadToolResult(ctx, sessionID, result)
}

// OffloadDataBlock writes base64 data into the remote data directory.
func (w *Workspace) OffloadDataBlock(
	ctx context.Context,
	block *message.DataBlock,
) (*message.DataBlock, error) {
	if w == nil || w.core == nil {
		return nil, fmt.Errorf("workspace/opensandbox: nil workspace")
	}
	return w.core.OffloadDataBlock(ctx, block)
}

var (
	_ workspace.Workspace       = (*Workspace)(nil)
	_ workspace.RootedWorkspace = (*Workspace)(nil)
)
