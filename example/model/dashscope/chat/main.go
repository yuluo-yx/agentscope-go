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
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "dashscope chat example: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// call chat
	fmt.Println("start chat call: ------------------")
	if err := chat(); err != nil {
		return err
	}

	// call stream chat
	fmt.Println("\nstart stream chat call: ------------------")
	if err := streamChat(); err != nil {
		return err
	}
	return nil
}

// chat. chat method use case.
func chat() error {

	// create chatModel instance
	chat, err := newDashScopeChatModel(false)
	if err != nil {
		return fmt.Errorf("create non-stream chat model: %w", err)
	}

	// tools
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

	// user prompt
	visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("Describe this image in one sentence."),
		message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
	})
	if err != nil {
		return fmt.Errorf("create vision message: %w", err)
	}
	tokens, err := chat.CountTokens(asmodel.CallRequest{
		Messages: []*message.Message{visionMessage},
		Tools:    schemas,
	})
	if err != nil {
		return fmt.Errorf("count tokens: %w", err)
	}

	fmt.Printf(
		"chat_model=%s dashscope_model=%s tools=%d multimodal_blocks=%d estimated_tokens=%d\n",
		chat.Name(),
		chat.Name(),
		len(schemas),
		len(visionMessage.Content),
		tokens,
	)

	ctx := context.Background()
	liveMessage, err := message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go.")
	if err != nil {
		return fmt.Errorf("create live message: %w", err)
	}
	response, err := chat.Call(ctx, asmodel.CallRequest{
		Messages: []*message.Message{liveMessage},
	})
	if err != nil {
		return fmt.Errorf("call live chat: %w", err)
	}
	responseText := ""
	if text := response.GetTextContent(); text != nil {
		responseText = *text
	}
	fmt.Printf("dashscope_live=ok response=%q\n", shorten(responseText, 120))

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
		text := ""
		if responseText := toolCallResponse.GetTextContent(); responseText != nil {
			text = *responseText
		}
		return fmt.Errorf("DashScope weather request returned no tool call: %q", text)
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

	weatherText := ""
	if text := weatherResponse.GetTextContent(); text != nil {
		weatherText = *text
	}
	fmt.Printf("dashscope_weather=ok tool=%s input=%s response=%q\n", weatherCall.Name, weatherCall.Input, shorten(weatherText, 120))
	return nil
}

func streamChat() error {

	streamChat, err := newDashScopeChatModel(true)
	if err != nil {
		return fmt.Errorf("create stream chat model: %w", err)
	}

	// tools
	weather, err := weatherTool()
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(weather)
	if err != nil {
		return fmt.Errorf("create stream toolkit: %w", err)
	}
	schemas, err := kit.ToolSchemas()
	if err != nil {
		return fmt.Errorf("build stream tool schemas: %w", err)
	}

	// user prompt
	visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("Describe this image in one sentence."),
		message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
	})
	if err != nil {
		return fmt.Errorf("create stream vision message: %w", err)
	}
	tokens, err := streamChat.CountTokens(asmodel.CallRequest{
		Messages: []*message.Message{visionMessage},
		Tools:    schemas,
	})
	if err != nil {
		return fmt.Errorf("count stream tokens: %w", err)
	}

	fmt.Printf(
		"chat_model=%s dashscope_model=%s tools=%d multimodal_blocks=%d estimated_tokens=%d\n",
		streamChat.Name(),
		streamChat.Name(),
		len(schemas),
		len(visionMessage.Content),
		tokens,
	)

	ctx := context.Background()
	liveMessage, err := message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go.")
	if err != nil {
		return fmt.Errorf("create stream live message: %w", err)
	}
	responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
		Messages: []*message.Message{liveMessage},
		Stream:   true,
	})
	if err != nil {
		return fmt.Errorf("stream live chat: %w", err)
	}
	var streamed strings.Builder
	var finalText string
	for response := range responses {
		text := ""
		if responseText := response.GetTextContent(); responseText != nil {
			text = *responseText
		}
		if response.IsLast {
			finalText = text
			continue
		}
		if text != "" {
			streamed.WriteString(text)
			fmt.Printf("dashscope_stream_delta=%q\n", shorten(text, 60))
		}
	}
	if finalText == "" {
		finalText = streamed.String()
	}
	fmt.Printf("dashscope_stream=ok response=%q\n", shorten(finalText, 120))
	return nil
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

func firstToolCall(blocks message.ContentBlockList) *message.ToolCallBlock {
	for _, block := range blocks {
		if toolCall, ok := block.(*message.ToolCallBlock); ok {
			return toolCall
		}
	}
	return nil
}

func shorten(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
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
		dashscope.WithChatParameters(dashscope.ChatParameters{
			MaxTokens:   func() *int64 { v := int64(1000); return &v }(),
			Temperature: func() *float64 { v := 0.0; return &v }(),
		}),
	)
}
