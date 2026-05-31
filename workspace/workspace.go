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
	Name() string
}

// Workspace describes the lifecycle and resources of an agent workspace.
type Workspace interface {
	WorkspaceID() string
	IsAlive() bool
	Initialize(context.Context) error
	Close(context.Context) error
	Reset(context.Context) error
	GetInstructions(context.Context) (string, error)
	ListTools(context.Context) ([]Tool, error)
	ListMCPs(context.Context) ([]MCPClient, error)
	ListSkills(context.Context) ([]skill.Skill, error)
	OffloadContext(context.Context, string, []*message.Message) (string, error)
	OffloadToolResult(context.Context, string, *message.ToolResultBlock) (string, error)
	AddMCP(context.Context, MCPClient) error
	RemoveMCP(context.Context, string) error
	AddSkill(context.Context, string) error
	RemoveSkill(context.Context, string) error
}
