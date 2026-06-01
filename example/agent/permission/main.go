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
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

type scriptedChatModel struct {
	responses []*asmodel.ChatResponse
}

func (m *scriptedChatModel) Name() string {
	return "scripted-permission"
}

func (m *scriptedChatModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return m.nextResponse()
}

func (m *scriptedChatModel) Stream(ctx context.Context, _ asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	response, err := m.nextResponse()
	if err != nil {
		return nil, err
	}
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		delta := response.Clone()
		delta.IsLast = false
		delta.Usage = nil
		select {
		case <-ctx.Done():
			return
		case out <- *delta:
		}
		select {
		case <-ctx.Done():
		case out <- *response:
		}
	}()
	return out, nil
}

func (m *scriptedChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *scriptedChatModel) nextResponse() (*asmodel.ChatResponse, error) {
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func main() {
	executed := false
	write := mustTool(tool.NewFunctionTool(
		"WriteThing",
		"Write one value after user confirmation.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			executed = true
			return message.ContentBlockList{message.NewTextBlock("written")}, nil
		},
		tool.WithFunctionSuggestedRule("WriteThing"),
	))
	kit := mustToolkit(tool.NewToolkit(write))
	model := &scriptedChatModel{responses: []*asmodel.ChatResponse{
		asmodel.NewChatResponse(message.ContentBlockList{
			message.NewToolCallBlock("write-call", "WriteThing", `{}`),
		}, true),
		asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("write approved")}, true),
	}}
	agent := mustAgent(agentpkg.NewAgent("Friday", "Ask before writes.", model, agentpkg.WithToolkit(kit)))
	user := mustMessage(message.NewUserMessage("user", "Write the thing."))

	var confirm *message.RequireUserConfirmEvent
	if err := agent.ReplyStream(context.Background(), user, func(event message.Event) error {
		if typed, ok := event.(*message.RequireUserConfirmEvent); ok {
			confirm = typed
		}
		return nil
	}); err != nil {
		panic(err)
	}
	if confirm == nil || len(confirm.ToolCalls) == 0 {
		panic("expected a user confirmation event")
	}
	fmt.Printf("confirmation=required tool=%s suggestions=%d\n", confirm.ToolCalls[0].Name, len(confirm.ToolCalls[0].SuggestedRules))

	confirmEvent := message.NewUserConfirmResultEvent(confirm.ReplyID(), []message.ConfirmResult{{
		Confirmed: true,
		ToolCall:  confirm.ToolCalls[0],
		Rules:     confirm.ToolCalls[0].SuggestedRules,
	}})
	reply := mustReply(agent.Reply(context.Background(), confirmEvent))
	replyText := ""
	if text := reply.GetTextContent(""); text != nil {
		replyText = *text
	}
	fmt.Printf("confirmed_reply=%s executed=%t\n", replyText, executed)
}

func mustTool(t *tool.FunctionTool, err error) *tool.FunctionTool {
	if err != nil {
		panic(err)
	}
	return t
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

func mustReply(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}
