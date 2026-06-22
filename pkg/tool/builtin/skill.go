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

package builtin

import (
	"context"
	"fmt"

	astate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/skill"
)

// SkillViewer reads full workspace skill instructions by the agent-facing name.
type SkillViewer struct {
	baseTool
	skills map[string]skill.Skill
}

// NewSkillViewer creates the built-in Skill tool for workspace skills.
func NewSkillViewer(skills []skill.Skill) *SkillViewer {
	index := make(map[string]skill.Skill, len(skills))
	for _, current := range skills {
		index[current.Name] = current
	}
	return &SkillViewer{
		baseTool: baseTool{
			name:            "Skill",
			description:     "Retrieve a skill within the conversation. When a task matches an available skill, read the skill's full instructions.",
			schema:          skillViewerSchema(),
			concurrencySafe: true,
			readOnly:        true,
		},
		skills: index,
	}
}

// Execute returns the requested skill Markdown body.
func (s *SkillViewer) Execute(_ context.Context, input map[string]any, _ *astate.AgentState) (<-chan astool.ToolChunk, error) {
	name, _ := input["skill"].(string)
	if name == "" {
		return errorText("SkillNotFoundError: Skill name is required."), nil
	}
	current, ok := s.skills[name]
	if !ok {
		return errorText(fmt.Sprintf("SkillNotFoundError: Skill %q not found.", name)), nil
	}
	return successText(current.Markdown), nil
}

func skillViewerSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": map[string]any{
				"type":        "string",
				"description": "The exact name of the skill to view.",
			},
		},
		"required": []any{"skill"},
	}
}

var _ astool.Tool = (*SkillViewer)(nil)
