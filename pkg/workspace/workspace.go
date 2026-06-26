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

// Package workspace manages execution workspaces, tools, skills, and offload storage.
package workspace

import (
	"context"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/skill"
)

type (
	// Tool is the tool interface exposed by a workspace to an Agent.
	Tool = tool.Tool

	// ToolSchema is the model-facing JSON Schema for one tool.
	ToolSchema = model.ToolSchema

	// Skill is one Agent skill loaded from a workspace.
	Skill = skill.Skill
)

// MCPClient is the minimal contract a workspace needs to track MCP clients and expose MCP tools.
type MCPClient interface {
	// Name returns the stable MCP name used for registration, removal, and diagnostics.
	Name() string
	// IsStateful reports whether this MCP requires a persistent connection.
	IsStateful() bool
	// IsConnected reports whether a stateful MCP is currently connected.
	IsConnected() bool
	// Connect starts a persistent MCP connection. Stateless MCP clients may no-op.
	Connect(context.Context) error
	// Close releases a persistent MCP connection. Stateless MCP clients may no-op.
	Close() error
	// ListTools returns AgentScope tools exposed by this MCP.
	ListTools(context.Context) ([]Tool, error)
}

// MCPClientType identifies a serializable workspace MCP transport.
type MCPClientType string

const (
	// MCPClientTypeStdio describes a subprocess-backed MCP server.
	MCPClientTypeStdio MCPClientType = "stdio_mcp"
	// MCPClientTypeHTTP describes an HTTP MCP server.
	MCPClientTypeHTTP MCPClientType = "http_mcp"
)

// MCPStdioConfig is the persisted workspace config for a stdio MCP server.
type MCPStdioConfig struct {
	Command              string            `json:"command"`
	Args                 []string          `json:"args,omitempty"`
	Env                  map[string]string `json:"env,omitempty"`
	CWD                  string            `json:"cwd,omitempty"`
	EncodingErrorHandler string            `json:"encoding_error_handler,omitempty"`
}

// MCPHTTPConfig is the persisted workspace config for an HTTP MCP server.
type MCPHTTPConfig struct {
	URL                 string            `json:"url"`
	Headers             map[string]string `json:"headers,omitempty"`
	Timeout             time.Duration     `json:"timeout,omitempty"`
	Transport           string            `json:"transport,omitempty"`
	ContinuousListening bool              `json:"continuous_listening,omitempty"`
}

// MCPClientConfig is the stable JSON config written to workspace indexes and sent to gateways.
type MCPClientConfig struct {
	Name             string          `json:"name"`
	Type             MCPClientType   `json:"type"`
	Stateful         bool            `json:"is_stateful"`
	Stdio            *MCPStdioConfig `json:"stdio,omitempty"`
	HTTP             *MCPHTTPConfig  `json:"http,omitempty"`
	EnabledTools     []string        `json:"enable_tools,omitempty"`
	DisabledTools    []string        `json:"disable_tools,omitempty"`
	ExecutionTimeout time.Duration   `json:"execution_timeout,omitempty"`
}

// MCPConfigProvider exposes a serializable MCP config for workspace persistence.
type MCPConfigProvider interface {
	MCPClientConfig() (MCPClientConfig, error)
}

// MCPClientFactory restores an MCP client from persisted config.
type MCPClientFactory func(MCPClientConfig) (MCPClient, error)

// Offloader persists data that should leave the active model context.
type Offloader interface {
	// OffloadContext persists compressed or oversized context and returns a reference.
	OffloadContext(context.Context, string, []*message.Message) (string, error)
	// OffloadToolResult persists the omitted portion of a tool result and returns a reference.
	OffloadToolResult(context.Context, string, *message.ToolResultBlock) (string, error)
	// OffloadDataBlock persists a base64 DataBlock and returns a URL-backed block.
	OffloadDataBlock(context.Context, *message.DataBlock) (*message.DataBlock, error)
}

// Workspace describes the lifecycle and resources of an agent workspace.
type Workspace interface {
	// WorkspaceID returns the stable workspace identifier used in paths, logs, and offload references.
	WorkspaceID() string
	// IsAlive reports whether the workspace has been initialized and not yet closed.
	IsAlive() bool
	// Initialize prepares the workspace directories, seed resources, and any backing runtime.
	Initialize(context.Context) error
	// Close releases workspace resources without deleting reusable persisted state unless the implementation documents otherwise.
	Close(context.Context) error
	// Reset clears runtime state so later tool calls start from a clean workspace.
	Reset(context.Context) error
	// GetInstructions returns system-prompt guidance that should be given to agents using this workspace.
	GetInstructions(context.Context) (string, error)
	// ListTools returns the tools exposed by this workspace, such as local file tools or runtime gateway tools.
	ListTools(context.Context) ([]Tool, error)
	// ListMCPs returns MCP clients currently registered with this workspace.
	ListMCPs(context.Context) ([]MCPClient, error)
	// ListSkills returns skills currently available inside this workspace.
	ListSkills(context.Context) ([]skill.Skill, error)
	Offloader
	// AddMCP registers an MCP client so its tools can be recovered or exposed through the workspace.
	AddMCP(context.Context, MCPClient) error
	// RemoveMCP unregisters an MCP client by name.
	RemoveMCP(context.Context, string) error
	// AddSkill loads or links a skill directory into the workspace.
	AddSkill(context.Context, string) error
	// RemoveSkill removes a skill from the workspace by name or path, depending on the implementation.
	RemoveSkill(context.Context, string) error
}

// RootedWorkspace optionally reports the agent-visible root for file operations.
type RootedWorkspace interface {
	WorkspaceRoot() string
}
