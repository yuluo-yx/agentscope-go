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
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
	microworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace/microsandbox"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "microsandbox workspace example failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	root, err := os.MkdirTemp("", "agentscope-microsandbox-workspace-example-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	image := os.Getenv("AGENTSCOPE_MICROSANDBOX_IMAGE")
	if image == "" {
		image = "python:3.12"
	}
	sandboxName := os.Getenv("AGENTSCOPE_MICROSANDBOX_NAME")
	if sandboxName == "" {
		sandboxName = fmt.Sprintf("agentscope-msb-example-%d", time.Now().UnixNano())
	}
	hostWorkdir := filepath.Join(root, "workspace")
	ws, err := microworkspace.NewWorkspace(
		microworkspace.WithSandboxName(sandboxName),
		microworkspace.WithImage(image),
		microworkspace.WithHostWorkdir(hostWorkdir),
		microworkspace.WithRequestTimeout(90*time.Second),
		microworkspace.WithOpenTimeout(5*time.Minute),
	)
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	defer func() {
		if closeErr := ws.Close(context.Background()); closeErr != nil {
			fmt.Fprintf(os.Stderr, "close microsandbox workspace: %v\n", closeErr)
		}
	}()

	if err := ws.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize workspace for image %q: %w", image, err)
	}

	tools, err := ws.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	state := asstate.NewAgentState()
	briefPath := "/workspace/data/brief.md"
	content := "# Microsandbox workspace brief\nAgentScope Go can run workspace tools inside a local Microsandbox microVM.\n"
	writeResponse, err := runTool(ctx, findTool(tools, "Write"), map[string]any{
		"file_path": briefPath,
		"content":   content,
	}, state)
	if err != nil {
		return err
	}
	readResponse, err := runTool(ctx, findTool(tools, "Read"), map[string]any{
		"file_path": briefPath,
		"limit":     20,
	}, state)
	if err != nil {
		return err
	}
	readText := textContent(readResponse.Content)

	fmt.Printf(
		"microsandbox_workspace_alive=%t sandbox_name=%s image=%s tools=%s write=%s read_has_brief=%t\n",
		ws.IsAlive(),
		ws.SandboxName(),
		image,
		toolNames(tools),
		writeResponse.State,
		strings.Contains(readText, "Microsandbox workspace brief"),
	)

	apiKey := os.Getenv("AI_DASHSCOPE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}
	if err := callDashScope(ctx, apiKey, ws, readText); err != nil {
		return err
	}
	return nil
}

func callDashScope(ctx context.Context, apiKey string, ws *microworkspace.Workspace, readText string) error {
	maxTokens := int64(256)
	temperature := 0.2
	chat, err := dashscope.NewChatModel(
		credential.NewDashScope(apiKey).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(false),
		dashscope.WithChatParameters(dashscope.ChatParameters{MaxTokens: &maxTokens, Temperature: &temperature}),
	)
	if err != nil {
		return fmt.Errorf("create dashscope chat model: %w", err)
	}
	instructions, err := ws.GetInstructions(ctx)
	if err != nil {
		return fmt.Errorf("get workspace instructions: %w", err)
	}
	system, err := message.NewSystemMessage("system", instructions)
	if err != nil {
		return fmt.Errorf("create system message: %w", err)
	}
	user, err := message.NewUserMessage("user", "Summarize this Microsandbox workspace tool result in one short sentence:\n"+readText)
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}
	request := asmodel.CallRequest{Messages: []*message.Message{system, user}}
	tokens, err := chat.CountTokens(request)
	if err != nil {
		return fmt.Errorf("count tokens: %w", err)
	}
	response, err := chat.Call(ctx, request)
	if err != nil {
		return fmt.Errorf("dashscope chat call: %w", err)
	}
	responseText := ""
	if text := response.GetTextContent(); text != nil {
		responseText = *text
	}
	fmt.Printf("dashscope_live=ok chat_model=%s estimated_tokens=%d response=%q\n", chat.Name(), tokens, shorten(responseText, 120))
	return nil
}

func findTool(tools []asworkspace.Tool, name string) asworkspace.Tool {
	for _, current := range tools {
		if current.Name() == name {
			return current
		}
	}
	return nil
}

func runTool(ctx context.Context, current asworkspace.Tool, input map[string]any, state *asstate.AgentState) (*tool.ToolResponse, error) {
	if current == nil {
		return nil, fmt.Errorf("missing workspace tool")
	}
	chunks, err := current.Execute(ctx, input, state)
	if err != nil {
		return nil, fmt.Errorf("execute %s: %w", current.Name(), err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			return nil, fmt.Errorf("append %s chunk: %w", current.Name(), err)
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
