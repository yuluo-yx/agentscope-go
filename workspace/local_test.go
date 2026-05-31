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

package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/workspace"
)

func TestLocalWorkspaceInitializesToolsAndSeedSkills(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sourceSkill := t.TempDir()
	writeWorkspaceSkill(t, sourceSkill, "review", "Review code", "Read files first.\n")

	workdir := filepath.Join(t.TempDir(), "workspace")
	ws, err := workspace.NewLocalWorkspace(workdir, workspace.WithSkillPaths(sourceSkill))
	if err != nil {
		t.Fatalf("NewLocalWorkspace returned error: %v", err)
	}
	if initErr := ws.Initialize(ctx); initErr != nil {
		t.Fatalf("Initialize returned error: %v", initErr)
	}
	assertWorkspaceLifecycle(t, ws, workdir)
	assertWorkspaceTools(t, ctx, ws)
	assertWorkspaceSkills(t, ctx, ws)
	assertWorkspaceInstructions(t, ctx, ws, workdir)
}

func assertWorkspaceLifecycle(t *testing.T, ws *workspace.LocalWorkspace, workdir string) {
	t.Helper()

	if !ws.IsAlive() || ws.WorkspaceID() == "" {
		t.Fatalf("workspace lifecycle fields not set: alive=%v id=%q", ws.IsAlive(), ws.WorkspaceID())
	}
	for _, subdir := range []string{"data", "skills", "sessions"} {
		info, statErr := os.Stat(filepath.Join(workdir, subdir))
		if statErr != nil {
			t.Fatalf("%s directory not initialized: %v", subdir, statErr)
		}
		if !info.IsDir() {
			t.Fatalf("%s path should be a directory: %#v", subdir, info)
		}
	}
}

func assertWorkspaceTools(t *testing.T, ctx context.Context, ws *workspace.LocalWorkspace) {
	t.Helper()

	tools, err := ws.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if got := toolNames(tools); strings.Join(got, ",") != "Bash,Edit,Glob,Grep,Read,Write" {
		t.Fatalf("unexpected workspace tools: %#v", got)
	}
}

func assertWorkspaceSkills(t *testing.T, ctx context.Context, ws *workspace.LocalWorkspace) {
	t.Helper()

	skills, err := ws.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills returned error: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "review" || skills[0].Markdown != "Read files first.\n" {
		t.Fatalf("seed skill not loaded: %#v", skills)
	}
}

func assertWorkspaceInstructions(t *testing.T, ctx context.Context, ws *workspace.LocalWorkspace, workdir string) {
	t.Helper()

	instructions, err := ws.GetInstructions(ctx)
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if !strings.Contains(instructions, workdir) {
		t.Fatalf("instructions should mention workdir %q: %s", workdir, instructions)
	}
}

func TestLocalWorkspaceOffloadsContextAndToolResults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws, err := workspace.NewLocalWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalWorkspace returned error: %v", err)
	}
	if initErr := ws.Initialize(ctx); initErr != nil {
		t.Fatalf("Initialize returned error: %v", initErr)
	}

	msg, err := message.NewUserMessage("user", []message.ContentBlock{
		message.NewTextBlock("inspect"),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"), message.WithDataBlockName("hello.txt")),
	})
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	contextPath, err := ws.OffloadContext(ctx, "session-1", []*message.Message{msg})
	if err != nil {
		t.Fatalf("OffloadContext returned error: %v", err)
	}
	contextBytes, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("ReadFile context returned error: %v", err)
	}
	contextText := string(contextBytes)
	if strings.Contains(contextText, "aGVsbG8=") || !strings.Contains(contextText, "file://") {
		t.Fatalf("context should replace base64 data with file URL: %s", contextText)
	}

	toolResult := message.NewToolResultBlock("call-1", "Read", message.ToolResultOutput{Blocks: message.ContentBlockList{
		message.NewTextBlock("text:"),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"), message.WithDataBlockName("hello.txt")),
	}}, message.ToolResultSuccess)
	resultPath, err := ws.OffloadToolResult(ctx, "session-1", toolResult)
	if err != nil {
		t.Fatalf("OffloadToolResult returned error: %v", err)
	}
	resultBytes, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("ReadFile result returned error: %v", err)
	}
	resultText := string(resultBytes)
	if !strings.Contains(resultText, "text:") || !strings.Contains(resultText, "<data url='file://") || !strings.Contains(resultText, "media_type='text/plain'") {
		t.Fatalf("tool result offload content mismatch: %s", resultText)
	}
}

func toolNames(tools []workspace.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	return names
}

func writeWorkspaceSkill(t *testing.T, dir, name, description, body string) {
	t.Helper()
	if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
		t.Fatalf("MkdirAll returned error: %v", mkdirErr)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
	if writeErr := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); writeErr != nil {
		t.Fatalf("WriteFile returned error: %v", writeErr)
	}
}
