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

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/tool"
	tasktool "github.com/yuluo-yx/agentscope-go/tool/task"
)

type scriptedChatModel struct {
	responses []*asmodel.ChatResponse
}

func (m *scriptedChatModel) Name() string {
	return "scripted-example"
}

func (m *scriptedChatModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func (m *scriptedChatModel) Stream(context.Context, asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	return nil, fmt.Errorf("stream is not used in this example")
}

func (m *scriptedChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func main() {
	kit := mustToolkit(tool.NewToolkit(tasktool.NewTaskCreate()))
	model := &scriptedChatModel{responses: []*asmodel.ChatResponse{
		asmodel.NewChatResponse(
			message.ContentBlockList{
				message.NewToolCallBlock("task-call", "TaskCreate", `{"subject":"Write examples","description":"Create standalone examples for AgentScope Go."}`),
			},
			true,
		),
		asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("task tracked")}, true),
	}}
	agent := mustAgent(agentpkg.NewAgent("Friday", "Track work with task tools.", model, agentpkg.WithToolkit(kit)))
	user := mustMessage(message.NewUserMessage("user", "Track the example task."))
	reply, err := agent.Reply(context.Background(), user)
	if err != nil {
		panic(err)
	}

	replyText := ""
	if text := reply.GetTextContent(""); text != nil {
		replyText = *text
	}
	fmt.Printf("agent_reply=%s tasks=%d\n", replyText, len(agent.AgentState().TaskContext.Tasks))
}

func mustToolkit(kit *tool.Toolkit, err error) *tool.Toolkit {
	if err != nil {
		panic(err)
	}
	return kit
}

func mustAgent(agent *agentpkg.Agent, err error) *agentpkg.Agent {
	if err != nil {
		panic(err)
	}
	return agent
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}
