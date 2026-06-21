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

	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "message example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	chatModel, err := newChatModel()
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}

	fmt.Println("system message: ------------------")
	system, err := message.NewSystemMessage(string(message.RoleSystem), "You are tom, is a helpful assistant.")
	if err != nil {
		return fmt.Errorf("create system message: %w", err)
	}
	user, err := message.NewUserMessage(string(message.RoleUser), "What is your name?")
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}
	systemCallResponse, err := chatModel.Call(ctx, model.CallRequest{
		Messages: []*message.Message{
			system,
			user,
		},
	})
	if err != nil {
		return fmt.Errorf("call system message example: %w", err)
	}
	if text := systemCallResponse.GetTextContent(); text != nil {
		fmt.Println(*text)
	}

	fmt.Println("\nuser message: ------------------")
	user1, err := message.NewUserMessage(string(message.RoleUser), "hi, please introduce yourself.")
	if err != nil {
		return fmt.Errorf("create single user message: %w", err)
	}
	userCallResponse, err := chatModel.Call(ctx, model.CallRequest{
		Messages: []*message.Message{
			user1,
		},
	})
	if err != nil {
		return fmt.Errorf("call user message example: %w", err)
	}
	if text := userCallResponse.GetTextContent(); text != nil {
		fmt.Println(*text)
	}

	fmt.Println("\nhistory message: -----------------")
	system5, err := message.NewSystemMessage(string(message.RoleSystem), "You are tom, is a helpful assistant.")
	if err != nil {
		return fmt.Errorf("create history system message: %w", err)
	}
	user5, err := message.NewUserMessage(string(message.RoleUser), "What is your name?")
	if err != nil {
		return fmt.Errorf("create first history user message: %w", err)
	}
	ass5, err := message.NewAssistantMessage(string(message.RoleAssistant), "I am tom, a helpful assistant.")
	if err != nil {
		return fmt.Errorf("create first history assistant message: %w", err)
	}
	user6, err := message.NewUserMessage(string(message.RoleUser), "Nice, tom, I am very happy, because I can talk with you.")
	if err != nil {
		return fmt.Errorf("create second history user message: %w", err)
	}
	ass6, err := message.NewAssistantMessage(string(message.RoleAssistant), "Me too.")
	if err != nil {
		return fmt.Errorf("create second history assistant message: %w", err)
	}
	user7, err := message.NewUserMessage(string(message.RoleUser), "我的心情好吗？")
	if err != nil {
		return fmt.Errorf("create final history user message: %w", err)
	}
	history := []*message.Message{system5, user5, ass5, user6, ass6}
	historyCallResponse, err := chatModel.Call(ctx, model.CallRequest{
		Messages: append(history, user7),
	})
	if err != nil {
		return fmt.Errorf("call history message example: %w", err)
	}
	if text := historyCallResponse.GetTextContent(); text != nil {
		fmt.Println(*text)
	}
	return nil
}

func newChatModel() (model.ChatModel, error) {
	apiKey := os.Getenv("AI_DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}

	return dashscope.NewChatModel(credential.NewDashScope(apiKey).ChatCredential(), "qwen3.7-max")
}
