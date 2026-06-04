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
	"path/filepath"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	wslocal "github.com/yuluo-yx/agentscope-go/workspace/local"
)

type scriptedSummaryModel struct {
	callRequests []asmodel.CallRequest
}

func (m *scriptedSummaryModel) Name() string {
	return "scripted-summary"
}

func (m *scriptedSummaryModel) Call(_ context.Context, request asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	m.callRequests = append(m.callRequests, request.Clone())
	return asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock(`{
		"task_overview": "demonstrate context compression",
		"current_state": "old messages were summarized",
		"important_discoveries": "workspace offload keeps the full context",
		"next_steps": "continue with the latest instruction",
		"context_to_preserve": "custom strategies can replace the default chain"
	}`)}, true), nil
}

func (m *scriptedSummaryModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	response, err := m.Call(ctx, request)
	if err != nil {
		return nil, err
	}
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
		case out <- *response:
		}
	}()
	return out, nil
}

func (m *scriptedSummaryModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

type staticContextStrategy struct{}

func (staticContextStrategy) ContextStrategyName() string {
	return "static-summary"
}

func (staticContextStrategy) ApplyContextStrategy(_ context.Context, input *agentpkg.ContextStrategyInput) error {
	input.State.Summary.Text = "summary from a custom context strategy"
	return nil
}

func main() {
	ctx := context.Background()
	workdir := mustTempDir("agentscope-context-strategy-*")
	workspace := mustWorkspace(wslocal.NewWorkspace(workdir))
	must(workspace.Initialize(ctx))
	defer func() {
		must(workspace.Close(context.Background()))
	}()

	state := agentpkg.NewAgentState()
	state.Context = []*message.Message{
		mustMessage(message.NewUserMessage("user", strings.Repeat("old user context ", 20))),
		mustMessage(message.NewAssistantMessage("Friday", strings.Repeat("old assistant context ", 20))),
		mustMessage(message.NewUserMessage("user", "keep this recent instruction")),
	}

	model := &scriptedSummaryModel{}
	contextConfig := agentpkg.DefaultContextConfig()
	contextConfig.MaxTokens = 80
	contextConfig.TriggerRatio = 0.5
	contextConfig.ReserveRatio = 0.2

	agent := mustAgent(agentpkg.NewAgent(
		"Friday",
		"Compress old context before the next reasoning step.",
		model,
		agentpkg.WithAgentState(state),
		agentpkg.WithOffloader(workspace),
		agentpkg.WithContextConfig(contextConfig),
	))
	must(agent.CompressContext(ctx))

	customState := agentpkg.NewAgentState()
	customAgent := mustAgent(agentpkg.NewAgent(
		"Friday",
		"Use a custom context strategy.",
		model,
		agentpkg.WithAgentState(customState),
		agentpkg.WithContextStrategies(staticContextStrategy{}),
	))
	must(customAgent.CompressContext(ctx))

	offloadPath := filepath.Join(workdir, "sessions", state.SessionID, "context.jsonl")
	_, offloadErr := os.Stat(offloadPath)
	fmt.Printf(
		"summary=%t remaining=%d offloaded=%t custom_summary=%q model_calls=%d\n",
		strings.Contains(state.Summary.Text, "demonstrate context compression"),
		len(state.Context),
		offloadErr == nil,
		customState.Summary.Text,
		len(model.callRequests),
	)
}

func mustTempDir(pattern string) string {
	dir, err := os.MkdirTemp("", pattern)
	if err != nil {
		panic(err)
	}
	return dir
}

func mustWorkspace(workspace *wslocal.Workspace, err error) *wslocal.Workspace {
	if err != nil {
		panic(err)
	}
	return workspace
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

func must(err error) {
	if err != nil {
		panic(err)
	}
}
