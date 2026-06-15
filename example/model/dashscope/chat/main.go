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

	// call chat
	fmt.Println("start chat call: ------------------")
	chat()

	// call stream chat
	fmt.Println("\nstart stream chat call: ------------------")
	streamChat()
}

// chat. chat method use case.
func chat() {

	// create chatModel instance
	chat := mustModel(dashscope.NewChatModel(
		// tips: need update.
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(false),
		dashscope.WithChatParameters(dashscope.ChatParameters{
			MaxTokens:   func() *int64 { v := int64(1000); return &v }(),
			Temperature: func() *float64 { v := 0.0; return &v }(),
		}),
	))

	// tools
	kit := mustToolkit(tool.NewToolkit(weatherTool()))
	schemas, err := kit.ToolSchemas()
	if err != nil {
		panic(err)
	}

	// user prompt
	visionMessage := mustMessage(message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("Describe this image in one sentence."),
		message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
	}))
	tokens, err := chat.CountTokens(asmodel.CallRequest{
		Messages: []*message.Message{visionMessage},
		Tools:    schemas,
	})
	if err != nil {
		panic(err)
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
	liveMessage := mustMessage(message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go."))
	response, err := chat.Call(ctx, asmodel.CallRequest{
		Messages: []*message.Message{liveMessage},
	})
	if err != nil {
		panic(err)
	}
	responseText := ""
	if text := response.GetTextContent(); text != nil {
		responseText = *text
	}
	fmt.Printf("dashscope_live=ok response=%q\n", shorten(responseText, 120))

	weatherMessage := mustMessage(message.NewUserMessage("user", "Use the GetWeather tool to answer: 杭州的天气怎么样？"))
	toolCallResponse, err := chat.Call(ctx, asmodel.CallRequest{
		Messages: []*message.Message{weatherMessage},
		Tools:    schemas,
	})
	if err != nil {
		panic(err)
	}
	weatherCall := firstToolCall(toolCallResponse.Content)
	if weatherCall == nil {
		text := ""
		if responseText := toolCallResponse.GetTextContent(); responseText != nil {
			text = *responseText
		}
		panic(fmt.Sprintf("DashScope weather request returned no tool call: %q", text))
	}
	toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
	if err != nil {
		panic(err)
	}

	assistantMessage := mustMessage(message.NewAssistantMessage("assistant", toolCallResponse.Content))
	toolMessage := mustMessage(message.NewAssistantMessage("tool", message.ContentBlockList{
		message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
	}))
	weatherResponse, err := chat.Call(ctx, asmodel.CallRequest{
		Messages: []*message.Message{weatherMessage, assistantMessage, toolMessage},
	})
	if err != nil {
		panic(err)
	}

	weatherText := ""
	if text := weatherResponse.GetTextContent(); text != nil {
		weatherText = *text
	}
	fmt.Printf("dashscope_weather=ok tool=%s input=%s response=%q\n", weatherCall.Name, weatherCall.Input, shorten(weatherText, 120))
}

func streamChat() {

	streamChat := mustModel(dashscope.NewChatModel(
		// tips: need update.
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(true),
		dashscope.WithChatParameters(dashscope.ChatParameters{
			MaxTokens:   func() *int64 { v := int64(1000); return &v }(),
			Temperature: func() *float64 { v := 0.0; return &v }(),
		}),
	))

	// tools
	kit := mustToolkit(tool.NewToolkit(weatherTool()))
	schemas, err := kit.ToolSchemas()
	if err != nil {
		panic(err)
	}

	// user prompt
	visionMessage := mustMessage(message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("Describe this image in one sentence."),
		message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
	}))
	tokens, err := streamChat.CountTokens(asmodel.CallRequest{
		Messages: []*message.Message{visionMessage},
		Tools:    schemas,
	})
	if err != nil {
		panic(err)
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
	liveMessage := mustMessage(message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go."))
	responses, err := streamChat.Stream(ctx, asmodel.CallRequest{
		Messages: []*message.Message{liveMessage},
		Stream:   true,
	})
	if err != nil {
		panic(err)
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
}

func weatherTool() *tool.FunctionTool {

	return mustTool(tool.NewFunctionTool(
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
	))
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

func mustModel(model asmodel.ChatModel, err error) asmodel.ChatModel {
	if err != nil {
		panic(err)
	}
	return model
}

func mustTool(tool *tool.FunctionTool, err error) *tool.FunctionTool {
	if err != nil {
		panic(err)
	}
	return tool
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
