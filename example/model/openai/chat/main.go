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
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/openai"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

const openAIRequestTimeout = 90 * time.Second

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

	chat := newOpenAIModel(false)

	// tools
	kit, err := tool.NewToolkit(weatherTool())
	if err != nil {
		panic(err)
	}
	schemas, err := kit.ToolSchemas()
	if err != nil {
		panic(err)
	}

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
	fmt.Printf("openai_live=ok response=%q\n", responseText)

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
		panic(fmt.Sprintf("glm weather request returned no tool call: %q", text))
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
	fmt.Printf("openai_weather=ok tool=%s input=%s response=%q\n", weatherCall.Name, weatherCall.Input, weatherText)
}

func streamChat() {

	streamChat := newOpenAIModel(true)

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
			fmt.Printf("openai_stream_delta=%q\n", text)
		}
	}
	if finalText == "" {
		finalText = streamed.String()
	}
	fmt.Printf("openai_stream=ok response=%q\n", finalText)
}

func newOpenAIModel(stream bool) asmodel.ChatModel {

	// 使用自定义 http client
	httpClient, proxyURL := newOpenAIHTTPClient()
	if proxyURL != "" {
		fmt.Printf("openai_proxy=%s\n", proxyURL)
	}

	chat, err := openai.NewChatModel(
		credential.NewOpenAI(
			os.Getenv("AI_OPENAI_API_KEY"),
			// 指定代理地址
			credential.WithBaseURL("https://xxx.xx/v1"),
		).ChatCredential(),
		"gpt-5.5",
		openai.WithHTTPClient(httpClient),
		openai.WithStream(stream),
		// 可以覆盖默认 SDK User-Agent，避免被中转服务 WAF 拦截
		openai.WithHeader("User-Agent", "Mozilla/5.0"),
		openai.WithChatParameters(openai.ChatParameters{
			MaxTokens:   func() *int64 { v := int64(256); return &v }(),
			Temperature: func() *float64 { v := 0.01; return &v }(),
		}),
	)
	if err != nil {
		panic(err)
	}

	return chat
}

func newOpenAIHTTPClient() (*http.Client, string) {

	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyURL := strings.TrimSpace(os.Getenv("AI_OPENAI_PROXY_URL"))
	if proxyURL == "" {
		return &http.Client{Transport: transport, Timeout: openAIRequestTimeout}, ""
	}

	parsedProxyURL, err := url.Parse(proxyURL)
	if err != nil {
		panic(fmt.Sprintf("invalid AI_OPENAI_PROXY_URL %q: %v", proxyURL, err))
	}
	if parsedProxyURL.Scheme == "" || parsedProxyURL.Host == "" {
		panic(fmt.Sprintf("invalid AI_OPENAI_PROXY_URL %q: expected scheme://host", proxyURL))
	}
	transport.Proxy = http.ProxyURL(parsedProxyURL)

	return &http.Client{Transport: transport, Timeout: openAIRequestTimeout}, parsedProxyURL.Redacted()
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
