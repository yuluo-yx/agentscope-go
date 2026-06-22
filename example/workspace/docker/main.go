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
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
	dockerworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace/docker"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "workspace docker example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	root, err := os.MkdirTemp("", "agentscope-docker-workspace-example-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	image := os.Getenv("AGENTSCOPE_DOCKER_IMAGE")
	if image == "" {
		image = "ubuntu:latest"
	}
	hostWorkdir := filepath.Join(root, "workspace")
	ws, err := dockerworkspace.NewWorkspace(
		dockerworkspace.WithImage(image),
		dockerworkspace.WithHostWorkdir(hostWorkdir),
		dockerworkspace.WithPullImage(false),
		dockerworkspace.WithNetworkDisabled(true),
	)
	if err != nil {
		return fmt.Errorf("create Docker workspace: %w", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize Docker workspace for image %q: %w", image, err)
	}
	defer func() {
		_ = ws.Close(context.Background())
	}()

	tools, err := ws.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("list Docker workspace tools: %w", err)
	}
	state := asstate.NewAgentState()
	briefPath := "/workspace/data/brief.md"
	writeTool, err := findTool(tools, "Write")
	if err != nil {
		return err
	}
	writeResponse, err := runTool(ctx, writeTool, map[string]any{
		"file_path": briefPath,
		"content":   fmt.Sprintf("# Docker workspace brief\nAgentScope Go can run workspace tools inside a Docker container. random check: %f\n", rand.Float32()),
	}, state)
	if err != nil {
		return err
	}
	readTool, err := findTool(tools, "Read")
	if err != nil {
		return err
	}
	readResponse, err := runTool(ctx, readTool, map[string]any{
		"file_path": briefPath,
		"limit":     20,
	}, state)
	if err != nil {
		return err
	}
	readText := textContent(readResponse.Content)
	maxTokens := int64(256)
	temperature := 0.2
	apiKey := os.Getenv("AI_DASHSCOPE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}
	chat, err := dashscope.NewChatModel(
		credential.NewDashScope(apiKey).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(false),
		dashscope.WithChatParameters(dashscope.ChatParameters{MaxTokens: &maxTokens, Temperature: &temperature}),
	)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}
	user, err := message.NewUserMessage("user", "The Docker workspace Read tool returned, tips: print random float to check:\n"+readText)
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}
	request := asmodel.CallRequest{Messages: []*message.Message{user}}
	tokens, err := chat.CountTokens(request)
	if err != nil {
		return fmt.Errorf("count tokens: %w", err)
	}

	fmt.Printf(
		"docker_workspace_alive=%t image=%s tools=%s write=%s read_has_brief=%t chat_model=%s estimated_tokens=%d\n",
		ws.IsAlive(),
		image,
		toolNames(tools),
		writeResponse.State,
		strings.Contains(readText, "Docker workspace brief"),
		chat.Name(),
		tokens,
	)

	response, err := chat.Call(ctx, request)
	if err != nil {
		return fmt.Errorf("call DashScope chat: %w", err)
	}
	responseText := ""
	if text := response.GetTextContent(); text != nil {
		responseText = *text
	}
	fmt.Printf("dashscope_live=ok response=%q\n", shorten(responseText, 120))
	return nil
}

func findTool(tools []asworkspace.Tool, name string) (asworkspace.Tool, error) {
	for _, current := range tools {
		if current.Name() == name {
			return current, nil
		}
	}
	return nil, fmt.Errorf("missing workspace tool: %s", name)
}

func runTool(ctx context.Context, current asworkspace.Tool, input map[string]any, state *asstate.AgentState) (*tool.ToolResponse, error) {
	chunks, err := current.Execute(ctx, input, state)
	if err != nil {
		return nil, fmt.Errorf("execute %s tool: %w", current.Name(), err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			return nil, fmt.Errorf("append %s tool chunk: %w", current.Name(), err)
		}
	}
	return response, nil
}

func toolNames(tools []asworkspace.Tool) string {
	names := make([]string, 0, len(tools))
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return strings.Join(names, ",")
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

func shorten(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
