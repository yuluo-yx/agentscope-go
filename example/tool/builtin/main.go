// Copyright 20\d\d AgentScope Go
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
	"path/filepath"
	"strings"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/tool/builtin"
)

func main() {
	dir := mustTempDir()
	defer func() { _ = os.RemoveAll(dir) }()

	state := asstate.NewAgentState()
	filePath := filepath.Join(dir, "note.txt")
	tools := []tool.Tool{
		builtin.NewBash(),
		builtin.NewEdit(),
		builtin.NewGlob(),
		builtin.NewGrep(),
		builtin.NewRead(),
		builtin.NewWrite(),
	}

	write := runTool(builtin.NewWrite(), map[string]any{"file_path": filePath, "content": "AgentScope Go\nbefore edit\n"}, state)
	read := runTool(builtin.NewRead(), map[string]any{"file_path": filePath}, state)
	edit := runTool(builtin.NewEdit(), map[string]any{"file_path": filePath, "old_string": "before edit", "new_string": "after edit"}, state)
	glob := runTool(builtin.NewGlob(), map[string]any{"pattern": "**/*.txt", "path": dir}, state)
	grep := runTool(builtin.NewGrep(), map[string]any{"pattern": "AgentScope", "path": dir, "glob": "*.txt"}, state)
	bash := runTool(builtin.NewBash(), map[string]any{"command": "printf shell-ok"}, state)

	fmt.Printf(
		"builtin_tools=%s write=%s read_has_text=%t edit=%s glob_has_file=%t grep_has_text=%t bash=%q\n",
		toolNames(tools),
		write.State,
		strings.Contains(textOutput(read), "AgentScope Go"),
		edit.State,
		strings.Contains(textOutput(glob), "note.txt"),
		strings.Contains(textOutput(grep), "AgentScope"),
		strings.TrimSpace(textOutput(bash)),
	)
	kit := mustToolkit(tool.NewToolkit(builtin.NewRead()))
	fmt.Println(runDashScopeToolCall(context.Background(), kit, state, fmt.Sprintf("Use the Read tool to read %s, then answer with one short sentence about the file.", filePath)))
}

func mustTempDir() string {
	dir, err := os.MkdirTemp("", "agentscope-builtin-example-*")
	if err != nil {
		panic(err)
	}
	return dir
}

func runTool(t tool.Tool, input map[string]any, state *asstate.AgentState) *tool.ToolResponse {
	chunks, err := t.Execute(context.Background(), input, state)
	if err != nil {
		panic(err)
	}
	response := tool.NewToolResponse("example-call")
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			panic(err)
		}
	}
	return response
}

func textOutput(response *tool.ToolResponse) string {
	var builder strings.Builder
	for _, block := range response.Content {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func toolNames(tools []tool.Tool) string {
	names := make([]string, 0, len(tools))
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return strings.Join(names, ",")
}

func runDashScopeToolCall(ctx context.Context, kit *tool.Toolkit, state *asstate.AgentState, prompt string) string {
	schemas, err := kit.ToolSchemas()
	if err != nil {
		panic(err)
	}
	apiKey := strings.TrimSpace(os.Getenv("AI_DASHSCOPE_API_KEY"))
	live := apiKey != ""
	if apiKey == "" {
		apiKey = "demo-dashscope-key"
	}
	maxTokens := int64(256)
	temperature := 0.2
	chat := mustModel(dashscope.NewChatModel(
		dashscope.NewCredential(apiKey),
		getenv("AI_DASHSCOPE_MODEL", "qwen3.7-max"),
		dashscope.WithStream(false),
		dashscope.WithChatParameters(dashscope.ChatParameters{MaxTokens: &maxTokens, Temperature: &temperature}),
	))
	user := mustMessage(message.NewUserMessage("user", prompt))
	request := asmodel.CallRequest{Messages: []*message.Message{user}, Tools: schemas}
	tokens, err := chat.CountTokens(request)
	if err != nil {
		panic(err)
	}
	if !live {
		return fmt.Sprintf("chat_tool=%s mode=offline chat_model=%s estimated_tokens=%d", schemaNames(schemas), chat.Name(), tokens)
	}
	messages := []*message.Message{user}
	var lastToolCall *message.ToolCallBlock
	var lastToolResponse *tool.ToolResponse
	for turn := 0; turn < 4; turn++ {
		response, err := chat.Call(ctx, asmodel.CallRequest{Messages: messages, Tools: schemas})
		if err != nil {
			panic(err)
		}
		toolCall := firstToolCall(response.Content)
		if toolCall == nil {
			if lastToolCall == nil {
				panic(fmt.Sprintf("DashScope returned no tool call: %q", textOutputBlocks(response.Content)))
			}
			text := textOutputBlocks(response.Content)
			if strings.TrimSpace(text) == "" {
				panic(fmt.Sprintf("DashScope returned empty final text after %s", lastToolCall.Name))
			}
			return fmt.Sprintf("chat_tool=%s mode=live chat_model=%s input=%s state=%s response=%q", lastToolCall.Name, chat.Name(), lastToolCall.Input, lastToolResponse.State, shorten(text, 96))
		}
		toolResponse, err := kit.RunTool(ctx, toolCall, state)
		if err != nil {
			panic(err)
		}
		assistantMessage := mustMessage(message.NewAssistantMessage("assistant", response.Content))
		toolMessage := mustMessage(message.NewAssistantMessage("tool", message.ContentBlockList{
			message.NewToolResultBlock(toolCall.ID, toolCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
		}))
		messages = append(messages, assistantMessage, toolMessage)
		lastToolCall = toolCall
		lastToolResponse = toolResponse
	}
	panic("DashScope did not produce final text after tool calls")
}

func getenv(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
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

func mustModel(model asmodel.ChatModel, err error) asmodel.ChatModel {
	if err != nil {
		panic(err)
	}
	return model
}

func mustToolkit(kit *tool.Toolkit, err error) *tool.Toolkit {
	if err != nil {
		panic(err)
	}
	return kit
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}
