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

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
)

func main() {

	ctx := context.Background()

	fmt.Println("system message: ------------------")
	system := mustMessage(message.NewSystemMessage(string(message.RoleSystem), "You are tom, is a helpful assistant."))
	user := mustMessage(message.NewUserMessage(string(message.RoleUser), "What is your name?"))
	systemCallResponse, err := newChatModel().Call(ctx, model.CallRequest{
		Messages: []*message.Message{
			system,
			user,
		},
	})
	if err != nil {
		panic(err)
	}
	if text := systemCallResponse.GetTextContent(""); text != nil {
		fmt.Println(*text)
	}

	fmt.Println("\nuser message: ------------------")
	user1 := mustMessage(message.NewUserMessage(string(message.RoleUser), "hi, please introduce yourself."))
	userCallResponse, err := newChatModel().Call(ctx, model.CallRequest{
		Messages: []*message.Message{
			user1,
		},
	})
	if err != nil {
		panic(err)
	}
	if text := userCallResponse.GetTextContent(""); text != nil {
		fmt.Println(*text)
	}

	fmt.Println("\nhistory message: -----------------")
	system5 := mustMessage(message.NewSystemMessage(string(message.RoleSystem), "You are tom, is a helpful assistant."))
	user5 := mustMessage(message.NewUserMessage(string(message.RoleUser), "What is your name?"))
	ass5 := mustMessage(message.NewAssistantMessage(string(message.RoleAssistant), "I am tom, a helpful assistant."))
	user6 := mustMessage(message.NewUserMessage(string(message.RoleUser), "Nice, tom, I am very happy, because I can talk with you."))
	ass6 := mustMessage(message.NewAssistantMessage(string(message.RoleAssistant), "Me too."))
	user7 := mustMessage(message.NewUserMessage(string(message.RoleUser), "我的心情好吗？"))
	history := []*message.Message{system5, user5, ass5, user6, ass6}
	historyCallResponse, err := newChatModel().Call(ctx, model.CallRequest{
		Messages: append(history, user7),
	})
	if err != nil {
		panic(err)
	}
	if text := historyCallResponse.GetTextContent(""); text != nil {
		fmt.Println(*text)
	}
}

func newChatModel() model.ChatModel {

	chatModel, err := dashscope.NewChatModel(dashscope.NewCredential(os.Getenv("AI_DASHSCOPE_API_KEY")), "qwen3.7-max")
	if err != nil {
		panic(err)
	}

	return chatModel
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}
