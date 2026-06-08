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
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/anthropic"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/model/deepseek"
	"github.com/yuluo-yx/agentscope-go/model/gemini"
	"github.com/yuluo-yx/agentscope-go/model/moonshot"
	"github.com/yuluo-yx/agentscope-go/model/ollama"
	"github.com/yuluo-yx/agentscope-go/model/openai"
	"github.com/yuluo-yx/agentscope-go/model/xai"
	"github.com/yuluo-yx/agentscope-go/model/zhipu"
)

func main() {
	providers := []asmodel.ChatModel{
		mustModel(openai.NewChatModel(openai.NewCredential("demo-openai-key"), "gpt-4.1-mini", openai.WithStream(false))),
		mustModel(anthropic.NewChatModel(anthropic.NewCredential("demo-anthropic-key"), "claude-sonnet-4-5", anthropic.WithStream(false))),
		mustModel(deepseek.NewChatModel(deepseek.NewCredential("demo-deepseek-key"), "deepseek-chat", deepseek.WithStream(false))),
		mustModel(dashscope.NewChatModel(dashscope.NewCredential("demo-dashscope-key"), "qwen3.6-plus", dashscope.WithStream(false))),
		mustModel(gemini.NewChatModel(gemini.NewCredential("demo-gemini-key"), "gemini-2.5-flash")),
		mustModel(moonshot.NewChatModel(moonshot.NewCredential("demo-moonshot-key"), "kimi-k2.6", moonshot.WithStream(false))),
		mustModel(xai.NewChatModel(xai.NewCredential("demo-xai-key"), "grok-3", xai.WithStream(false))),
		mustModel(zhipu.NewChatModel(zhipu.NewCredential("demo-zhipu-key"), "glm-4.5-flash", zhipu.WithStream(false))),
		mustModel(ollama.NewChatModel(ollama.NewCredential(ollama.WithHost("http://localhost:11434")), "llama3.2", ollama.WithStream(false))),
	}

	user := mustMessage(message.NewUserMessage("user", "Estimate provider request tokens."))
	request := asmodel.CallRequest{Messages: []*message.Message{user}}

	names := make([]string, 0, len(providers))
	tokenEstimates := make([]string, 0, len(providers))
	for _, provider := range providers {
		names = append(names, provider.Name())
		tokens, err := provider.CountTokens(request)
		if err != nil {
			panic(fmt.Sprintf("%s CountTokens: %v", provider.Name(), err))
		}
		tokenEstimates = append(tokenEstimates, fmt.Sprintf("%s=%d", provider.Name(), tokens))
	}
	fmt.Printf("providers=%d names=%s token_estimates=%s\n", len(providers), strings.Join(names, ","), strings.Join(tokenEstimates, ","))
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
