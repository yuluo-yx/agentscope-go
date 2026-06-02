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

	"github.com/yuluo-yx/agentscope-go/example/common/modelconfig"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func main() {
	greet := mustTool(tool.NewFunctionTool(
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
	))

	response := runTool(greet, map[string]any{"name": "Go"}, nil)
	output := ""
	if text := response.GetTextContent(); text != nil {
		output = *text
	}
	fmt.Printf("function_tool=%s state=%s output=%q\n", greet.Name(), response.State, output)
	kit := mustToolkit(tool.NewToolkit(greet))
	fmt.Println(runDashScopeToolCall(context.Background(), kit, asstate.NewAgentState(), "Use the Greet tool to greet Go, then answer with the greeting."))
}

func runTool(t tool.Tool, input map[string]any, state *asstate.AgentState) *tool.ToolResponse {
	chunks, err := t.Execute(context.Background(), input, state)
	if err != nil {
		panic(err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			panic(err)
		}
	}
	return response
}

func runDashScopeToolCall(ctx context.Context, kit *tool.Toolkit, state *asstate.AgentState, prompt string) string {
	schemas, err := kit.ToolSchemas()
	if err != nil {
		panic(err)
	}
	cfg := modelconfig.DashScope("qwen3.7-max")
	maxTokens := int64(256)
	temperature := 0.2
	chat := mustModel(dashscope.NewChatModel(
		dashscope.NewCredential(cfg.APIKey),
		cfg.Model,
		dashscope.WithStream(false),
		dashscope.WithChatParameters(dashscope.ChatParameters{MaxTokens: &maxTokens, Temperature: &temperature}),
	))
	user := mustMessage(message.NewUserMessage("user", prompt))
	request := asmodel.CallRequest{Messages: []*message.Message{user}, Tools: schemas}
	tokens, err := chat.CountTokens(request)
	if err != nil {
		panic(err)
	}
	if !cfg.Live {
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
			text := ""
			if responseText := response.GetTextContent(); responseText != nil {
				text = *responseText
			}
			if lastToolCall == nil {
				panic(fmt.Sprintf("DashScope returned no tool call: %q", text))
			}
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

func mustTool(t *tool.FunctionTool, err error) *tool.FunctionTool {
	if err != nil {
		panic(err)
	}
	return t
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
