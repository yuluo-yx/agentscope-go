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
	tasktool "github.com/yuluo-yx/agentscope-go/tool/task"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "tool task example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	state := asstate.NewAgentState()
	create, err := runTool(tasktool.NewTaskCreate(), map[string]any{
		"subject":     "Create examples",
		"description": "Create standalone example modules.",
		"metadata":    map[string]any{"phase": "examples"},
	}, state)
	if err != nil {
		return err
	}
	if create.State != message.ToolResultSuccess {
		text := create.GetTextContent()
		if text == nil {
			return fmt.Errorf("TaskCreate returned no text content")
		}
		return fmt.Errorf("%s", *text)
	}
	if len(state.TaskContext.Tasks) == 0 {
		return fmt.Errorf("TaskCreate did not create a task")
	}

	taskID := state.TaskContext.Tasks[0].ID
	update, err := runTool(tasktool.NewTaskUpdate(), map[string]any{
		"task_id": taskID,
		"status":  "in_progress",
		"owner":   "example",
	}, state)
	if err != nil {
		return err
	}
	list, err := runTool(tasktool.NewTaskList(), nil, state)
	if err != nil {
		return err
	}
	get, err := runTool(tasktool.NewTaskGet(), map[string]any{"task_id": taskID}, state)
	if err != nil {
		return err
	}
	listText := list.GetTextContent()
	getText := get.GetTextContent()

	fmt.Printf(
		"task_tools=%s tasks=%d status=%s update=%s list_has_task=%t get_has_owner=%t\n",
		toolNames(tasktool.NewTools()),
		len(state.TaskContext.Tasks),
		state.TaskContext.Tasks[0].State,
		update.State,
		listText != nil && strings.Contains(*listText, "Create examples"),
		getText != nil && strings.Contains(*getText, "example"),
	)
	kit, err := tool.NewToolkit(tasktool.NewTaskGet())
	if err != nil {
		return fmt.Errorf("create toolkit: %w", err)
	}
	result, err := runDashScopeToolCall(ctx, kit, state, fmt.Sprintf("Use the TaskGet tool with task_id %s, then answer with one short sentence about the task.", taskID))
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

func toolNames(tools []tool.Tool) string {
	names := make([]string, 0, len(tools))
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return strings.Join(names, ",")
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
