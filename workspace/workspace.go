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

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/tool/skill"
)

// Tool is the workspace-visible tool interface.
type Tool = tool.Tool

// MCPClient is the minimal MCP client contract tracked by a workspace.
type MCPClient interface {
	// Name returns the stable MCP server name used for workspace registration and removal.
	Name() string
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
	// OffloadContext persists a compressed or oversized message context and returns a reference string.
	OffloadContext(context.Context, string, []*message.Message) (string, error)
	// OffloadToolResult persists a large tool result and returns a reference that can be kept in agent context.
	OffloadToolResult(context.Context, string, *message.ToolResultBlock) (string, error)
	// AddMCP registers an MCP client so its tools can be recovered or exposed through the workspace.
	AddMCP(context.Context, MCPClient) error
	// RemoveMCP unregisters an MCP client by name.
	RemoveMCP(context.Context, string) error
	// AddSkill loads or links a skill directory into the workspace.
	AddSkill(context.Context, string) error
	// RemoveSkill removes a skill from the workspace by name or path, depending on the implementation.
	RemoveSkill(context.Context, string) error
}
