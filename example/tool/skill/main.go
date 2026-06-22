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

package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/skill"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "tool skill example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	loader := skill.NewLocalLoader("resources", skill.WithScanSubdirs(true))
	loadedSkills, err := loader.ListSkills(ctx)
	if err != nil {
		return fmt.Errorf("list skills: %w", err)
	}
	if len(loadedSkills) == 0 {
		return fmt.Errorf("no skills loaded from resources")
	}

	toolsForSkills, err := skillTools(loadedSkills)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(toolsForSkills...)
	if err != nil {
		return fmt.Errorf("create toolkit: %w", err)
	}
	skillToolName := pickSkillToolName(loadedSkills)

	model, err := newDashScopeChatModel(true)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}

	agent, err := agentpkg.NewAgent("Friday", "Use skill tools when they help.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	user, err := message.NewUserMessage("user", fmt.Sprintf("Use %s to plan this task, then summarize the guidance.", skillToolName))
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}

	var replyText strings.Builder
	events := []string{}
	err = agent.ReplyStream(ctx, user, func(event message.Event) error {
		switch e := event.(type) {
		case *message.ToolCallStartEvent:
			events = append(events, "tool_call:"+e.ToolCallName)
		case *message.ToolResultEndEvent:
			events = append(events, "tool_result:"+string(e.State))
		case *message.TextBlockDeltaEvent:
			replyText.WriteString(e.Delta)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reply stream: %w", err)
	}

	skillNames := make([]string, 0, len(loadedSkills))
	for _, item := range loadedSkills {
		skillNames = append(skillNames, item.Name)
	}
	sort.Strings(skillNames)
	fmt.Printf("skills=%d names=%s agent_reply=%s events=%s\n", len(loadedSkills), strings.Join(skillNames, ","), replyText.String(), strings.Join(events, ","))
	return nil
}

func skillTools(skills []skill.Skill) ([]tool.Tool, error) {
	tools := make([]tool.Tool, 0, len(skills))
	for _, loaded := range skills {
		loaded := loaded
		toolName := "Skill_" + loaded.Name
		schema := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "Optional question for why this skill is being requested.",
				},
			},
		}
		handler := func(_ context.Context, input map[string]any, _ *agentpkg.AgentState) (message.ContentBlockList, error) {
			question, _ := input["question"].(string)
			text := fmt.Sprintf("skill=%s description=%s", loaded.Name, loaded.Description)
			if strings.TrimSpace(question) != "" {
				text += "\nquestion=" + question
			}
			text += "\nmarkdown:\n" + strings.TrimSpace(loaded.Markdown)
			return message.ContentBlockList{message.NewTextBlock(text)}, nil
		}
		permissionFn := func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{
				Behavior:       permission.BehaviorAllow,
				Message:        "Skill tool is allowed in this example.",
				DecisionReason: "Example keeps permissions simple to focus on agent flow",
			}, nil
		}
		skillTool, err := tool.NewFunctionTool(
			toolName,
			"Reads one loaded skill and returns its description and markdown body.",
			schema,
			handler,
			tool.WithFunctionReadOnly(true),
			tool.WithFunctionPermissionFunc(permissionFn),
		)
		if err != nil {
			return nil, fmt.Errorf("create %s skill tool: %w", loaded.Name, err)
		}
		tools = append(tools, skillTool)
	}
	return tools, nil
}

func pickSkillToolName(skills []skill.Skill) string {
	for _, loaded := range skills {
		if loaded.Name == "planning" {
			return "Skill_planning"
		}
	}
	return "Skill_" + skills[0].Name
}

func newDashScopeChatModel(stream bool) (*dashscope.ChatModel, error) {
	apiKey := os.Getenv("AI_DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}
	return dashscope.NewChatModel(
		credential.NewDashScope(apiKey).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(stream),
	)
}
