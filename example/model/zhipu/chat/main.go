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
	"strings"

	"github.com/yuluo-yx/agentscope-go/example/common/modelconfig"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/zhipu"
)

func main() {
	fmt.Println("start zhipu chat call: ------------------")
	chat()

	fmt.Println("\nstart zhipu stream chat call: ------------------")
	streamChat()
}

func chat() {
	cfg := modelconfig.Zhipu("glm-5.1")
	temperature := 0.2
	maxTokens := int64(256)

	chat := mustModel(zhipu.NewChatModel(
		zhipuCredential(cfg),
		cfg.Model,
		zhipu.WithStream(false),
		zhipu.WithChatParameters(zhipu.ChatParameters{
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
		}),
	))

	user := mustMessage(message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go."))
	request := asmodel.CallRequest{Messages: []*message.Message{user}}
	tokens, err := chat.CountTokens(request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("chat_model=%s zhipu_model=%s estimated_tokens=%d\n", chat.Name(), cfg.Model, tokens)
	if !cfg.Live {
		fmt.Println("zhipu_live=skipped")
		return
	}

	response, err := chat.Call(context.Background(), request)
	if err != nil {
		panic(err)
	}
	responseText := ""
	if text := response.GetTextContent(); text != nil {
		responseText = *text
	}
	fmt.Printf("zhipu_live=ok response=%q\n", shorten(responseText, 120))
}

func streamChat() {
	cfg := modelconfig.Zhipu("glm-5.1")
	temperature := 0.2
	maxTokens := int64(256)

	streamChat := mustModel(zhipu.NewChatModel(
		zhipuCredential(cfg),
		cfg.Model,
		zhipu.WithStream(true),
		zhipu.WithChatParameters(zhipu.ChatParameters{
			MaxTokens:   &maxTokens,
			Temperature: &temperature,
		}),
	))

	user := mustMessage(message.NewUserMessage("user", "Reply with one short sentence about AgentScope Go."))
	request := asmodel.CallRequest{Messages: []*message.Message{user}, Stream: true}
	tokens, err := streamChat.CountTokens(request)
	if err != nil {
		panic(err)
	}

	fmt.Printf("chat_model=%s zhipu_model=%s estimated_tokens=%d\n", streamChat.Name(), cfg.Model, tokens)
	if !cfg.Live {
		fmt.Println("zhipu_live=skipped")
		return
	}

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
			fmt.Printf("zhipu_stream_delta=%q\n", shorten(text, 60))
		}
	}
	if finalText == "" {
		finalText = streamed.String()
	}
	fmt.Printf("zhipu_stream=ok response=%q\n", shorten(finalText, 120))
}

func zhipuCredential(cfg modelconfig.ZhipuConfig) zhipu.Credential {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return zhipu.NewCredential(cfg.APIKey)
	}
	return zhipu.NewCredential(cfg.APIKey, zhipu.WithBaseURL(cfg.BaseURL))
}

func mustModel(model asmodel.ChatModel, err error) asmodel.ChatModel {
	if err != nil {
		panic(err)
	}
	return model
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
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
