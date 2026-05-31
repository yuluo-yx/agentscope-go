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

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	asstate "github.com/yuluo-yx/agentscope-go/state"
)

type failingChatModel struct {
	streamCalls int
}

func (m *failingChatModel) Name() string {
	return "primary-failing"
}

func (m *failingChatModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return nil, fmt.Errorf("primary call failed")
}

func (m *failingChatModel) Stream(context.Context, asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	m.streamCalls++
	return nil, fmt.Errorf("primary stream failed")
}

func (m *failingChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

type scriptedChatModel struct {
	streamCalls int
	responses   []*asmodel.ChatResponse
}

func (m *scriptedChatModel) Name() string {
	return "fallback-scripted"
}

func (m *scriptedChatModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return m.nextResponse()
}

func (m *scriptedChatModel) Stream(ctx context.Context, _ asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	m.streamCalls++
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
	state := asstate.NewAgentState()
	longResult := mustMessage(message.NewAssistantMessage("Friday", message.ContentBlockList{
		message.NewToolResultBlock(
			"read-call",
			"Read",
			message.ToolResultOutput{
				Raw: "this raw tool result is intentionally long",
				Blocks: message.ContentBlockList{
					message.NewTextBlock("this text tool result is intentionally long"),
				},
			},
			message.ToolResultSuccess,
		),
	}))
	state.Context = []*message.Message{longResult}

	primary := &failingChatModel{}
	fallback := &scriptedChatModel{responses: []*asmodel.ChatResponse{
		asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("fallback model replied")}, true),
	}}
	modelConfig := agentpkg.DefaultModelConfig()
	modelConfig.MaxRetries = 1
	modelConfig.FallbackModel = fallback

	contextConfig := agentpkg.DefaultContextConfig()
	contextConfig.ToolResultLimit = 10
	reactConfig := agentpkg.DefaultReActConfig()
	reactConfig.MaxIters = 2

	agent := mustAgent(agentpkg.NewAgent(
		"Friday",
		"Use the fallback model if the primary model fails.",
		primary,
		agentpkg.WithAgentState(state),
		agentpkg.WithModelConfig(modelConfig),
		agentpkg.WithContextConfig(contextConfig),
		agentpkg.WithReActConfig(reactConfig),
	))
	reply := mustReply(agent.Reply(context.Background(), mustMessage(message.NewUserMessage("user", "Use configured fallback."))))
	fmt.Printf("reply=%s primary_stream_calls=%d fallback_stream_calls=%d compressed=%t\n",
		textContent(reply),
		primary.streamCalls,
		fallback.streamCalls,
		contextCompressed(state),
	)
}

func contextCompressed(state *asstate.AgentState) bool {
	if state == nil || len(state.Context) == 0 {
		return false
	}
	results := state.Context[0].GetContentBlocks("tool_result")
	if len(results) == 0 {
		return false
	}
	result, ok := results[0].(*message.ToolResultBlock)
	return ok && strings.Contains(result.Output.Raw, "truncated") &&
		len(result.Output.Blocks) > 0 &&
		strings.Contains(result.Output.Blocks[0].(*message.TextBlock).Text, "truncated")
}

func textContent(msg *message.Message) string {
	if msg == nil {
		return ""
	}
	text := msg.GetTextContent("")
	if text == nil {
		return ""
	}
	return *text
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
