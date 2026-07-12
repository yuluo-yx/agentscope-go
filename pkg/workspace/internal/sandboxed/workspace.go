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

package sandboxed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/skill"
	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

const (
	defaultGatewayTimeout = 30 * time.Second
	defaultInstructions   = `<workspace>
You have a remote sandbox workspace. All workspace tools execute inside the sandbox at {workdir}.

Layout:
- data/ for offloaded files
- skills/ for reusable skills
- sessions/ for offloaded context and tool results
</workspace>`
	deleteTreeScript = `for target do
  if [ -e "$target" ] || [ -L "$target" ]; then
    find "$target" -depth -delete
  fi
done`
	launchGatewayScript = `pid_file="${6}.pid"
old_pid=
if [ -s "$pid_file" ]; then
  IFS= read -r old_pid <"$pid_file" || true
fi
case "$old_pid" in
  ''|*[!0-9]*) ;;
  *)
    if [ -r "/proc/$old_pid/cmdline" ] && grep -F -a -q -- "$1" "/proc/$old_pid/cmdline"; then
      kill "$old_pid" >/dev/null 2>&1 || true
      attempt=0
      while [ -d "/proc/$old_pid" ] && [ "$attempt" -lt 50 ]; do
        sleep 0.1
        attempt=$((attempt + 1))
      done
      if [ -d "/proc/$old_pid" ]; then
        kill -KILL "$old_pid" >/dev/null 2>&1 || true
      fi
    fi
    ;;
esac
nohup "$2" -u "$3" --config "$4" --port "$5" >"$6" 2>&1 </dev/null &
printf '%s\n' "$!" >"$pid_file"`
)

// Workspace implements a Workspace lifecycle independent of a specific remote runtime.
type Workspace struct {
	mu sync.Mutex

	id             string
	workdir        string
	instructions   string
	gatewayHome    string
	gatewayPort    int
	gatewayTimeout time.Duration
	mcpFile        string
	gatewayVenv    string
	gatewayPython  string
	gatewayScript  string
	gatewayLog     string
	gatewayMarker  string
	gatewayVersion string
	dataDir        string
	skillsDir      string
	sessionsDir    string

	provider          Provider
	gatewayFactory    GatewayFactory
	mcpCodec          MCPCodec
	bootstrapCommands [][]string
	defaultMCPs       []workspace.MCPClient
	skillPaths        []string

	backend     Backend
	gateway     Gateway
	gatewayGate *operationGate
	mcps        []workspace.MCPClient
	alive       bool
}

// New creates a shared remote Workspace lifecycle.
func New(config Config) (*Workspace, error) {
	id := strings.TrimSpace(config.ID)
	if id == "" {
		return nil, fmt.Errorf("workspace/sandboxed: workspace id is empty")
	}
	workdir, err := absoluteSandboxPath(config.Workdir)
	if err != nil {
		return nil, fmt.Errorf("workspace/sandboxed: invalid workdir: %w", err)
	}
	gatewayHome, err := absoluteSandboxPath(config.GatewayHome)
	if err != nil {
		return nil, fmt.Errorf("workspace/sandboxed: invalid gateway home: %w", err)
	}
	if config.GatewayPort <= 0 || config.GatewayPort > 65535 {
		return nil, fmt.Errorf("workspace/sandboxed: gateway port must be between 1 and 65535")
	}
	if config.Provider == nil {
		return nil, fmt.Errorf("workspace/sandboxed: provider is nil")
	}
	if config.GatewayFactory == nil {
		return nil, fmt.Errorf("workspace/sandboxed: gateway factory is nil")
	}
	if config.MCPCodec == nil {
		return nil, fmt.Errorf("workspace/sandboxed: MCP codec is nil")
	}
	for index, command := range config.BootstrapCommands {
		if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
			return nil, fmt.Errorf("workspace/sandboxed: bootstrap command %d is empty", index)
		}
	}
	instructions := config.Instructions
	if strings.TrimSpace(instructions) == "" {
		instructions = defaultInstructions
	}
	gatewayTimeout := config.GatewayTimeout
	if gatewayTimeout <= 0 {
		gatewayTimeout = defaultGatewayTimeout
	}
	gatewayVenv := path.Join(gatewayHome, ".venv")
	w := &Workspace{
		id:                id,
		workdir:           workdir,
		instructions:      instructions,
		gatewayHome:       gatewayHome,
		gatewayPort:       config.GatewayPort,
		gatewayTimeout:    gatewayTimeout,
		mcpFile:           path.Join(workdir, ".mcp"),
		gatewayVenv:       gatewayVenv,
		gatewayPython:     path.Join(gatewayVenv, "bin", "python"),
		gatewayScript:     path.Join(gatewayHome, "_mcp_gateway_app.py"),
		gatewayLog:        path.Join(gatewayHome, "gateway.log"),
		gatewayMarker:     path.Join(gatewayHome, ".bootstrap-version"),
		gatewayVersion:    bootstrapFingerprint(config.BootstrapCommands),
		dataDir:           path.Join(workdir, "data"),
		skillsDir:         path.Join(workdir, "skills"),
		sessionsDir:       path.Join(workdir, "sessions"),
		provider:          config.Provider,
		gatewayFactory:    config.GatewayFactory,
		mcpCodec:          config.MCPCodec,
		bootstrapCommands: cloneArgvList(config.BootstrapCommands),
		defaultMCPs:       append([]workspace.MCPClient(nil), config.DefaultMCPs...),
		skillPaths:        append([]string(nil), config.SkillPaths...),
	}
	return w, nil
}

// WorkspaceID returns the stable Workspace identifier.
func (w *Workspace) WorkspaceID() string {
	if w == nil {
		return ""
	}
	return w.id
}

// WorkspaceRoot returns the remote working directory visible to the agent.
func (w *Workspace) WorkspaceRoot() string {
	if w == nil {
		return ""
	}
	return w.workdir
}

// IsAlive reports whether the shared lifecycle has been initialized.
func (w *Workspace) IsAlive() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alive
}

// Initialize connects the runtime, restores remote state, and starts the loopback gateway.
func (w *Workspace) Initialize(ctx context.Context) (err error) {
	if w == nil {
		return fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.alive {
		return nil
	}

	backend, err := w.provider.Open(ctx)
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: open provider: %w", err)
	}
	if backend == nil {
		rollbackCtx, cancel := w.detachedContext(ctx)
		defer cancel()
		_ = w.provider.Close(rollbackCtx)
		return fmt.Errorf("workspace/sandboxed: provider returned nil backend")
	}
	w.backend = backend
	succeeded := false
	defer func() {
		if !succeeded {
			err = errors.Join(err, w.rollbackInitialize(ctx))
		}
	}()

	if err := w.ensureLayout(ctx); err != nil {
		return err
	}
	configs, err := w.restoreMCPConfigs(ctx)
	if err != nil {
		return err
	}
	gatewayClient, gatewayGate, err := w.setupGateway(ctx)
	if err != nil {
		return err
	}
	w.gateway = gatewayClient
	w.gatewayGate = gatewayGate

	if err := w.loadGatewayMCPs(ctx, gatewayClient, configs); err != nil {
		return err
	}
	if err := w.seedSkillsLocked(ctx); err != nil {
		return err
	}
	w.alive = true
	succeeded = true
	return nil
}

func (w *Workspace) rollbackInitialize(ctx context.Context) error {
	rollbackCtx, cancel := w.detachedContext(ctx)
	defer cancel()
	var rollbackErrs []error
	if w.gatewayGate != nil {
		if err := w.gatewayGate.closeAndWait(rollbackCtx); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("workspace/sandboxed: rollback gateway operations: %w", err))
		}
	}
	if w.gateway != nil {
		if err := w.gateway.Close(internalGatewayContext(rollbackCtx)); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("workspace/sandboxed: rollback gateway: %w", err))
		}
	}
	if err := w.provider.Close(rollbackCtx); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("workspace/sandboxed: rollback provider: %w", err))
	}
	w.backend = nil
	w.gateway = nil
	w.gatewayGate = nil
	w.mcps = nil
	w.alive = false
	return errors.Join(rollbackErrs...)
}

func (w *Workspace) loadGatewayMCPs(
	ctx context.Context,
	gatewayClient Gateway,
	expected []workspace.MCPClientConfig,
) error {
	live, err := gatewayClient.ListMCPs(ctx)
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: list gateway MCPs: %w", err)
	}
	if len(live) != len(expected) {
		return fmt.Errorf(
			"workspace/sandboxed: gateway restored %d MCPs, expected %d",
			len(live),
			len(expected),
		)
	}
	expectedNames := make(map[string]struct{}, len(expected))
	for _, config := range expected {
		expectedNames[config.Name] = struct{}{}
	}
	w.mcps = make([]workspace.MCPClient, 0, len(live))
	for _, config := range live {
		if _, ok := expectedNames[config.Name]; !ok {
			return fmt.Errorf("workspace/sandboxed: gateway restored unexpected MCP %q", config.Name)
		}
		w.mcps = append(w.mcps, gatewayClient.NewMCPClient(config, true))
	}
	return nil
}

func (w *Workspace) seedSkillsLocked(ctx context.Context) error {
	for _, source := range w.skillPaths {
		if err := w.addSkillLocked(ctx, source); err != nil {
			return fmt.Errorf("workspace/sandboxed: seed skill %q: %w", source, err)
		}
	}
	return nil
}

// Close releases the gateway facade and asks the provider to preserve or close the remote runtime.
func (w *Workspace) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.gatewayGate != nil {
		if err := w.gatewayGate.closeAndWait(ctx); err != nil {
			w.gatewayGate.reopen()
			return fmt.Errorf("workspace/sandboxed: wait for gateway operations: %w", err)
		}
	}

	var errs []error
	if w.gateway != nil {
		if err := w.gateway.Close(internalGatewayContext(ctx)); err != nil {
			errs = append(errs, fmt.Errorf("workspace/sandboxed: close gateway: %w", err))
		}
	}
	if w.provider != nil {
		if err := w.provider.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("workspace/sandboxed: close provider: %w", err))
		}
	}
	markMCPsDisconnected(w.mcps)
	w.gateway = nil
	w.gatewayGate = nil
	w.backend = nil
	w.mcps = nil
	w.alive = false
	return errors.Join(errs...)
}

// Reset preserves the runtime and gateway while clearing remote data, skills, sessions, and MCPs.
func (w *Workspace) Reset(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive || w.backend == nil || w.gateway == nil {
		return fmt.Errorf("workspace/sandboxed: workspace is not initialized")
	}
	if w.gatewayGate != nil {
		if err := w.gatewayGate.closeAndWait(ctx); err != nil {
			w.gatewayGate.reopen()
			return fmt.Errorf("workspace/sandboxed: wait for gateway operations: %w", err)
		}
		defer w.gatewayGate.reopen()
	}

	var errs []error
	if err := w.resetMCPsLocked(internalGatewayContext(ctx)); err != nil {
		errs = append(errs, err)
	}
	if err := w.deleteTrees(ctx, w.dataDir, w.skillsDir, w.sessionsDir); err != nil {
		errs = append(errs, err)
	} else if err := w.ensureDataDirectories(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (w *Workspace) resetMCPsLocked(ctx context.Context) error {
	snapshot := append([]workspace.MCPClient(nil), w.mcps...)
	snapshotConfigs := make([]workspace.MCPClientConfig, 0, len(snapshot))
	for _, client := range snapshot {
		if client == nil {
			continue
		}
		config, err := mcpConfig(client)
		if err != nil {
			return err
		}
		snapshotConfigs = append(snapshotConfigs, config)
	}

	emptyData, err := w.mcpCodec.Marshal([]workspace.MCPClientConfig{})
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: encode empty MCP file: %w", err)
	}
	snapshotData, err := w.mcpCodec.Marshal(snapshotConfigs)
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: encode current MCP file: %w", err)
	}
	liveConfigs, err := w.gateway.ListMCPs(ctx)
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: list gateway MCPs before reset: %w", err)
	}

	for _, config := range liveConfigs {
		if err := w.gateway.RemoveMCP(ctx, config.Name); err != nil {
			rollbackErr := w.restoreGatewayMCPsLocked(ctx, liveConfigs)
			var reconcileErr error
			if rollbackErr != nil {
				reconcileErr = w.reconcileMCPsLocked(ctx)
			}
			return errors.Join(
				fmt.Errorf("workspace/sandboxed: remove MCP %q: %w", config.Name, err),
				rollbackErr,
				reconcileErr,
			)
		}
	}
	if err := w.backend.WriteFile(ctx, w.mcpFile, emptyData); err != nil {
		rollbackErr := w.restoreGatewayMCPsLocked(ctx, liveConfigs)
		rollbackCtx, cancel := w.detachedContext(ctx)
		defer cancel()
		fileRollbackErr := w.backend.WriteFile(rollbackCtx, w.mcpFile, snapshotData)
		if fileRollbackErr != nil {
			fileRollbackErr = fmt.Errorf("workspace/sandboxed: restore MCP file: %w", fileRollbackErr)
		}
		var reconcileErr error
		if rollbackErr != nil {
			reconcileErr = w.reconcileMCPsLocked(rollbackCtx)
		}
		return errors.Join(
			fmt.Errorf("workspace/sandboxed: clear MCP file: %w", err),
			rollbackErr,
			fileRollbackErr,
			reconcileErr,
		)
	}
	markMCPsDisconnected(snapshot)
	w.mcps = nil
	return nil
}

func (w *Workspace) restoreGatewayMCPsLocked(
	ctx context.Context,
	configs []workspace.MCPClientConfig,
) error {
	rollbackCtx, cancel := w.detachedContext(ctx)
	defer cancel()
	live, err := w.gateway.ListMCPs(rollbackCtx)
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: list MCPs during reset rollback: %w", err)
	}
	liveNames := make(map[string]struct{}, len(live))
	for _, config := range live {
		liveNames[config.Name] = struct{}{}
	}
	var errs []error
	for _, config := range configs {
		if _, exists := liveNames[config.Name]; exists {
			continue
		}
		if err := w.gateway.AddMCP(rollbackCtx, config); err != nil {
			errs = append(errs, fmt.Errorf(
				"workspace/sandboxed: restore MCP %q during reset rollback: %w",
				config.Name,
				err,
			))
		}
	}
	return errors.Join(errs...)
}

func (w *Workspace) reconcileMCPsLocked(ctx context.Context) error {
	live, err := w.gateway.ListMCPs(ctx)
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: reconcile gateway MCPs: %w", err)
	}
	markMCPsDisconnected(w.mcps)
	w.mcps = make([]workspace.MCPClient, 0, len(live))
	for _, config := range live {
		w.mcps = append(w.mcps, w.gateway.NewMCPClient(config, true))
	}
	if err := w.saveMCPFileLocked(ctx); err != nil {
		return fmt.Errorf("workspace/sandboxed: persist reconciled MCPs: %w", err)
	}
	return nil
}

type mcpDisconnectionMarker interface {
	MarkDisconnected()
}

func markMCPDisconnected(client workspace.MCPClient) {
	if marker, ok := client.(mcpDisconnectionMarker); ok {
		marker.MarkDisconnected()
	}
}

func markMCPsDisconnected(clients []workspace.MCPClient) {
	for _, client := range clients {
		markMCPDisconnected(client)
	}
}

// GetInstructions returns Workspace guidance containing the remote directory.
func (w *Workspace) GetInstructions(ctx context.Context) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return strings.ReplaceAll(w.instructions, "{workdir}", w.workdir), nil
}

// ListTools returns the six built-in tools that execute through the remote Backend.
func (w *Workspace) ListTools(ctx context.Context) ([]tool.Tool, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return newRemoteTools(w), nil
}

// ListMCPs returns the MCP clients currently proxied by the gateway.
func (w *Workspace) ListMCPs(ctx context.Context) ([]workspace.MCPClient, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]workspace.MCPClient(nil), w.mcps...), nil
}

// AddMCP registers an MCP with both the gateway and the remote `.mcp` file.
func (w *Workspace) AddMCP(ctx context.Context, client workspace.MCPClient) error {
	if w == nil {
		return fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive || w.gateway == nil {
		return fmt.Errorf("workspace/sandboxed: workspace is not initialized")
	}
	if client == nil {
		return fmt.Errorf("workspace/sandboxed: nil MCP client")
	}
	if strings.TrimSpace(client.Name()) == "" {
		return fmt.Errorf("workspace/sandboxed: MCP name is empty")
	}
	if w.findMCPLocked(client.Name()) >= 0 {
		return fmt.Errorf("workspace/sandboxed: duplicate MCP %q", client.Name())
	}
	config, err := mcpConfig(client)
	if err != nil {
		return err
	}
	if err := w.gateway.AddMCP(ctx, config); err != nil {
		return fmt.Errorf("workspace/sandboxed: add gateway MCP %q: %w", client.Name(), err)
	}
	proxy := w.gateway.NewMCPClient(config, true)
	w.mcps = append(w.mcps, proxy)
	if err := w.saveMCPFileLocked(ctx); err != nil {
		rollbackCtx, cancel := w.detachedContext(ctx)
		defer cancel()
		rollbackErr := w.gateway.RemoveMCP(rollbackCtx, client.Name())
		if rollbackErr == nil {
			w.mcps = w.mcps[:len(w.mcps)-1]
			markMCPDisconnected(proxy)
			return err
		}
		reconcileErr := w.reconcileMCPsLocked(rollbackCtx)
		return errors.Join(
			err,
			fmt.Errorf("workspace/sandboxed: rollback added MCP %q: %w", client.Name(), rollbackErr),
			reconcileErr,
		)
	}
	return nil
}

// RemoveMCP removes an MCP from both the gateway and the remote `.mcp` file.
func (w *Workspace) RemoveMCP(ctx context.Context, name string) error {
	if w == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("workspace/sandboxed: MCP name is empty")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive || w.gateway == nil {
		return fmt.Errorf("workspace/sandboxed: workspace is not initialized")
	}
	index := w.findMCPLocked(name)
	if index < 0 {
		return nil
	}
	removed := w.mcps[index]
	config, configErr := mcpConfig(removed)
	if configErr != nil {
		return configErr
	}
	if w.gatewayGate != nil {
		if err := w.gatewayGate.closeAndWait(ctx); err != nil {
			w.gatewayGate.reopen()
			return fmt.Errorf("workspace/sandboxed: wait for gateway operations: %w", err)
		}
		defer w.gatewayGate.reopen()
	}
	internalCtx := internalGatewayContext(ctx)
	if err := w.gateway.RemoveMCP(internalCtx, name); err != nil {
		return fmt.Errorf("workspace/sandboxed: remove gateway MCP %q: %w", name, err)
	}
	w.mcps = append(w.mcps[:index], w.mcps[index+1:]...)
	if err := w.saveMCPFileLocked(ctx); err != nil {
		rollbackCtx, cancel := w.detachedContext(internalCtx)
		defer cancel()
		rollbackErr := w.gateway.AddMCP(rollbackCtx, config)
		if rollbackErr == nil {
			w.mcps = append(w.mcps, nil)
			copy(w.mcps[index+1:], w.mcps[index:])
			w.mcps[index] = removed
			return err
		}
		markMCPDisconnected(removed)
		reconcileErr := w.reconcileMCPsLocked(rollbackCtx)
		return errors.Join(
			err,
			fmt.Errorf("workspace/sandboxed: rollback removed MCP %q: %w", name, rollbackErr),
			reconcileErr,
		)
	}
	markMCPDisconnected(removed)
	return nil
}

func (w *Workspace) ensureLayout(ctx context.Context) error {
	if err := w.ensureDataDirectories(ctx); err != nil {
		return err
	}
	result, err := w.backend.Exec(ctx, []string{
		"mkdir", "-p", w.gatewayHome,
	}, ExecOptions{CWD: "/"})
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: create gateway home: %w", err)
	}
	if !result.OK() {
		return commandError("create gateway home", result)
	}
	return nil
}

func (w *Workspace) ensureDataDirectories(ctx context.Context) error {
	result, err := w.backend.Exec(ctx, []string{
		"mkdir", "-p", w.workdir, w.dataDir, w.skillsDir, w.sessionsDir,
	}, ExecOptions{CWD: "/"})
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: create workspace layout: %w", err)
	}
	if !result.OK() {
		return commandError("create workspace layout", result)
	}
	return nil
}

func (w *Workspace) restoreMCPConfigs(ctx context.Context) ([]workspace.MCPClientConfig, error) {
	defaults := make([]workspace.MCPClientConfig, 0, len(w.defaultMCPs))
	for _, client := range w.defaultMCPs {
		config, err := mcpConfig(client)
		if err != nil {
			return nil, err
		}
		defaults = append(defaults, config)
	}
	configs := defaults
	exists, err := w.fileExists(ctx, w.mcpFile)
	if err != nil {
		return nil, err
	}
	if exists {
		data, readErr := w.backend.ReadFile(ctx, w.mcpFile)
		if readErr != nil {
			return nil, fmt.Errorf("workspace/sandboxed: read MCP file: %w", readErr)
		}
		decoded, decodeErr := w.mcpCodec.Unmarshal(data)
		if decodeErr == nil {
			configs = decoded
		}
	}
	data, err := w.mcpCodec.Marshal(configs)
	if err != nil {
		return nil, fmt.Errorf("workspace/sandboxed: encode MCP file: %w", err)
	}
	if err := w.backend.WriteFile(ctx, w.mcpFile, data); err != nil {
		return nil, fmt.Errorf("workspace/sandboxed: write MCP file: %w", err)
	}
	return configs, nil
}

func (w *Workspace) setupGateway(ctx context.Context) (Gateway, *operationGate, error) {
	if err := w.bootstrapGateway(ctx); err != nil {
		return nil, nil, err
	}

	launch := []string{
		"sh", "-c", launchGatewayScript, "--",
		w.gatewayScript,
		w.gatewayPython,
		w.gatewayScript,
		w.mcpFile,
		strconv.Itoa(w.gatewayPort),
		w.gatewayLog,
	}
	result, err := w.backend.Exec(ctx, launch, ExecOptions{CWD: w.workdir})
	if err != nil {
		return nil, nil, fmt.Errorf("workspace/sandboxed: launch gateway: %w", err)
	}
	if !result.OK() {
		return nil, nil, commandError("launch gateway", result)
	}

	gate := &operationGate{}
	client, err := w.gatewayFactory(&gatewayBackend{Backend: w.backend, gate: gate}, w.gatewayPort, w.gatewayTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("workspace/sandboxed: create gateway client: %w", err)
	}
	if client == nil {
		return nil, nil, fmt.Errorf("workspace/sandboxed: gateway factory returned nil client")
	}
	if err := w.waitForGateway(ctx, client); err != nil {
		return nil, nil, err
	}
	return client, gate, nil
}

func (w *Workspace) bootstrapGateway(ctx context.Context) error {
	current, err := w.gatewayBootstrapCurrent(ctx)
	if err != nil {
		return err
	}
	if current {
		return nil
	}
	for index, argv := range w.bootstrapCommands {
		result, execErr := w.backend.Exec(ctx, argv, ExecOptions{
			CWD:     w.workdir,
			Timeout: 10 * time.Minute,
		})
		if execErr != nil {
			return fmt.Errorf("workspace/sandboxed: bootstrap command %d: %w", index, execErr)
		}
		if !result.OK() {
			return commandError(fmt.Sprintf("bootstrap command %d", index), result)
		}
	}
	// Write the bootstrap marker last so an interrupted bootstrap is retried in full.
	if err := w.backend.WriteFile(ctx, w.gatewayScript, gatewayAppScript); err != nil {
		return fmt.Errorf("workspace/sandboxed: upload gateway script: %w", err)
	}
	if err := w.backend.WriteFile(ctx, w.gatewayMarker, []byte(w.gatewayVersion+"\n")); err != nil {
		return fmt.Errorf("workspace/sandboxed: write gateway bootstrap marker: %w", err)
	}
	return nil
}

func (w *Workspace) waitForGateway(ctx context.Context, client Gateway) error {
	deadline := time.NewTimer(w.gatewayTimeout)
	defer deadline.Stop()
	delay := 100 * time.Millisecond
	var lastErr error
	for {
		if err := client.Bootstrap(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-deadline.C:
			timer.Stop()
			logCtx, cancel := w.detachedContext(ctx)
			logTail := w.gatewayLogTail(logCtx, 2000)
			cancel()
			return fmt.Errorf(
				"workspace/sandboxed: gateway did not become healthy within %s: %w; log tail: %s",
				w.gatewayTimeout,
				lastErr,
				logTail,
			)
		case <-timer.C:
		}
		if delay < time.Second {
			delay = min(delay*3/2, time.Second)
		}
	}
}

func (w *Workspace) gatewayBootstrapCurrent(ctx context.Context) (bool, error) {
	for _, filename := range []string{w.gatewayPython, w.gatewayScript, w.gatewayMarker} {
		exists, err := w.fileExists(ctx, filename)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}
	script, err := w.backend.ReadFile(ctx, w.gatewayScript)
	if err != nil {
		return false, fmt.Errorf("workspace/sandboxed: read gateway script: %w", err)
	}
	if string(script) != string(gatewayAppScript) {
		return false, nil
	}
	marker, err := w.backend.ReadFile(ctx, w.gatewayMarker)
	if err != nil {
		return false, fmt.Errorf("workspace/sandboxed: read gateway bootstrap marker: %w", err)
	}
	return strings.TrimSpace(string(marker)) == w.gatewayVersion, nil
}

func bootstrapFingerprint(commands [][]string) string {
	digest := sha256.New()
	writeFingerprintPart(digest, gatewayAppScript)
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(commands)))
	_, _ = digest.Write(count[:])
	for _, command := range commands {
		binary.BigEndian.PutUint64(count[:], uint64(len(command)))
		_, _ = digest.Write(count[:])
		for _, argument := range command {
			writeFingerprintPart(digest, []byte(argument))
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writeFingerprintPart(digest interface{ Write([]byte) (int, error) }, data []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(data)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write(data)
}

func (w *Workspace) detachedContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := defaultGatewayTimeout
	if w != nil && w.gatewayTimeout > 0 {
		timeout = min(w.gatewayTimeout, defaultGatewayTimeout)
	}
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

func (w *Workspace) fileExists(ctx context.Context, filename string) (bool, error) {
	result, err := w.backend.Exec(ctx, []string{"test", "-f", filename}, ExecOptions{CWD: "/"})
	if err != nil {
		return false, fmt.Errorf("workspace/sandboxed: test file %q: %w", filename, err)
	}
	switch result.ExitCode {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, commandError("test file "+filename, result)
	}
}

func (w *Workspace) deleteTrees(ctx context.Context, paths ...string) error {
	argv := make([]string, 0, 4+len(paths))
	argv = append(argv, "sh", "-c", deleteTreeScript, "--")
	argv = append(argv, paths...)
	result, err := w.backend.Exec(ctx, argv, ExecOptions{CWD: "/"})
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: clear workspace state: %w", err)
	}
	if !result.OK() {
		return commandError("clear workspace state", result)
	}
	return nil
}

func (w *Workspace) saveMCPFileLocked(ctx context.Context) error {
	configs := make([]workspace.MCPClientConfig, 0, len(w.mcps))
	for _, client := range w.mcps {
		config, err := mcpConfig(client)
		if err != nil {
			return err
		}
		configs = append(configs, config)
	}
	data, err := w.mcpCodec.Marshal(configs)
	if err != nil {
		return fmt.Errorf("workspace/sandboxed: encode MCP file: %w", err)
	}
	if err := w.backend.WriteFile(ctx, w.mcpFile, data); err != nil {
		return fmt.Errorf("workspace/sandboxed: write MCP file: %w", err)
	}
	return nil
}

func (w *Workspace) findMCPLocked(name string) int {
	for index, client := range w.mcps {
		if client != nil && client.Name() == name {
			return index
		}
	}
	return -1
}

func (w *Workspace) gatewayLogTail(ctx context.Context, limit int) string {
	data, err := w.backend.ReadFile(ctx, w.gatewayLog)
	if err != nil {
		return "<unavailable>"
	}
	if limit > 0 && len(data) > limit {
		data = data[len(data)-limit:]
	}
	return strings.TrimSpace(string(data))
}

func commandError(operation string, result ExecResult) error {
	detail := strings.TrimSpace(string(result.Stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(result.Stdout))
	}
	if len(detail) > 2000 {
		detail = detail[len(detail)-2000:]
	}
	return fmt.Errorf(
		"workspace/sandboxed: %s failed with exit code %d: %s",
		operation,
		result.ExitCode,
		detail,
	)
}

func absoluteSandboxPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("path contains NUL byte")
	}
	if !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	cleaned := path.Clean(value)
	if cleaned == "/" {
		return "", fmt.Errorf("root directory is not a valid workspace path")
	}
	return cleaned, nil
}

func cloneArgvList(commands [][]string) [][]string {
	if len(commands) == 0 {
		return nil
	}
	out := make([][]string, len(commands))
	for index, command := range commands {
		out[index] = append([]string(nil), command...)
	}
	return out
}

var (
	_ workspace.Workspace       = (*Workspace)(nil)
	_ workspace.RootedWorkspace = (*Workspace)(nil)
	_ skill.Loader              = (*remoteSkillLoader)(nil)
)
