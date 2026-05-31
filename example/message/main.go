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
)

func main() {
	system := mustMessage(message.NewSystemMessage("system", "You are an AgentScope Go assistant. Keep replies concise."))
	user := mustMessage(message.NewUserMessage("user", "Create a short plan for adding a workspace-backed tool."))
	assistant := mustMessage(message.NewAssistantMessage("assistant", "Use a local workspace, expose the needed tools, run the tool, then append the result to the conversation."))
	history := []*message.Message{system, user, assistant}

	fmt.Printf(
		"conversation_messages=%d roles=%s system_finished=%t user=%q assistant=%q\n",
		len(history),
		roles(history),
		system.FinishedAt != nil,
		textContent(user),
		textContent(assistant),
	)
}

func roles(messages []*message.Message) string {
	values := make([]string, 0, len(messages))
	for _, msg := range messages {
		values = append(values, string(msg.Role))
	}
	return strings.Join(values, ",")
}

func textContent(msg *message.Message) string {
	if msg == nil {
		return ""
	}
	text := msg.GetTextContent(" ")
	if text == nil {
		return ""
	}
	return *text
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}
