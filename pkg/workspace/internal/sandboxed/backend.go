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

// Package sandboxed provides the shared lifecycle for remote sandbox workspaces.
package sandboxed

import (
	"context"
	"fmt"
	"time"

	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

// ExecOptions describes one remote process execution.
type ExecOptions struct {
	CWD     string
	Env     map[string]string
	Timeout time.Duration
}

// ExecResult contains the remote process exit code and captured output.
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

// OK reports whether the remote process exited successfully.
func (r ExecResult) OK() bool {
	return r.ExitCode == 0
}

// Backend is the minimal set of sandbox primitives required by the remote lifecycle.
//
// Exec must preserve argv boundaries. Callers pass ["sh", "-c", script, ...]
// only when shell semantics are explicitly required. WriteFile must create
// parent directories as needed.
type Backend interface {
	Exec(context.Context, []string, ExecOptions) (ExecResult, error)
	ReadFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte) error
}

// LimitedFileReader lets a Backend enforce a byte limit before materializing a remote file.
type LimitedFileReader interface {
	ReadFileLimit(context.Context, string, int64) ([]byte, error)
}

// Provider creates or connects to a remote runtime and preserves or releases it on close.
type Provider interface {
	Open(context.Context) (Backend, error)
	Close(context.Context) error
}

// Gateway is the minimal MCP gateway contract required by the shared lifecycle.
type Gateway interface {
	Bootstrap(context.Context) error
	AddMCP(context.Context, workspace.MCPClientConfig) error
	RemoveMCP(context.Context, string) error
	ListMCPs(context.Context) ([]workspace.MCPClientConfig, error)
	NewMCPClient(workspace.MCPClientConfig, bool) workspace.MCPClient
	Close(context.Context) error
}

// GatewayFactory creates a loopback gateway client from a remote Backend.
type GatewayFactory func(Backend, int, time.Duration) (Gateway, error)

// MCPCodec reads and writes canonical Python `.mcp` data while accepting the legacy Go format.
type MCPCodec interface {
	Marshal([]workspace.MCPClientConfig) ([]byte, error)
	Unmarshal([]byte) ([]workspace.MCPClientConfig, error)
}

// Config configures the shared remote Workspace lifecycle.
type Config struct {
	ID                string
	Workdir           string
	GatewayHome       string
	GatewayPort       int
	GatewayTimeout    time.Duration
	Instructions      string
	Provider          Provider
	GatewayFactory    GatewayFactory
	MCPCodec          MCPCodec
	BootstrapCommands [][]string
	DefaultMCPs       []workspace.MCPClient
	SkillPaths        []string
}

func mcpConfig(client workspace.MCPClient) (workspace.MCPClientConfig, error) {
	if client == nil {
		return workspace.MCPClientConfig{}, fmt.Errorf("workspace/sandboxed: nil MCP client")
	}
	provider, ok := client.(workspace.MCPConfigProvider)
	if !ok {
		return workspace.MCPClientConfig{}, fmt.Errorf(
			"workspace/sandboxed: MCP %q cannot be persisted",
			client.Name(),
		)
	}
	config, err := provider.MCPClientConfig()
	if err != nil {
		return workspace.MCPClientConfig{}, err
	}
	if config.Name != client.Name() {
		return workspace.MCPClientConfig{}, fmt.Errorf(
			"workspace/sandboxed: MCP client name %q does not match persisted name %q",
			client.Name(),
			config.Name,
		)
	}
	return cloneMCPClientConfig(config), nil
}

func cloneMCPClientConfig(config workspace.MCPClientConfig) workspace.MCPClientConfig {
	out := config
	if config.Stdio != nil {
		stdio := *config.Stdio
		stdio.Args = append([]string(nil), config.Stdio.Args...)
		stdio.Env = cloneStringMap(config.Stdio.Env)
		out.Stdio = &stdio
	}
	if config.HTTP != nil {
		httpConfig := *config.HTTP
		httpConfig.Headers = cloneStringMap(config.HTTP.Headers)
		out.HTTP = &httpConfig
	}
	out.EnabledTools = append([]string(nil), config.EnabledTools...)
	out.DisabledTools = append([]string(nil), config.DisabledTools...)
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
