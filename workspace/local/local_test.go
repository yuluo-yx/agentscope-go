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

package local_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	toolmcp "github.com/yuluo-yx/agentscope-go/tool/mcp"
	"github.com/yuluo-yx/agentscope-go/workspace"
	"github.com/yuluo-yx/agentscope-go/workspace/local"
)

func TestWorkspacePersistsIndexesRestoresMCPAndReconcilesSkills(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := filepath.Join(t.TempDir(), "workspace")
	firstSkill := filepath.Join(t.TempDir(), "skill-one")
	secondSkill := filepath.Join(t.TempDir(), "skill-two")
	writeWorkspaceSkill(t, firstSkill, "review", "Review code", "First body.\n")
	writeWorkspaceSkill(t, secondSkill, "review", "Review code again", "Second body.\n")
	mcpClient, err := toolmcp.NewHTTPClient(
		"weather",
		toolmcp.HTTPConfig{URL: "https://example.invalid/mcp"},
		toolmcp.WithStateful(false),
		toolmcp.WithEnabledTools("forecast"),
	)
	if err != nil {
		t.Fatalf("NewHTTPClient returned error: %v", err)
	}

	ws, err := local.NewWorkspace(workdir, local.WithSkillPaths(firstSkill, secondSkill), local.WithMCPs(mcpClient))
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	initErr := ws.Initialize(ctx)
	if initErr != nil {
		t.Fatalf("Initialize returned error: %v", initErr)
	}
	assertFileExists(t, filepath.Join(workdir, ".mcp"))
	assertFileExists(t, filepath.Join(workdir, "skills", ".skills"))
	skills, err := ws.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills returned error: %v", err)
	}
	if got := skillNames(skills); strings.Join(got, ",") != "review,review (1)" {
		t.Fatalf("duplicate skill names should be made agent-facing unique: %#v", got)
	}

	manualSkill := filepath.Join(workdir, "skills", "manual")
	writeWorkspaceSkill(t, manualSkill, "manual", "Manual skill", "Manual body.\n")
	skills, err = ws.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills after manual add returned error: %v", err)
	}
	if !contains(skillNames(skills), "manual") {
		t.Fatalf("manual skill should be reconciled into .skills index: %#v", skillNames(skills))
	}
	removeErr := ws.RemoveSkill(ctx, "manual")
	if removeErr != nil {
		t.Fatalf("RemoveSkill returned error: %v", removeErr)
	}
	skills, err = ws.ListSkills(ctx)
	if err != nil {
		t.Fatalf("ListSkills after remove returned error: %v", err)
	}
	if contains(skillNames(skills), "manual") {
		t.Fatalf("manual skill should be removed from disk and index: %#v", skillNames(skills))
	}

	restored, err := local.NewWorkspace(workdir)
	if err != nil {
		t.Fatalf("NewWorkspace restored returned error: %v", err)
	}
	restoreInitErr := restored.Initialize(ctx)
	if restoreInitErr != nil {
		t.Fatalf("Initialize restored returned error: %v", restoreInitErr)
	}
	mcps, err := restored.ListMCPs(ctx)
	if err != nil {
		t.Fatalf("ListMCPs restored returned error: %v", err)
	}
	if len(mcps) != 1 || mcps[0].Name() != "weather" {
		t.Fatalf("restored .mcp should recreate weather MCP client: %#v", mcps)
	}
}

func TestWorkspaceOffloadsDataBlockMediaTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws, err := local.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	var _ workspace.Offloader = ws

	cases := []struct {
		name      string
		mediaType string
		wantExt   string
	}{
		{name: "png", mediaType: "image/png", wantExt: ".png"},
		{name: "mpeg_audio", mediaType: "audio/mpeg", wantExt: ".mp3"},
		{name: "mp4_video", mediaType: "video/mp4", wantExt: ".mp4"},
		{name: "pdf", mediaType: "application/pdf", wantExt: ".pdf"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := base64.StdEncoding.EncodeToString([]byte("payload-" + tt.name))
			block := message.NewDataBlock(
				message.NewBase64Source(payload, tt.mediaType),
				message.WithDataBlockName(tt.name),
			)
			offloaded, err := ws.OffloadDataBlock(ctx, block)
			if err != nil {
				t.Fatalf("OffloadDataBlock returned error: %v", err)
			}
			source, ok := offloaded.Source.(*message.URLSource)
			if !ok {
				t.Fatalf("OffloadDataBlock should return URL source: %#v", offloaded.Source)
			}
			if !strings.HasPrefix(source.URL, "file://") || !strings.HasSuffix(source.URL, tt.wantExt) {
				t.Fatalf("unexpected offload URL for %s: %s", tt.mediaType, source.URL)
			}
			path := strings.TrimPrefix(source.URL, "file://")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile offloaded data returned error: %v", err)
			}
			if string(data) != "payload-"+tt.name {
				t.Fatalf("offloaded data mismatch: %q", string(data))
			}
		})
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s should exist: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("%s should be a file", path)
	}
}

func skillNames(skills []workspace.Skill) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	return names
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func writeWorkspaceSkill(t *testing.T, dir, name, description, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
