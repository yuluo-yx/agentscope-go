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

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	wslocal "github.com/yuluo-yx/agentscope-go/pkg/workspace/local"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "workspace local example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	root, err := os.MkdirTemp("", "agentscope-workspace-example-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	skillDir := filepath.Join(root, "source-skill")
	if err := writeSkill(skillDir, "review", "Review files", "Read files before writing changes.\n"); err != nil {
		return err
	}

	workspacePath := filepath.Join(root, "workspace")
	ws, err := wslocal.NewWorkspace(workspacePath, wslocal.WithSkillPaths(skillDir))
	if err != nil {
		return fmt.Errorf("create local workspace: %w", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize workspace: %w", err)
	}

	tools, err := ws.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("list workspace tools: %w", err)
	}
	skills, err := ws.ListSkills(ctx)
	if err != nil {
		return fmt.Errorf("list workspace skills: %w", err)
	}
	state := asstate.NewAgentState()
	briefPath := filepath.Join(workspacePath, "data", "brief.md")
	writeTool, err := findTool(tools, "Write")
	if err != nil {
		return err
	}
	writeResponse, err := runTool(ctx, writeTool, map[string]any{
		"file_path": briefPath,
		"content":   "# Workspace brief\nUse the local workspace as the agent file environment.\n",
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
	user, err := message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("Use the workspace brief before answering."),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"), message.WithDataBlockName("hello.txt")),
	})
	if err != nil {
		return fmt.Errorf("create user message: %w", err)
	}
	contextPath, err := ws.OffloadContext(ctx, "session-1", []*message.Message{user})
	if err != nil {
		return fmt.Errorf("offload context: %w", err)
	}
	resultPath, err := ws.OffloadToolResult(ctx, "session-1", message.NewToolResultBlock(
		"read-call",
		"Read",
		message.ToolResultOutput{Blocks: readResponse.Content},
		readResponse.State,
	))
	if err != nil {
		return fmt.Errorf("offload tool result: %w", err)
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
	return nil
}

func findTool(tools []tool.Tool, name string) (tool.Tool, error) {
	for _, current := range tools {
		if current.Name() == name {
			return current, nil
		}
	}
	return nil, fmt.Errorf("missing workspace tool: %s", name)
}

func runTool(ctx context.Context, current tool.Tool, input map[string]any, state *asstate.AgentState) (*tool.ToolResponse, error) {
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

func writeSkill(dir, name, description, body string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}
	return nil
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
