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
	"github.com/yuluo-yx/agentscope-go/model/anthropic"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "anthropic chat example: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("start anthropic chat call: ------------------")
	if err := chat(); err != nil {
		return err
	}

	fmt.Println("\nstart anthropic stream chat call: ------------------")
	if err := streamChat(); err != nil {
		return err
	}
	return nil
}

func chat() error {
	chat, err := newAnthropicModel(false)
	if err != nil {
		return fmt.Errorf("create non-stream Anthropic model: %w", err)
	}

	weather, err := weatherTool()
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(weather)
	if err != nil {
		return fmt.Errorf("create toolkit: %w", err)
	}
	schemas, err := kit.ToolSchemas()
	if err != nil {
		return fmt.Errorf("build tool schemas: %w", err)
	}

	liveMessage, err := message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go.")
	if err != nil {
		return fmt.Errorf("create live message: %w", err)
	}
	liveRequest := asmodel.CallRequest{Messages: []*message.Message{liveMessage}}
	tokens, err := chat.CountTokens(liveRequest)
	if err != nil {
		return fmt.Errorf("count tokens: %w", err)
	}

	fmt.Printf("chat_model=%s anthropic_model=%s estimated_tokens=%d\n", chat.Name(), "claude-sonnet-4-5", tokens)

	ctx := context.Background()
	response, err := chat.Call(ctx, liveRequest)
	if err != nil {
		return fmt.Errorf("call live chat: %w", err)
	}
	fmt.Printf("anthropic_live=ok response=%q\n", shorten(textContent(response), 120))

	weatherMessage, err := message.NewUserMessage("user", "Use the GetWeather tool to answer: 杭州的天气怎么样？")
	if err != nil {
		return fmt.Errorf("create weather message: %w", err)
	}
	toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
		Messages: []*message.Message{weatherMessage},
		Tools:    schemas,
	})
	if err != nil {
		return fmt.Errorf("call weather chat: %w", err)
	}
	weatherCall := firstToolCall(toolCallResponse.Content)
	if weatherCall == nil {
		return fmt.Errorf("anthropic weather request returned no tool call: %q", shorten(textContent(toolCallResponse), 120))
	}
	toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
	if err != nil {
		return fmt.Errorf("run weather tool: %w", err)
	}

	assistantMessage, err := message.NewAssistantMessage("assistant", toolCallResponse.Content)
	if err != nil {
		return fmt.Errorf("create assistant message: %w", err)
	}
	toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
		message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
	})
	if err != nil {
		return fmt.Errorf("create tool result message: %w", err)
	}
	weatherResponse, err := chat.Call(ctx, asmodel.CallRequest{
		Messages: []*message.Message{weatherMessage, assistantMessage, toolMessage},
	})
	if err != nil {
		return fmt.Errorf("call final weather chat: %w", err)
	}

	fmt.Printf("anthropic_weather=ok tool=%s input=%s response=%q\n", weatherCall.Name, weatherCall.Input, shorten(textContent(weatherResponse), 120))
	return nil
}

func streamChat() error {
	streamChat, err := newAnthropicModel(true)
	if err != nil {
		return fmt.Errorf("create stream Anthropic model: %w", err)
	}

	user, err := message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go.")
	if err != nil {
		return fmt.Errorf("create stream user message: %w", err)
	}
	request := asmodel.CallRequest{Messages: []*message.Message{user}, Stream: true}
	tokens, err := streamChat.CountTokens(request)
	if err != nil {
		return fmt.Errorf("count stream tokens: %w", err)
	}

	fmt.Printf("chat_model=%s anthropic_model=%s estimated_tokens=%d\n", streamChat.Name(), "claude-sonnet-4-5", tokens)

	responses, err := streamChat.Stream(context.Background(), request)
	if err != nil {
		return fmt.Errorf("stream chat: %w", err)
	}
	var streamed strings.Builder
	var finalText string
	for response := range responses {
		if response.Error != nil {
			return fmt.Errorf("stream response: %w", response.Error)
		}
		text := textContent(&response)
		if response.IsLast {
			finalText = text
			continue
		}
		if text != "" {
			streamed.WriteString(text)
			fmt.Printf("anthropic_stream_delta=%q\n", shorten(text, 60))
		}
	}
	if finalText == "" {
		finalText = streamed.String()
	}
	fmt.Printf("anthropic_stream=ok response=%q\n", shorten(finalText, 120))
	return nil
}

func newAnthropicModel(stream bool) (asmodel.ChatModel, error) {
	apiKey := os.Getenv("AI_ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("AI_ANTHROPIC_API_KEY is required")
	}
	temperature := 0.2
	maxTokens := int64(256)

	chat, err := anthropic.NewChatModel(
		credential.NewAnthropic(apiKey).ChatCredential(),
		"claude-sonnet-4-5",
		anthropic.WithStream(stream),
		anthropic.WithChatParameters(anthropic.ChatParameters{
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
		}),
	)
	if err != nil {
		return nil, err
	}
	return chat, nil
}

func weatherTool() (*tool.FunctionTool, error) {
	functionTool, err := tool.NewFunctionTool(
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
	if err != nil {
		return nil, fmt.Errorf("create weather tool: %w", err)
	}
	return functionTool, nil
}

func firstToolCall(blocks message.ContentBlockList) *message.ToolCallBlock {
	for _, block := range blocks {
		if toolCall, ok := block.(*message.ToolCallBlock); ok {
			return toolCall
		}
	}
	return nil
}

func textContent(response *asmodel.ChatResponse) string {
	if response == nil {
		return ""
	}
	if text := response.GetTextContent(); text != nil {
		return *text
	}
	return ""
}

func shorten(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}
