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
	"strings"

	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "tool function example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	greet, err := tool.NewFunctionTool(
		"Greet",
		"Return a greeting for one name.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"required": []any{"name"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			name, _ := input["name"].(string)
			if name == "" {
				name = "AgentScope"
			}
			return message.ContentBlockList{message.NewTextBlock("hello " + name)}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
	if err != nil {
		return fmt.Errorf("create Greet tool: %w", err)
	}

	response, err := runTool(greet, map[string]any{"name": "Go"}, nil)
	if err != nil {
		return err
	}
	output := ""
	if text := response.GetTextContent(); text != nil {
		output = *text
	}
	fmt.Printf("function_tool=%s state=%s output=%q\n", greet.Name(), response.State, output)
	kit, err := tool.NewToolkit(greet)
	if err != nil {
		return fmt.Errorf("create toolkit: %w", err)
	}
	result, err := runDashScopeToolCall(ctx, kit, asstate.NewAgentState(), "Use the Greet tool to greet Go, then answer with the greeting.")
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func runTool(t tool.Tool, input map[string]any, state *asstate.AgentState) (*tool.ToolResponse, error) {
	chunks, err := t.Execute(context.Background(), input, state)
	if err != nil {
		return nil, fmt.Errorf("execute %s tool: %w", t.Name(), err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			return nil, fmt.Errorf("append %s tool chunk: %w", t.Name(), err)
		}
	}
	return response, nil
}

func runDashScopeToolCall(ctx context.Context, kit *tool.Toolkit, state *asstate.AgentState, prompt string) (string, error) {
	schemas, err := kit.ToolSchemas()
	if err != nil {
		return "", fmt.Errorf("build tool schemas: %w", err)
	}
	maxTokens := int64(256)
	temperature := 0.2
	apiKey := os.Getenv("AI_DASHSCOPE_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}
	chat, err := dashscope.NewChatModel(
		credential.NewDashScope(apiKey).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(false),
		dashscope.WithChatParameters(dashscope.ChatParameters{MaxTokens: &maxTokens, Temperature: &temperature}),
	)
	if err != nil {
		return "", fmt.Errorf("create DashScope chat model: %w", err)
	}
	user, err := message.NewUserMessage("user", prompt)
	if err != nil {
		return "", fmt.Errorf("create user message: %w", err)
	}
	messages := []*message.Message{user}
	var lastToolCall *message.ToolCallBlock
	var lastToolResponse *tool.ToolResponse
	for turn := 0; turn < 4; turn++ {
		response, err := chat.Call(ctx, asmodel.CallRequest{Messages: messages, Tools: schemas})
		if err != nil {
			return "", fmt.Errorf("call DashScope turn %d: %w", turn+1, err)
		}
		toolCall := firstToolCall(response.Content)
		if toolCall == nil {
			text := ""
			if responseText := response.GetTextContent(); responseText != nil {
				text = *responseText
			}
			if lastToolCall == nil {
				return "", fmt.Errorf("DashScope returned no tool call: %q", text)
			}
			if strings.TrimSpace(text) == "" {
				return "", fmt.Errorf("DashScope returned empty final text after %s", lastToolCall.Name)
			}
			return fmt.Sprintf("chat_tool=%s mode=live chat_model=%s input=%s state=%s response=%q", lastToolCall.Name, chat.Name(), lastToolCall.Input, lastToolResponse.State, shorten(text, 96)), nil
		}
		toolResponse, err := kit.RunTool(ctx, toolCall, state)
		if err != nil {
			return "", fmt.Errorf("run %s tool: %w", toolCall.Name, err)
		}
		assistantMessage, err := message.NewAssistantMessage("assistant", response.Content)
		if err != nil {
			return "", fmt.Errorf("create assistant message: %w", err)
		}
		toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
			message.NewToolResultBlock(toolCall.ID, toolCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
		})
		if err != nil {
			return "", fmt.Errorf("create tool result message: %w", err)
		}
		messages = append(messages, assistantMessage, toolMessage)
		lastToolCall = toolCall
		lastToolResponse = toolResponse
	}
	return "", fmt.Errorf("DashScope did not produce final text after tool calls")
}

func textOutputBlocks(blocks message.ContentBlockList) string {
	var builder strings.Builder
	for _, block := range blocks {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func firstToolCall(blocks message.ContentBlockList) *message.ToolCallBlock {
	for _, block := range blocks {
		if toolCall, ok := block.(*message.ToolCallBlock); ok {
			return toolCall
		}
	}
	return nil
}

func schemaNames(schemas []asmodel.ToolSchema) string {
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		names = append(names, schema.Function.Name)
	}
	return strings.Join(names, ",")
}

func shorten(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
