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
	"github.com/yuluo-yx/agentscope-go/credential"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
	dockerworkspace "github.com/yuluo-yx/agentscope-go/workspace/docker"
)

func main() {
	ctx := context.Background()
	root := mustTempDir("agentscope-docker-workspace-example-*")
	defer func() { _ = os.RemoveAll(root) }()

	image := getenv("AGENTSCOPE_DOCKER_IMAGE", "ubuntu:latest")
	hostWorkdir := filepath.Join(root, "workspace")
	ws := mustWorkspace(dockerworkspace.NewWorkspace(
		dockerworkspace.WithImage(image),
		dockerworkspace.WithHostWorkdir(hostWorkdir),
		dockerworkspace.WithPullImage(false),
		dockerworkspace.WithNetworkDisabled(true),
	))
	if err := ws.Initialize(ctx); err != nil {
		panic(fmt.Sprintf("initialize Docker workspace failed for image %q: %v", image, err))
	}
	defer func() {
		if err := ws.Close(context.Background()); err != nil {
			panic(err)
		}
	}()

	tools := mustTools(ws.ListTools(ctx))
	state := asstate.NewAgentState()
	briefPath := "/workspace/data/brief.md"
	writeResponse := runTool(ctx, findTool(tools, "Write"), map[string]any{
		"file_path": briefPath,
		"content":   fmt.Sprintf("# Docker workspace brief\nAgentScope Go can run workspace tools inside a Docker container. random check: %f\n", rand.Float32()),
	}, state)
	readResponse := runTool(ctx, findTool(tools, "Read"), map[string]any{
		"file_path": briefPath,
		"limit":     20,
	}, state)
	readText := textContent(readResponse.Content)
	maxTokens := int64(256)
	temperature := 0.2
	chat := mustModel(dashscope.NewChatModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(false),
		dashscope.WithChatParameters(dashscope.ChatParameters{MaxTokens: &maxTokens, Temperature: &temperature}),
	))
	user := mustMessage(message.NewUserMessage("user", "The Docker workspace Read tool returned, tips: print random float to check:\n"+readText))
	request := asmodel.CallRequest{Messages: []*message.Message{user}}
	tokens, err := chat.CountTokens(request)
	if err != nil {
		panic(err)
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
		panic(err)
	}
	responseText := ""
	if text := response.GetTextContent(); text != nil {
		responseText = *text
	}
	fmt.Printf("dashscope_live=ok response=%q\n", shorten(responseText, 120))
}

func findTool(tools []asworkspace.Tool, name string) asworkspace.Tool {
	for _, current := range tools {
		if current.Name() == name {
			return current
		}
	}
	panic("missing workspace tool: " + name)
}

func runTool(ctx context.Context, current asworkspace.Tool, input map[string]any, state *asstate.AgentState) *tool.ToolResponse {
	chunks, err := current.Execute(ctx, input, state)
	if err != nil {
		panic(err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			panic(err)
		}
	}
	return response
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

func getenv(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func shorten(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

func mustTempDir(pattern string) string {
	dir, err := os.MkdirTemp("", pattern)
	if err != nil {
		panic(err)
	}
	return dir
}

func mustWorkspace(ws *dockerworkspace.Workspace, err error) *dockerworkspace.Workspace {
	if err != nil {
		panic(err)
	}
	return ws
}

func mustTools(tools []asworkspace.Tool, err error) []asworkspace.Tool {
	if err != nil {
		panic(err)
	}
	return tools
}

func mustInstructions(instructions string, err error) string {
	if err != nil {
		panic(err)
	}
	return instructions
}

func mustModel(model asmodel.ChatModel, err error) asmodel.ChatModel {
	if err != nil {
		panic(err)
	}
	return model
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}
