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
	"sort"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/tool/skill"
)

type scriptedChatModel struct {
	responses []*asmodel.ChatResponse
}

func (m *scriptedChatModel) Name() string {
	return "scripted-skill-example"
}

func (m *scriptedChatModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return m.nextResponse()
}

func (m *scriptedChatModel) Stream(ctx context.Context, _ asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	response, err := m.nextResponse()
	if err != nil {
		return nil, err
	}
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		delta := response.Clone()
		delta.IsLast = false
		delta.Usage = nil
		select {
		case <-ctx.Done():
			return
		case out <- *delta:
		}
		select {
		case <-ctx.Done():
		case out <- *response:
		}
	}()
	return out, nil
}

func (m *scriptedChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *scriptedChatModel) nextResponse() (*asmodel.ChatResponse, error) {
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func main() {
	ctx := context.Background()
	loader := skill.NewLocalLoader("resources", skill.WithScanSubdirs(true))
	loadedSkills, err := loader.ListSkills(ctx)
	if err != nil {
		panic(err)
	}
	if len(loadedSkills) == 0 {
		panic("no skills loaded from resources")
	}

	skillTools := mustSkillTools(loadedSkills)
	kit := mustToolkit(tool.NewToolkit(skillTools...))
	skillToolName := pickSkillToolName(loadedSkills)

	model := &scriptedChatModel{
		responses: []*asmodel.ChatResponse{
			asmodel.NewChatResponse(
				message.ContentBlockList{
					message.NewToolCallBlock(
						"skill-call",
						skillToolName,
						`{"question":"How should I organize implementation work?"}`,
					),
				},
				true,
			),
			asmodel.NewChatResponse(
				message.ContentBlockList{message.NewTextBlock(fmt.Sprintf("I used %s and summarized its guidance.", skillToolName))},
				true,
			),
		},
	}

	agent := mustAgent(agentpkg.NewAgent("Friday", "Use skill tools when they help.", model, agentpkg.WithToolkit(kit)))
	user := mustMessage(message.NewUserMessage("user", "Use a skill to plan this task."))

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
		panic(err)
	}

	skillNames := make([]string, 0, len(loadedSkills))
	for _, item := range loadedSkills {
		skillNames = append(skillNames, item.Name)
	}
	sort.Strings(skillNames)
	fmt.Printf("skills=%d names=%s agent_reply=%s events=%s\n", len(loadedSkills), strings.Join(skillNames, ","), replyText.String(), strings.Join(events, ","))
}

func mustSkillTools(skills []skill.Skill) []tool.Tool {
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
		skillTool := mustFunctionTool(tool.NewFunctionTool(
			toolName,
			"Reads one loaded skill and returns its description and markdown body.",
			schema,
			handler,
			tool.WithFunctionReadOnly(true),
			tool.WithFunctionPermissionFunc(permissionFn),
		))
		tools = append(tools, skillTool)
	}
	return tools
}

func pickSkillToolName(skills []skill.Skill) string {
	for _, loaded := range skills {
		if loaded.Name == "planning" {
			return "Skill_planning"
		}
	}
	return "Skill_" + skills[0].Name
}

func mustFunctionTool(value *tool.FunctionTool, err error) *tool.FunctionTool {
	if err != nil {
		panic(err)
	}
	return value
}

func mustToolkit(value *tool.Toolkit, err error) *tool.Toolkit {
	if err != nil {
		panic(err)
	}
	return value
}

func mustAgent(value *agentpkg.Agent, err error) *agentpkg.Agent {
	if err != nil {
		panic(err)
	}
	return value
}

func mustMessage(value *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return value
}
