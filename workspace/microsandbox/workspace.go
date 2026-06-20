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

// Package microsandbox provides a Microsandbox-backed workspace implementation.
package microsandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/tool/skill"
	"github.com/yuluo-yx/agentscope-go/utils"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
	wslocal "github.com/yuluo-yx/agentscope-go/workspace/local"
)

const defaultInstructions = `<workspace>
You have a Microsandbox workspace. All sandbox tools execute inside a local microVM at {workdir}.

Layout:
- data/ for offloaded files
- skills/ for reusable skills
- sessions/ for offloaded context and tool results
</workspace>`

// Workspace is a Microsandbox-backed workspace.
type Workspace struct {
	id               string
	sandboxName      string
	image            string
	containerWorkdir string
	hostWorkdir      string
	instructions     string
	env              map[string]string
	cpus             uint8
	memoryMiB        uint32
	ensureInstalled  bool
	keepSandbox      bool
	requestTimeout   time.Duration
	openTimeout      time.Duration
	defaultMCPs      []asworkspace.MCPClient
	mcps             []asworkspace.MCPClient
	runtime          sandboxRuntime
	handle           sandboxHandle
	ownsRuntime      bool
	alive            bool
}

var (
	_ asworkspace.Workspace       = (*Workspace)(nil)
	_ asworkspace.RootedWorkspace = (*Workspace)(nil)
)

// New is a shorthand for NewWorkspace.
func New(opts ...Option) (*Workspace, error) {
	return NewWorkspace(opts...)
}

// NewWorkspace creates a Microsandbox-backed workspace.
func NewWorkspace(opts ...Option) (*Workspace, error) {
	workspace := defaultWorkspace()
	if err := workspace.applyOptions(opts...); err != nil {
		return nil, err
	}
	workspace.applyDefaults()
	return workspace, nil
}

func defaultWorkspace() *Workspace {
	return &Workspace{
		id:               utils.NewID(),
		image:            defaultImage,
		containerWorkdir: defaultContainerWorkdir,
		instructions:     defaultInstructions,
		env:              map[string]string{},
		ensureInstalled:  true,
		requestTimeout:   defaultRequestTimeout,
		openTimeout:      defaultOpenTimeout,
		ownsRuntime:      true,
	}
}

func (w *Workspace) applyOptions(opts ...Option) error {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(w); err != nil {
			return err
		}
	}
	return nil
}

func (w *Workspace) applyDefaults() {
	if w.id == "" {
		w.id = utils.NewID()
	}
	if w.sandboxName == "" {
		w.sandboxName = w.id
	}
	if w.image == "" {
		w.image = defaultImage
	}
	if w.containerWorkdir == "" {
		w.containerWorkdir = defaultContainerWorkdir
	}
	if w.instructions == "" {
		w.instructions = defaultInstructions
	}
	if w.requestTimeout == 0 {
		w.requestTimeout = defaultRequestTimeout
	}
	if w.openTimeout == 0 {
		w.openTimeout = defaultOpenTimeout
	}
}

// WorkspaceID returns the workspace identifier.
func (w *Workspace) WorkspaceID() string {
	if w == nil {
		return ""
	}
	return w.id
}

// WorkspaceRoot returns the sandbox root exposed to the agent.
func (w *Workspace) WorkspaceRoot() string {
	if w == nil {
		return ""
	}
	return w.containerWorkdir
}

// SandboxName returns the Microsandbox sandbox name.
func (w *Workspace) SandboxName() string {
	if w == nil {
		return ""
	}
	return w.sandboxName
}

// IsAlive reports whether the Microsandbox has been initialized and not closed.
func (w *Workspace) IsAlive() bool {
	return w != nil && w.alive
}

// Initialize creates a Microsandbox and prepares workspace resources.
func (w *Workspace) Initialize(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.alive {
		return nil
	}
	if err := w.prepareHostWorkdir(); err != nil {
		return err
	}
	if err := w.restoreOrSeedMCPs(ctx); err != nil {
		return err
	}
	if w.runtime == nil {
		rt, err := newSDKRuntime()
		if err != nil {
			return err
		}
		w.runtime = rt
		w.ownsRuntime = true
	}
	handle, err := w.runtime.Create(ctx, w.sandboxSpec())
	if err != nil {
		return err
	}
	ready, err := handle.IsReady(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("workspace/microsandbox: sandbox %q is not ready", handle.ID())
	}
	w.handle = handle
	w.alive = true
	return w.saveMCPFile()
}

// Close stops the sandbox, or only detaches locally when keepSandbox is enabled.
func (w *Workspace) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var errs []error
	if w.handle != nil {
		if w.keepSandbox {
			if err := w.handle.Detach(ctx); err != nil {
				errs = append(errs, err)
			}
		} else if err := w.handle.Stop(ctx); err != nil {
			errs = append(errs, err)
		}
		if err := w.handle.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if w.runtime != nil && w.ownsRuntime {
		if err := w.runtime.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	w.handle = nil
	w.alive = false
	return errors.Join(errs...)
}

// Reset clears runtime state and creates a new sandbox on the next Initialize call.
func (w *Workspace) Reset(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if err := w.Close(ctx); err != nil {
		return err
	}
	if w.hostWorkdir != "" {
		for _, subdir := range []string{"data", "skills", "sessions"} {
			if err := os.RemoveAll(filepath.Join(w.hostWorkdir, subdir)); err != nil {
				return err
			}
		}
		if err := os.Remove(filepath.Join(w.hostWorkdir, ".mcp")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// GetInstructions returns the workspace system prompt fragment.
func (w *Workspace) GetInstructions(ctx context.Context) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return strings.ReplaceAll(w.instructions, "{workdir}", w.containerWorkdir), nil
}

// ListTools returns Microsandbox-backed tools.
func (w *Workspace) ListTools(ctx context.Context) ([]tool.Tool, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []tool.Tool{
		newBashTool(w),
		newEditTool(w),
		newGlobTool(w),
		newGrepTool(w),
		newReadTool(w),
		newWriteTool(w),
	}, nil
}

// ListMCPs returns the registered MCP clients.
func (w *Workspace) ListMCPs(ctx context.Context) ([]asworkspace.MCPClient, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]asworkspace.MCPClient(nil), w.mcps...), nil
}

// ListSkills returns skills from the host mirror.
func (w *Workspace) ListSkills(ctx context.Context) ([]skill.Skill, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w.hostWorkdir == "" {
		return []skill.Skill{}, nil
	}
	return skill.NewLocalLoader(filepath.Join(w.hostWorkdir, "skills"), skill.WithScanSubdirs(true)).ListSkills(ctx)
}

// OffloadContext writes context into the host mirror.
func (w *Workspace) OffloadContext(ctx context.Context, sessionID string, messages []*message.Message) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if w.hostWorkdir == "" {
		return "", fmt.Errorf("workspace/microsandbox: OffloadContext requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return "", err
	}
	return local.OffloadContext(ctx, sessionID, messages)
}

// OffloadToolResult writes a tool result into the host mirror.
func (w *Workspace) OffloadToolResult(ctx context.Context, sessionID string, result *message.ToolResultBlock) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if w.hostWorkdir == "" {
		return "", fmt.Errorf("workspace/microsandbox: OffloadToolResult requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return "", err
	}
	return local.OffloadToolResult(ctx, sessionID, result)
}

// OffloadDataBlock writes a DataBlock into the host mirror.
func (w *Workspace) OffloadDataBlock(ctx context.Context, block *message.DataBlock) (*message.DataBlock, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if w.hostWorkdir == "" {
		return nil, fmt.Errorf("workspace/microsandbox: OffloadDataBlock requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return nil, err
	}
	return local.OffloadDataBlock(ctx, block)
}

// AddMCP registers an MCP client.
func (w *Workspace) AddMCP(ctx context.Context, mcp asworkspace.MCPClient) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if mcp == nil {
		return fmt.Errorf("workspace/microsandbox: nil MCP client")
	}
	if w.findMCP(mcp.Name()) >= 0 {
		return fmt.Errorf("workspace/microsandbox: duplicate MCP %q", mcp.Name())
	}
	w.mcps = append(w.mcps, mcp)
	return w.saveMCPFile()
}

// RemoveMCP removes an MCP client by name.
func (w *Workspace) RemoveMCP(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("workspace/microsandbox: MCP name is empty")
	}
	index := w.findMCP(name)
	if index < 0 {
		return nil
	}
	w.mcps = append(w.mcps[:index], w.mcps[index+1:]...)
	return w.saveMCPFile()
}

// AddSkill copies a skill into the host mirror.
func (w *Workspace) AddSkill(ctx context.Context, sourceDir string) error {
	if w == nil {
		return fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if w.hostWorkdir == "" {
		return fmt.Errorf("workspace/microsandbox: AddSkill requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return err
	}
	return local.AddSkill(ctx, sourceDir)
}

// RemoveSkill removes a skill from the host mirror.
func (w *Workspace) RemoveSkill(ctx context.Context, name string) error {
	if w == nil {
		return nil
	}
	if w.hostWorkdir == "" {
		return fmt.Errorf("workspace/microsandbox: RemoveSkill requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return err
	}
	return local.RemoveSkill(ctx, name)
}

func (w *Workspace) sandboxSpec() sandboxSpec {
	return sandboxSpec{
		ID:              w.id,
		Name:            w.sandboxName,
		Image:           w.image,
		Workdir:         w.containerWorkdir,
		Env:             cloneStringMap(w.env),
		CPUs:            w.cpus,
		MemoryMiB:       w.memoryMiB,
		EnsureInstalled: w.ensureInstalled,
		RequestTimeout:  w.requestTimeout,
		OpenTimeout:     w.openTimeout,
	}
}

func (w *Workspace) prepareHostWorkdir() error {
	if w.hostWorkdir == "" {
		return nil
	}
	for _, subdir := range []string{"data", "skills", "sessions"} {
		if err := os.MkdirAll(filepath.Join(w.hostWorkdir, subdir), 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (w *Workspace) localMirror() (*wslocal.Workspace, error) {
	return wslocal.NewWorkspace(w.hostWorkdir, wslocal.WithWorkspaceID(w.id), wslocal.WithInstructions(w.instructions))
}

func (w *Workspace) restoreOrSeedMCPs(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("workspace/microsandbox: nil workspace")
	}
	if w.hostWorkdir == "" {
		w.mcps = append([]asworkspace.MCPClient(nil), w.defaultMCPs...)
		return nil
	}
	path := filepath.Join(w.hostWorkdir, ".mcp")
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var configs []asworkspace.MCPClientConfig
		if len(strings.TrimSpace(string(data))) > 0 {
			if unmarshalErr := json.Unmarshal(data, &configs); unmarshalErr != nil {
				w.mcps = append([]asworkspace.MCPClient(nil), w.defaultMCPs...)
				return nil
			}
		}
		restored := make([]asworkspace.MCPClient, 0, len(configs))
		for _, config := range configs {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			restored = append(restored, &persistedMCPClient{config: cloneMCPClientConfig(config)})
		}
		w.mcps = restored
	case os.IsNotExist(err):
		w.mcps = append([]asworkspace.MCPClient(nil), w.defaultMCPs...)
	default:
		w.mcps = append([]asworkspace.MCPClient(nil), w.defaultMCPs...)
	}
	return nil
}

func (w *Workspace) saveMCPFile() error {
	if w == nil || w.hostWorkdir == "" {
		return nil
	}
	configs := make([]asworkspace.MCPClientConfig, 0, len(w.mcps))
	for _, client := range w.mcps {
		config, err := mcpConfig(client)
		if err != nil {
			return err
		}
		configs = append(configs, config)
	}
	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(w.hostWorkdir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.hostWorkdir, ".mcp"), data, 0o600)
}

func (w *Workspace) findMCP(name string) int {
	for index, client := range w.mcps {
		if client != nil && client.Name() == name {
			return index
		}
	}
	return -1
}

func mcpConfig(client asworkspace.MCPClient) (asworkspace.MCPClientConfig, error) {
	if client == nil {
		return asworkspace.MCPClientConfig{}, fmt.Errorf("workspace/microsandbox: nil MCP client")
	}
	provider, ok := client.(asworkspace.MCPConfigProvider)
	if !ok {
		return asworkspace.MCPClientConfig{}, fmt.Errorf("workspace/microsandbox: MCP %q cannot be persisted", client.Name())
	}
	config, err := provider.MCPClientConfig()
	if err != nil {
		return asworkspace.MCPClientConfig{}, err
	}
	return cloneMCPClientConfig(config), nil
}

type persistedMCPClient struct {
	config asworkspace.MCPClientConfig
}

func (c *persistedMCPClient) Name() string {
	if c == nil {
		return ""
	}
	return c.config.Name
}

func (c *persistedMCPClient) IsStateful() bool {
	return c != nil && c.config.Stateful
}

func (c *persistedMCPClient) IsConnected() bool {
	return false
}

func (c *persistedMCPClient) Connect(context.Context) error {
	return nil
}

func (c *persistedMCPClient) Close() error {
	return nil
}

func (c *persistedMCPClient) ListTools(context.Context) ([]tool.Tool, error) {
	return []tool.Tool{}, nil
}

func (c *persistedMCPClient) MCPClientConfig() (asworkspace.MCPClientConfig, error) {
	if c == nil {
		return asworkspace.MCPClientConfig{}, fmt.Errorf("workspace/microsandbox: nil persisted MCP client")
	}
	return cloneMCPClientConfig(c.config), nil
}
