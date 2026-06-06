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

// Package agentsandbox provides an Agent Sandbox-backed workspace implementation.
package agentsandbox

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
You have an Agent Sandbox workspace. All sandbox tools execute inside the sandbox at {workdir}.

Layout:
- data/ for offloaded files
- skills/ for reusable skills
- sessions/ for offloaded context and tool results
</workspace>`

// Workspace 是基于 Kubernetes Agent Sandbox 的 workspace。
type Workspace struct {
	id               string
	templateName     string
	namespace        string
	containerWorkdir string
	hostWorkdir      string
	instructions     string
	apiURL           string
	gatewayName      string
	gatewayNamespace string
	serverPort       int
	mode             connectionMode
	env              map[string]string
	keepSandbox      bool
	requestTimeout   time.Duration
	openTimeout      time.Duration
	maxUploadSize    int64
	maxDownloadSize  int64
	defaultMCPs      []asworkspace.MCPClient
	mcps             []asworkspace.MCPClient
	runtime          sandboxRuntime
	handle           sandboxHandle
	ownsRuntime      bool
	alive            bool
}

var _ asworkspace.Workspace = (*Workspace)(nil)

// New 是 NewWorkspace 的简写。
func New(opts ...Option) (*Workspace, error) {
	return NewWorkspace(opts...)
}

// NewWorkspace 创建 Agent Sandbox-backed workspace。
func NewWorkspace(opts ...Option) (*Workspace, error) {
	workspace := &Workspace{
		id:               utils.NewID(),
		namespace:        defaultNamespace,
		containerWorkdir: defaultContainerWorkdir,
		instructions:     defaultInstructions,
		mode:             connectionModePortForward,
		env:              map[string]string{},
		requestTimeout:   defaultRequestTimeout,
		openTimeout:      defaultOpenTimeout,
		ownsRuntime:      true,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(workspace); err != nil {
			return nil, err
		}
	}
	if workspace.id == "" {
		workspace.id = utils.NewID()
	}
	if workspace.namespace == "" {
		workspace.namespace = defaultNamespace
	}
	if workspace.containerWorkdir == "" {
		workspace.containerWorkdir = defaultContainerWorkdir
	}
	if workspace.instructions == "" {
		workspace.instructions = defaultInstructions
	}
	if workspace.requestTimeout == 0 {
		workspace.requestTimeout = defaultRequestTimeout
	}
	if workspace.openTimeout == 0 {
		workspace.openTimeout = defaultOpenTimeout
	}
	if strings.TrimSpace(workspace.templateName) == "" {
		return nil, fmt.Errorf("workspace/agentsandbox: template name is empty")
	}
	if !strings.HasPrefix(workspace.containerWorkdir, "/") {
		return nil, fmt.Errorf("workspace/agentsandbox: container workdir must be absolute")
	}
	return workspace, nil
}

// WorkspaceID 返回 workspace 标识。
func (w *Workspace) WorkspaceID() string {
	if w == nil {
		return ""
	}
	return w.id
}

// IsAlive 返回 sandbox 是否已初始化且未关闭。
func (w *Workspace) IsAlive() bool {
	return w != nil && w.alive
}

// Initialize 创建或连接 Agent Sandbox。
func (w *Workspace) Initialize(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("workspace/agentsandbox: nil workspace")
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
		return fmt.Errorf("workspace/agentsandbox: sandbox %q is not ready", handle.ID())
	}
	w.handle = handle
	w.alive = true
	return w.saveMCPFile()
}

// Close 删除 SandboxClaim，或在 keepSandbox 模式下只断开连接。
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
			if err := w.handle.Disconnect(ctx); err != nil {
				errs = append(errs, err)
			}
		} else if err := w.handle.Close(ctx); err != nil {
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

// Reset 清理运行态并在下次 Initialize 时创建新 sandbox。
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

// GetInstructions 返回 workspace system prompt 片段。
func (w *Workspace) GetInstructions(ctx context.Context) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace/agentsandbox: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return strings.ReplaceAll(w.instructions, "{workdir}", w.containerWorkdir), nil
}

// ListTools 返回 Agent Sandbox-backed tools。
func (w *Workspace) ListTools(ctx context.Context) ([]tool.Tool, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/agentsandbox: nil workspace")
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

// ListMCPs 返回已注册 MCP clients。
func (w *Workspace) ListMCPs(ctx context.Context) ([]asworkspace.MCPClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w == nil {
		return nil, fmt.Errorf("workspace/agentsandbox: nil workspace")
	}
	return append([]asworkspace.MCPClient(nil), w.mcps...), nil
}

// ListSkills 返回宿主 mirror 中的 skills。
func (w *Workspace) ListSkills(ctx context.Context) ([]skill.Skill, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/agentsandbox: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w.hostWorkdir == "" {
		return []skill.Skill{}, nil
	}
	return skill.NewLocalLoader(filepath.Join(w.hostWorkdir, "skills"), skill.WithScanSubdirs(true)).ListSkills(ctx)
}

// OffloadContext 将上下文写入宿主 mirror。
func (w *Workspace) OffloadContext(ctx context.Context, sessionID string, messages []*message.Message) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace/agentsandbox: nil workspace")
	}
	if w.hostWorkdir == "" {
		return "", fmt.Errorf("workspace/agentsandbox: OffloadContext requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return "", err
	}
	return local.OffloadContext(ctx, sessionID, messages)
}

// OffloadToolResult 将工具结果写入宿主 mirror。
func (w *Workspace) OffloadToolResult(ctx context.Context, sessionID string, result *message.ToolResultBlock) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace/agentsandbox: nil workspace")
	}
	if w.hostWorkdir == "" {
		return "", fmt.Errorf("workspace/agentsandbox: OffloadToolResult requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return "", err
	}
	return local.OffloadToolResult(ctx, sessionID, result)
}

// OffloadDataBlock 将 DataBlock 写入宿主 mirror。
func (w *Workspace) OffloadDataBlock(ctx context.Context, block *message.DataBlock) (*message.DataBlock, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/agentsandbox: nil workspace")
	}
	if w.hostWorkdir == "" {
		return nil, fmt.Errorf("workspace/agentsandbox: OffloadDataBlock requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return nil, err
	}
	return local.OffloadDataBlock(ctx, block)
}

// AddMCP 注册 MCP client。
func (w *Workspace) AddMCP(ctx context.Context, mcp asworkspace.MCPClient) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("workspace/agentsandbox: nil workspace")
	}
	if mcp == nil {
		return fmt.Errorf("workspace/agentsandbox: nil MCP client")
	}
	if w.findMCP(mcp.Name()) >= 0 {
		return fmt.Errorf("workspace/agentsandbox: duplicate MCP %q", mcp.Name())
	}
	w.mcps = append(w.mcps, mcp)
	return w.saveMCPFile()
}

// RemoveMCP 按名称移除 MCP client。
func (w *Workspace) RemoveMCP(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("workspace/agentsandbox: MCP name is empty")
	}
	index := w.findMCP(name)
	if index < 0 {
		return nil
	}
	w.mcps = append(w.mcps[:index], w.mcps[index+1:]...)
	return w.saveMCPFile()
}

// AddSkill 将 skill 复制到宿主 mirror。
func (w *Workspace) AddSkill(ctx context.Context, sourceDir string) error {
	if w == nil {
		return fmt.Errorf("workspace/agentsandbox: nil workspace")
	}
	if w.hostWorkdir == "" {
		return fmt.Errorf("workspace/agentsandbox: AddSkill requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return err
	}
	return local.AddSkill(ctx, sourceDir)
}

// RemoveSkill 从宿主 mirror 移除 skill。
func (w *Workspace) RemoveSkill(ctx context.Context, name string) error {
	if w == nil {
		return nil
	}
	if w.hostWorkdir == "" {
		return fmt.Errorf("workspace/agentsandbox: RemoveSkill requires WithHostWorkdir")
	}
	local, err := w.localMirror()
	if err != nil {
		return err
	}
	return local.RemoveSkill(ctx, name)
}

func (w *Workspace) sandboxSpec() sandboxSpec {
	return sandboxSpec{
		ID:               w.id,
		TemplateName:     w.templateName,
		Namespace:        w.namespace,
		Workdir:          w.containerWorkdir,
		APIURL:           w.apiURL,
		GatewayName:      w.gatewayName,
		GatewayNamespace: w.gatewayNamespace,
		ServerPort:       w.serverPort,
		Mode:             w.mode,
		Env:              cloneStringMap(w.env),
		RequestTimeout:   w.requestTimeout,
		OpenTimeout:      w.openTimeout,
		MaxUploadSize:    w.maxUploadSize,
		MaxDownloadSize:  w.maxDownloadSize,
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
		return fmt.Errorf("workspace/agentsandbox: nil workspace")
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
		return asworkspace.MCPClientConfig{}, fmt.Errorf("workspace/agentsandbox: nil MCP client")
	}
	provider, ok := client.(asworkspace.MCPConfigProvider)
	if !ok {
		return asworkspace.MCPClientConfig{}, fmt.Errorf("workspace/agentsandbox: MCP %q cannot be persisted", client.Name())
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
		return asworkspace.MCPClientConfig{}, fmt.Errorf("workspace/agentsandbox: nil persisted MCP client")
	}
	return cloneMCPClientConfig(c.config), nil
}

func cloneMCPClientConfig(config asworkspace.MCPClientConfig) asworkspace.MCPClientConfig {
	out := config
	if config.Stdio != nil {
		stdio := *config.Stdio
		stdio.Args = append([]string(nil), config.Stdio.Args...)
		stdio.Env = cloneStringMap(config.Stdio.Env)
		out.Stdio = &stdio
	}
	if config.HTTP != nil {
		http := *config.HTTP
		http.Headers = cloneStringMap(config.HTTP.Headers)
		out.HTTP = &http
	}
	out.EnabledTools = append([]string(nil), config.EnabledTools...)
	out.DisabledTools = append([]string(nil), config.DisabledTools...)
	return out
}
