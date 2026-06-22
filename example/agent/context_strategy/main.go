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

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	wslocal "github.com/yuluo-yx/agentscope-go/pkg/workspace/local"
)

type staticContextStrategy struct{}

func (staticContextStrategy) ContextStrategyName() string {
	return "static-summary"
}

func (staticContextStrategy) ApplyContextStrategy(_ context.Context, input *agentpkg.ContextStrategyInput) error {
	input.State.Summary.Text = "summary from a custom context strategy"
	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agent context strategy example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	workdir, err := os.MkdirTemp("", "agentscope-context-strategy-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	workspace, err := wslocal.NewWorkspace(workdir)
	if err != nil {
		return fmt.Errorf("create local workspace: %w", err)
	}
	if err := workspace.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize workspace: %w", err)
	}
	defer func() {
		_ = workspace.Close(context.Background())
	}()

	state := agentpkg.NewAgentState()
	oldUser, err := message.NewUserMessage("user", strings.Repeat("old user context ", 20))
	if err != nil {
		return fmt.Errorf("create old user message: %w", err)
	}
	oldAssistant, err := message.NewAssistantMessage("Friday", strings.Repeat("old assistant context ", 20))
	if err != nil {
		return fmt.Errorf("create old assistant message: %w", err)
	}
	recentUser, err := message.NewUserMessage("user", "keep this recent instruction")
	if err != nil {
		return fmt.Errorf("create recent user message: %w", err)
	}
	state.Context = []*message.Message{oldUser, oldAssistant, recentUser}

	model, err := newDashScopeChatModel(false)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}
	contextConfig := agentpkg.DefaultContextConfig()
	contextConfig.MaxTokens = 80
	contextConfig.TriggerRatio = 0.5
	contextConfig.ReserveRatio = 0.2

	agent, err := agentpkg.NewAgent(
		"Friday",
		"Compress old context before the next reasoning step.",
		model,
		agentpkg.WithAgentState(state),
		agentpkg.WithOffloader(workspace),
		agentpkg.WithContextConfig(contextConfig),
	)
	if err != nil {
		return fmt.Errorf("create compression agent: %w", err)
	}
	if err := agent.CompressContext(ctx); err != nil {
		return fmt.Errorf("compress context: %w", err)
	}

	customState := agentpkg.NewAgentState()
	customAgent, err := agentpkg.NewAgent(
		"Friday",
		"Use a custom context strategy.",
		model,
		agentpkg.WithAgentState(customState),
		agentpkg.WithContextStrategies(staticContextStrategy{}),
	)
	if err != nil {
		return fmt.Errorf("create custom strategy agent: %w", err)
	}
	if err := customAgent.CompressContext(ctx); err != nil {
		return fmt.Errorf("compress context with custom strategy: %w", err)
	}

	offloadPath := filepath.Join(workdir, "sessions", state.SessionID, "context.jsonl")
	_, offloadErr := os.Stat(offloadPath)
	fmt.Printf(
		"summary=%t remaining=%d offloaded=%t custom_summary=%q model=%s\n",
		strings.TrimSpace(state.Summary.Text) != "",
		len(state.Context),
		offloadErr == nil,
		customState.Summary.Text,
		model.Name(),
	)
	return nil
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
