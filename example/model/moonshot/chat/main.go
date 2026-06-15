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
	"github.com/yuluo-yx/agentscope-go/model/moonshot"
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

	// get apiKey and set some params.
	temperature := 0.2
	maxTokens := int64(256)

	// create chatModel instance
	chat, err := moonshot.NewChatModel(
		credential.NewMoonshot(os.Getenv("AI_MOONSHOT_API_KEY")).ChatCredential(),
		"moonshot-v1-8k",
		moonshot.WithStream(false),
		moonshot.WithChatParameters(moonshot.ChatParameters{
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
		}),
	)
	if err != nil {
		panic(err)
	}

	// tools
	kit, err := tool.NewToolkit(weatherTool())
	if err != nil {
		panic(err)
	}
	schemas, err := kit.ToolSchemas()
	if err != nil {
		panic(err)
	}

	// user prompt
	visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("Describe this image in one sentence."),
		message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
	})
	if err != nil {
		panic(err)
	}
	tokens, err := chat.CountTokens(asmodel.CallRequest{
		Messages: []*message.Message{visionMessage},
		Tools:    schemas,
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf(
		"chat_model=%s moonshot_model=%s tools=%d multimodal_blocks=%d estimated_tokens=%d\n",
		chat.Name(),
		chat.Name(),
		len(schemas),
		len(visionMessage.Content),
		tokens,
	)

	ctx := context.Background()
	liveMessage, err := message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go.")
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
	fmt.Printf("moonshot_live=ok response=%q\n", responseText)

	weatherMessage, err := message.NewUserMessage("user", "Use the GetWeather tool to answer: 杭州的天气怎么样？")
	if err != nil {
		panic(err)
	}
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
		panic(fmt.Sprintf("Moonshot weather request returned no tool call: %q", text))
	}
	toolResponse, err := kit.RunTool(ctx, weatherCall, asstate.NewAgentState())
	if err != nil {
		panic(err)
	}

	assistantMessage, err := message.NewAssistantMessage("assistant", toolCallResponse.Content)
	if err != nil {
		panic(err)
	}
	toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
		message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
	})
	if err != nil {
		panic(err)
	}
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
	fmt.Printf("moonshot_weather=ok tool=%s input=%s response=%q\n", weatherCall.Name, weatherCall.Input, weatherText)
}

func streamChat() {
	temperature := 0.2
	maxTokens := int64(256)

	streamChat, err := moonshot.NewChatModel(
		credential.NewMoonshot(os.Getenv("AI_MOONSHOT_API_KEY")).ChatCredential(),
		"moonshot-v1-8k",
		moonshot.WithStream(true),
		moonshot.WithChatParameters(moonshot.ChatParameters{
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
		}),
	)
	if err != nil {
		panic(err)
	}

	// tools
	kit, err := tool.NewToolkit(weatherTool())
	if err != nil {
		panic(err)
	}
	schemas, err := kit.ToolSchemas()
	if err != nil {
		panic(err)
	}

	// user prompt
	visionMessage, err := message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("Describe this image in one sentence."),
		message.NewDataBlock(message.NewURLSource("https://example.com/sample.png", "image/png"), message.WithDataBlockName("sample.png")),
	})
	if err != nil {
		panic(err)
	}
	tokens, err := streamChat.CountTokens(asmodel.CallRequest{
		Messages: []*message.Message{visionMessage},
		Tools:    schemas,
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf(
		"chat_model=%s moonshot_model=%s tools=%d multimodal_blocks=%d estimated_tokens=%d\n",
		streamChat.Name(),
		streamChat.Name(),
		len(schemas),
		len(visionMessage.Content),
		tokens,
	)

	ctx := context.Background()
	liveMessage, err := message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go.")
	if err != nil {
		panic(err)
	}
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
			fmt.Printf("moonshot_stream_delta=%q\n", text)
		}
	}
	if finalText == "" {
		finalText = streamed.String()
	}
	fmt.Printf("moonshot_stream=ok response=%q\n", finalText)
}

func weatherTool() *tool.FunctionTool {

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
		panic(err)
	}

	return functionTool
}

func firstToolCall(blocks message.ContentBlockList) *message.ToolCallBlock {
	for _, block := range blocks {
		if toolCall, ok := block.(*message.ToolCallBlock); ok {
			return toolCall
		}
	}
	return nil
}
