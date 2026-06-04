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

	"github.com/yuluo-yx/agentscope-go/message"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	wslocal "github.com/yuluo-yx/agentscope-go/workspace/local"
)

func main() {
	ctx := context.Background()
	root := mustTempDir("agentscope-workspace-example-*")
	defer func() { _ = os.RemoveAll(root) }()

	skillDir := filepath.Join(root, "source-skill")
	writeSkill(skillDir, "review", "Review files", "Read files before writing changes.\n")

	workspacePath := filepath.Join(root, "workspace")
	ws := mustWorkspace(wslocal.NewWorkspace(workspacePath, wslocal.WithSkillPaths(skillDir)))
	if err := ws.Initialize(ctx); err != nil {
		panic(err)
	}

	tools, err := ws.ListTools(ctx)
	if err != nil {
		panic(err)
	}
	skills, err := ws.ListSkills(ctx)
	if err != nil {
		panic(err)
	}
	state := asstate.NewAgentState()
	briefPath := filepath.Join(workspacePath, "data", "brief.md")
	writeResponse := runTool(ctx, findTool(tools, "Write"), map[string]any{
		"file_path": briefPath,
		"content":   "# Workspace brief\nUse the local workspace as the agent file environment.\n",
	}, state)
	readResponse := runTool(ctx, findTool(tools, "Read"), map[string]any{
		"file_path": briefPath,
		"limit":     20,
	}, state)
	user := mustMessage(message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("Use the workspace brief before answering."),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"), message.WithDataBlockName("hello.txt")),
	}))
	contextPath, err := ws.OffloadContext(ctx, "session-1", []*message.Message{user})
	if err != nil {
		panic(err)
	}
	resultPath, err := ws.OffloadToolResult(ctx, "session-1", message.NewToolResultBlock(
		"read-call",
		"Read",
		message.ToolResultOutput{Blocks: readResponse.Content},
		readResponse.State,
	))
	if err != nil {
		panic(err)
	}
	readText := readResponse.GetTextContent()

	fmt.Printf(
		"workspace_alive=%t tools=%d skills=%d write=%s read_has_brief=%t context_file=%t result_file=%t tool_names=%s\n",
		ws.IsAlive(),
		len(tools),
		len(skills),
		writeResponse.State,
		readText != nil && strings.Contains(*readText, "Workspace brief"),
		fileExists(contextPath),
		fileExists(resultPath),
		toolNames(tools),
	)
}

func findTool(tools []tool.Tool, name string) tool.Tool {
	for _, current := range tools {
		if current.Name() == name {
			return current
		}
	}
	panic("missing workspace tool: " + name)
}

func runTool(ctx context.Context, current tool.Tool, input map[string]any, state *asstate.AgentState) *tool.ToolResponse {
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

func writeSkill(dir, name, description, body string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		panic(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		panic(err)
	}
}

func toolNames(tools []tool.Tool) string {
	names := make([]string, 0, len(tools))
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return strings.Join(names, ",")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func mustTempDir(pattern string) string {
	dir, err := os.MkdirTemp("", pattern)
	if err != nil {
		panic(err)
	}
	return dir
}

func mustWorkspace(ws *wslocal.Workspace, err error) *wslocal.Workspace {
	if err != nil {
		panic(err)
	}
	return ws
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}
