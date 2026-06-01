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

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
)

func main() {

	ctx := context.Background()

	fmt.Println("system message: ------------------")
	system, err := message.NewSystemMessage(string(message.RoleSystem), "You are tom, is a helpful assistant.")
	if err != nil {
		panic(err)
	}
	user, err := message.NewUserMessage(string(message.RoleUser), "What is your name?")
	systemCallResponse, err := newChatModel().Call(ctx, model.CallRequest{
		Messages: []*message.Message{
			system,
			user,
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(textContent(systemCallResponse.Content))

	fmt.Println("\nuser message: ------------------")
	user1, err := message.NewUserMessage(string(message.RoleUser), "hi, please introduce yourself.")
	if err != nil {
		panic(err)
	}
	userCallResponse, err := newChatModel().Call(ctx, model.CallRequest{
		Messages: []*message.Message{
			user1,
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(textContent(userCallResponse.Content))

	fmt.Println("\nhistory message: -----------------")
	system5, err := message.NewSystemMessage(string(message.RoleSystem), "You are tom, is a helpful assistant.")
	user5, err := message.NewUserMessage(string(message.RoleUser), "What is your name?")
	ass5, err := message.NewAssistantMessage(string(message.RoleAssistant), "I am tom, a helpful assistant.")
	user6, err := message.NewUserMessage(string(message.RoleUser), "Nice, tom, I am very happy, because I can talk with you.")
	ass6, err := message.NewAssistantMessage(string(message.RoleAssistant), "Me too.")
	user7, err := message.NewUserMessage(string(message.RoleUser), "我的心情好吗？")
	history := []*message.Message{system5, user5, ass5, user6, ass6}
	historyCallResponse, err := newChatModel().Call(ctx, model.CallRequest{
		Messages: append(history, user7),
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(textContent(historyCallResponse.Content)))
}

func newChatModel() model.ChatModel {

	chatModel, err := dashscope.NewChatModel(dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")), "qwen3.7-max")
	if err != nil {
		panic(err)
	}

	return chatModel
}

func textContent(blocks message.ContentBlockList) string {
	var builder strings.Builder
	for _, block := range blocks {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}
