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

package workspace

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/builtin"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/skill"
)

// AgentResources is the workspace materialized into resources an Agent can consume directly.
type AgentResources struct {
	SystemPrompt string
	Toolkit      *tool.Toolkit
	Tools        []Tool
	MCPTools     []Tool
	Skills       []skill.Skill
	Offloader    Offloader
}

// BuildAgentResources initializes a workspace and converts it to Agent-consumable prompt, toolkit, and offloader resources.
func BuildAgentResources(ctx context.Context, workspace Workspace) (*AgentResources, error) {
	if workspace == nil {
		return nil, fmt.Errorf("workspace: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !workspace.IsAlive() {
		if err := workspace.Initialize(ctx); err != nil {
			return nil, err
		}
	}
	instructions, err := workspace.GetInstructions(ctx)
	if err != nil {
		return nil, err
	}
	workspaceTools, err := workspace.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	mcps, err := workspace.ListMCPs(ctx)
	if err != nil {
		return nil, err
	}
	mcpTools, err := listMCPTools(ctx, mcps)
	if err != nil {
		return nil, err
	}
	skills, err := workspace.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	allTools := append([]Tool{}, workspaceTools...)
	allTools = append(allTools, mcpTools...)
	if len(skills) > 0 {
		allTools = append(allTools, builtin.NewSkillViewer(skills))
	}
	kit, err := tool.NewToolkit(allTools...)
	if err != nil {
		return nil, err
	}
	systemPrompt := strings.TrimSpace(instructions)
	if skillPrompt := FormatSkillInstructions(skills); skillPrompt != "" {
		if systemPrompt != "" {
			systemPrompt += "\n"
		}
		systemPrompt += skillPrompt
	}
	return &AgentResources{
		SystemPrompt: systemPrompt,
		Toolkit:      kit,
		Tools:        append([]Tool(nil), workspaceTools...),
		MCPTools:     append([]Tool(nil), mcpTools...),
		Skills:       append([]skill.Skill(nil), skills...),
		Offloader:    workspace,
	}, nil
}

func listMCPTools(ctx context.Context, mcps []MCPClient) ([]Tool, error) {
	mcpTools := []Tool{}
	for _, client := range mcps {
		if client == nil {
			continue
		}
		if client.IsStateful() && !client.IsConnected() {
			connectErr := client.Connect(ctx)
			if connectErr != nil {
				return nil, connectErr
			}
		}
		tools, listErr := client.ListTools(ctx)
		if listErr != nil {
			return nil, listErr
		}
		mcpTools = append(mcpTools, tools...)
	}
	return mcpTools, nil
}

// FormatSkillInstructions returns the skill prompt fragment aligned with the Python implementation.
func FormatSkillInstructions(skills []skill.Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("<agent-skills>\n")
	builder.WriteString("Skills are a collection of instructions, scripts, and resources to extend your capabilities.\n\n")
	builder.WriteString("IMPORTANT: Skills are NOT tools, and you cannot call a skill directly. To use a skill, call the `Skill` tool to read the skill's full instructions, then follow those instructions.\n\n")
	builder.WriteString("# Available Skills:")
	for _, current := range skills {
		builder.WriteString("\n<skill>\n")
		builder.WriteString("<name>")
		builder.WriteString(html.EscapeString(current.Name))
		builder.WriteString("</name>\n<description>")
		builder.WriteString(html.EscapeString(current.Description))
		builder.WriteString("</description>\n<dir>")
		builder.WriteString(html.EscapeString(current.Dir))
		builder.WriteString("</dir>\n</skill>")
	}
	builder.WriteString("\n</agent-skills>")
	return builder.String()
}
