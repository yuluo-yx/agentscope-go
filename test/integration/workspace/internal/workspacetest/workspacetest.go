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

// Package workspacetest contains shared workspace integration test helpers.
package workspacetest

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

// ExerciseToolsAndOffload verifies the common workspace tool and offload contract.
func ExerciseToolsAndOffload(t testing.TB, ctx context.Context, ws asworkspace.Workspace, filePath string) {
	t.Helper()

	initializeWorkspace(t, ctx, ws)
	tools := listWorkspaceTools(t, ctx, ws)
	editedReadResponse := exerciseFileTools(t, ctx, tools, filePath)
	exerciseOffload(t, ctx, ws, editedReadResponse)
}

func initializeWorkspace(t testing.TB, ctx context.Context, ws asworkspace.Workspace) {
	t.Helper()

	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := ws.Close(context.Background()); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	instructions, err := ws.GetInstructions(ctx)
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if instructions == "" || !ws.IsAlive() {
		t.Fatalf("workspace should be alive with instructions, alive=%v instructions=%q", ws.IsAlive(), instructions)
	}
}

func listWorkspaceTools(t testing.TB, ctx context.Context, ws asworkspace.Workspace) []asworkspace.Tool {
	t.Helper()

	tools, err := ws.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if names := toolNames(tools); strings.Join(names, ",") != "Bash,Edit,Glob,Grep,Read,Write" {
		t.Fatalf("unexpected workspace tools: %#v", names)
	}
	return tools
}

func exerciseFileTools(t testing.TB, ctx context.Context, tools []asworkspace.Tool, filePath string) *tool.ToolResponse {
	t.Helper()

	state := asstate.NewAgentState()
	writeResponse := runTool(t, ctx, findTool(t, tools, "Write"), map[string]any{
		"file_path": filePath,
		"content":   "AgentScope Go workspace integration\n",
	}, state)
	if writeResponse.State != message.ToolResultSuccess {
		t.Fatalf("Write failed: state=%s output=%q", writeResponse.State, textContent(writeResponse.Content))
	}

	readResponse := runTool(t, ctx, findTool(t, tools, "Read"), map[string]any{
		"file_path": filePath,
		"limit":     20,
	}, state)
	if !strings.Contains(textContent(readResponse.Content), "workspace integration") {
		t.Fatalf("Read output should contain written content: %q", textContent(readResponse.Content))
	}

	editResponse := runTool(t, ctx, findTool(t, tools, "Edit"), map[string]any{
		"file_path":  filePath,
		"old_string": "integration",
		"new_string": "sandbox integration",
	}, state)
	if editResponse.State != message.ToolResultSuccess {
		t.Fatalf("Edit failed: state=%s output=%q", editResponse.State, textContent(editResponse.Content))
	}

	editedReadResponse := runTool(t, ctx, findTool(t, tools, "Read"), map[string]any{
		"file_path": filePath,
		"limit":     20,
	}, state)
	if !strings.Contains(textContent(editedReadResponse.Content), "sandbox integration") {
		t.Fatalf("Read output should contain edited content: %q", textContent(editedReadResponse.Content))
	}
	return editedReadResponse
}

func exerciseOffload(t testing.TB, ctx context.Context, ws asworkspace.Workspace, editedReadResponse *tool.ToolResponse) {
	t.Helper()

	userMessage, err := message.NewUserMessage("user", message.ContentBlockList{
		message.NewTextBlock("Keep this workspace context."),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"), message.WithDataBlockName("hello.txt")),
	})
	user := mustMessage(t, userMessage, err)
	contextPath, err := ws.OffloadContext(ctx, "session-1", []*message.Message{user})
	if err != nil {
		t.Fatalf("OffloadContext returned error: %v", err)
	}
	if !fileExists(contextPath) {
		t.Fatalf("offloaded context file does not exist: %s", contextPath)
	}
	resultPath, err := ws.OffloadToolResult(ctx, "session-1", message.NewToolResultBlock(
		"read-call",
		"Read",
		message.ToolResultOutput{Blocks: editedReadResponse.Content},
		editedReadResponse.State,
	))
	if err != nil {
		t.Fatalf("OffloadToolResult returned error: %v", err)
	}
	if !fileExists(resultPath) {
		t.Fatalf("offloaded tool result file does not exist: %s", resultPath)
	}
}

func findTool(t testing.TB, tools []asworkspace.Tool, name string) asworkspace.Tool {
	t.Helper()

	for _, current := range tools {
		if current.Name() == name {
			return current
		}
	}
	t.Fatalf("missing workspace tool %s", name)
	return nil
}

func runTool(t testing.TB, ctx context.Context, current asworkspace.Tool, input map[string]any, state *asstate.AgentState) *tool.ToolResponse {
	t.Helper()

	chunks, err := current.Execute(ctx, input, state)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			t.Fatalf("AppendChunk returned error: %v", err)
		}
	}
	return response
}

func toolNames(tools []asworkspace.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, current := range tools {
		names = append(names, current.Name())
	}
	return names
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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func mustMessage(t testing.TB, msg *message.Message, err error) *message.Message {
	t.Helper()

	if err != nil {
		t.Fatalf("message constructor returned error: %v", err)
	}
	return msg
}
