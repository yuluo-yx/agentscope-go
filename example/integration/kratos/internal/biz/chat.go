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

package biz

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

// StructuredOutput is the domain result for a structured model response.
type StructuredOutput struct {
	Output map[string]any
	Raw    string
}

// ChatUsecase owns AgentScope Go model, tool, and agent orchestration.
type ChatUsecase struct{}

// NewChatUsecase creates the Kratos business usecase for the chat example.
func NewChatUsecase() *ChatUsecase {
	return &ChatUsecase{}
}

// Chat runs one non-streaming ChatModel request.
func (uc *ChatUsecase) Chat(ctx context.Context, prompt string) (message.ContentBlockList, error) {
	chatModel, err := newChatModel(false)
	if err != nil {
		return nil, err
	}
	user, err := message.NewUserMessage("user", promptOrDefault(prompt, "hello"))
	if err != nil {
		return nil, err
	}
	response, err := chatModel.Call(ctx, asmodel.CallRequest{
		Messages: []*message.Message{user},
	})
	if err != nil {
		return nil, err
	}
	return response.Content, nil
}

// StreamChat runs one streaming ChatModel request.
func (uc *ChatUsecase) StreamChat(ctx context.Context, prompt string, emit func(message.ContentBlockList, bool) error) error {
	chatModel, err := newChatModel(true)
	if err != nil {
		return err
	}
	user, err := message.NewUserMessage("user", promptOrDefault(prompt, "hello"))
	if err != nil {
		return err
	}
	ch, err := chatModel.Stream(ctx, asmodel.CallRequest{
		Messages: []*message.Message{user},
	})
	if err != nil {
		return err
	}
	for response := range ch {
		if response.Error != nil {
			return response.Error
		}
		if err := emit(response.Content, response.IsLast); err != nil {
			return err
		}
	}
	return nil
}

// StreamChatTool lets the ChatModel decide a tool call, executes it, then streams the final answer.
func (uc *ChatUsecase) StreamChatTool(ctx context.Context, prompt string, emit func(message.ContentBlockList, bool) error) error {
	chatModel, err := newChatModel(true)
	if err != nil {
		return err
	}
	user, err := message.NewUserMessage("user", promptOrDefault(prompt, "杭州天气怎么样？"))
	if err != nil {
		return err
	}
	wt, err := weatherTool()
	if err != nil {
		return err
	}
	toolkit, err := tool.NewToolkit(wt)
	if err != nil {
		return err
	}
	toolSchemas, err := toolkit.ToolSchemas()
	if err != nil {
		return err
	}

	ch, err := chatModel.Stream(ctx, asmodel.CallRequest{
		Messages: []*message.Message{user},
		Tools:    toolSchemas,
	})
	if err != nil {
		return err
	}

	var final *asmodel.ChatResponse
	for response := range ch {
		if response.Error != nil {
			return response.Error
		}
		if response.IsLast {
			final = response.Clone()
		}
		if err := emit(response.Content, response.IsLast); err != nil {
			return err
		}
	}
	if final == nil {
		return nil
	}

	var weatherCall *message.ToolCallBlock
	for _, block := range final.Content {
		if toolCall, ok := block.(*message.ToolCallBlock); ok {
			weatherCall = toolCall
			break
		}
	}
	if weatherCall == nil {
		return nil
	}

	toolResponse, err := toolkit.RunTool(ctx, weatherCall, asstate.NewAgentState())
	if err != nil {
		return err
	}
	if err := emit(toolResponse.Content, false); err != nil {
		return err
	}

	assistantMessage, err := message.NewAssistantMessage("assistant", final.Content)
	if err != nil {
		return err
	}
	toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
		message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
	})
	if err != nil {
		return err
	}
	answer, err := chatModel.Stream(ctx, asmodel.CallRequest{
		Messages: []*message.Message{user, assistantMessage, toolMessage},
		Tools:    toolSchemas,
	})
	if err != nil {
		return err
	}
	for response := range answer {
		if response.Error != nil {
			return response.Error
		}
		if err := emit(response.Content, response.IsLast); err != nil {
			return err
		}
	}
	return nil
}

// AgentChat runs the Agent path, where AgentScope Go executes allowed read-only tools automatically.
func (uc *ChatUsecase) AgentChat(ctx context.Context, prompt string) (message.ContentBlockList, error) {
	agent, err := newJourneyAgent(false)
	if err != nil {
		return nil, err
	}
	user, err := message.NewUserMessage("user", promptOrDefault(prompt, "看下杭州天气，帮我规划 plan"))
	if err != nil {
		return nil, err
	}
	reply, err := agent.Reply(ctx, user)
	if err != nil {
		return nil, err
	}
	return reply.Content, nil
}

// AgentStreamChat streams the Agent event path, including automatic tool-call events.
func (uc *ChatUsecase) AgentStreamChat(ctx context.Context, prompt string, emit func(message.Event) error) error {
	agent, err := newJourneyAgent(true)
	if err != nil {
		return err
	}
	user, err := message.NewUserMessage("user", promptOrDefault(prompt, "看下杭州天气，帮我规划 plan"))
	if err != nil {
		return err
	}
	return agent.ReplyStream(ctx, user, emit)
}

// StructuredOutput asks the model for JSON and parses it into a map.
func (uc *ChatUsecase) StructuredOutput(ctx context.Context, prompt string) (*StructuredOutput, error) {
	chatModel, err := newChatModel(false)
	if err != nil {
		return nil, err
	}
	user, err := message.NewUserMessage("user", fmt.Sprintf(`Return only valid compact JSON with this schema:
{"city":"string","weather":"string","plan":["string"],"tips":["string"]}
User request: %s`, promptOrDefault(prompt, "杭州一日游")))
	if err != nil {
		return nil, err
	}
	response, err := chatModel.Call(ctx, asmodel.CallRequest{
		Messages: []*message.Message{user},
	})
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(textContent(response.Content))
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= start {
			raw = raw[start : end+1]
		}
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, fmt.Errorf("parse structured output: %w; raw=%s", err, raw)
	}
	return &StructuredOutput{Output: output, Raw: raw}, nil
}

func newJourneyAgent(stream bool) (*agentpkg.Agent, error) {
	wt, err := weatherTool()
	if err != nil {
		return nil, err
	}
	toolkit, err := tool.NewToolkit(wt)
	if err != nil {
		return nil, err
	}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeExplore)
	chatModel, err := newChatModel(stream)
	if err != nil {
		return nil, err
	}
	return agentpkg.NewAgent(
		"Journey Agent",
		"Use GetWeather before answering weather or travel-planning questions.",
		chatModel,
		agentpkg.WithToolkit(toolkit),
		agentpkg.WithAgentState(state),
	)
}

func newChatModel(stream bool) (asmodel.ChatModel, error) {
	return dashscope.NewChatModel(
		dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")),
		"qwen3.7-max",
		dashscope.WithStream(stream),
	)
}

func weatherTool() (*tool.FunctionTool, error) {
	return tool.NewFunctionTool(
		"GetWeather",
		"Return weather for one city.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{"type": "string", "description": "City name."},
			},
			"required": []any{"city"},
		},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("sunny")}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
}

func promptOrDefault(prompt, fallback string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fallback
	}
	return prompt
}

func textContent(blocks message.ContentBlockList) string {
	var builder strings.Builder
	for _, block := range blocks {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}
