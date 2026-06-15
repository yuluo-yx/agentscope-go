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
	fmt.Println("start anthropic chat call: ------------------")
	chat()

	fmt.Println("\nstart anthropic stream chat call: ------------------")
	streamChat()
}

func chat() {
	chat := newAnthropicModel(false)

	kit, err := tool.NewToolkit(weatherTool())
	if err != nil {
		panic(err)
	}
	schemas, err := kit.ToolSchemas()
	if err != nil {
		panic(err)
	}

	liveMessage := mustMessage(message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go."))
	liveRequest := asmodel.CallRequest{Messages: []*message.Message{liveMessage}}
	tokens, err := chat.CountTokens(liveRequest)
	if err != nil {
		panic(err)
	}

	fmt.Printf("chat_model=%s anthropic_model=%s estimated_tokens=%d\n", chat.Name(), "claude-sonnet-4-5", tokens)

	ctx := context.Background()
	response, err := chat.Call(ctx, liveRequest)
	if err != nil {
		panic(err)
	}
	fmt.Printf("anthropic_live=ok response=%q\n", shorten(textContent(response), 120))

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
		panic(fmt.Sprintf("anthropic weather request returned no tool call: %q", shorten(textContent(toolCallResponse), 120)))
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

	fmt.Printf("anthropic_weather=ok tool=%s input=%s response=%q\n", weatherCall.Name, weatherCall.Input, shorten(textContent(weatherResponse), 120))
}

func streamChat() {
	streamChat := newAnthropicModel(true)

	user := mustMessage(message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go."))
	request := asmodel.CallRequest{Messages: []*message.Message{user}, Stream: true}
	tokens, err := streamChat.CountTokens(request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("chat_model=%s anthropic_model=%s estimated_tokens=%d\n", streamChat.Name(), "claude-sonnet-4-5", tokens)

	responses, err := streamChat.Stream(context.Background(), request)
	if err != nil {
		panic(err)
	}
	var streamed strings.Builder
	var finalText string
	for response := range responses {
		if response.Error != nil {
			panic(response.Error)
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
}

func newAnthropicModel(stream bool) asmodel.ChatModel {
	temperature := 0.2
	maxTokens := int64(256)

	chat, err := anthropic.NewChatModel(
		credential.NewAnthropic(os.Getenv("AI_ANTHROPIC_API_KEY")).ChatCredential(),
		"claude-sonnet-4-5",
		anthropic.WithStream(stream),
		anthropic.WithChatParameters(anthropic.ChatParameters{
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
		}),
	)
	if err != nil {
		panic(err)
	}
	return chat
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

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
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
