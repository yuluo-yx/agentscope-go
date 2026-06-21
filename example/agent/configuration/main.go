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

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
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

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agent configuration example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	state := asstate.NewAgentState()
	longResult, err := message.NewAssistantMessage("Friday", message.ContentBlockList{
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
	})
	if err != nil {
		return fmt.Errorf("create long result message: %w", err)
	}
	state.Context = []*message.Message{longResult}

	primary := &failingChatModel{}
	fallback, err := newDashScopeChatModel(true)
	if err != nil {
		return fmt.Errorf("create DashScope fallback model: %w", err)
	}
	modelConfig := agentpkg.DefaultModelConfig()
	modelConfig.MaxRetries = 1
	modelConfig.FallbackModel = fallback

	contextConfig := agentpkg.DefaultContextConfig()
	contextConfig.ToolResultLimit = 10
	reactConfig := agentpkg.DefaultReActConfig()
	reactConfig.MaxIters = 2

	agent, err := agentpkg.NewAgent(
		"Friday",
		"Use the fallback model if the primary model fails.",
		primary,
		agentpkg.WithAgentState(state),
		agentpkg.WithModelConfig(modelConfig),
		agentpkg.WithContextConfig(contextConfig),
		agentpkg.WithReActConfig(reactConfig),
	)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	user, err := message.NewUserMessage("user", "Use the configured fallback model and reply with one short sentence.")
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}
	reply, err := agent.Reply(ctx, user)
	if err != nil {
		return fmt.Errorf("agent reply: %w", err)
	}
	replyText := ""
	if text := reply.GetTextContent(); text != nil {
		replyText = *text
	}
	fmt.Printf("reply=%s primary_stream_calls=%d fallback_model=%s compressed=%t\n",
		replyText,
		primary.streamCalls,
		fallback.Name(),
		contextCompressed(state),
	)
	return nil
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
	if !ok {
		return false
	}
	text := result.Output.Blocks.GetTextContent()
	return strings.Contains(result.Output.Raw, "truncated") &&
		len(result.Output.Blocks) > 0 &&
		text != nil &&
		strings.Contains(*text, "truncated")
}

func newDashScopeChatModel(stream bool) (*dashscope.ChatModel, error) {
	apiKey := os.Getenv("AI_DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}
	return dashscope.NewChatModel(
		credential.NewDashScope(apiKey).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(stream),
	)
}
