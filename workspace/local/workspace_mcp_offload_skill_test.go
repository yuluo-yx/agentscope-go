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

package local

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

func TestWorkspaceOptionsNilCanceledAndBasicMethods(t *testing.T) {
	t.Parallel()

	if _, err := NewWorkspace(" "); err == nil || !strings.Contains(err.Error(), "workdir is empty") {
		t.Fatalf("NewWorkspace should reject empty workdir, got %v", err)
	}
	ws, err := NewWorkspace(
		t.TempDir(),
		WithWorkspaceID(""),
		WithInstructions("local={workdir}"),
		WithMCPClientFactory(nil),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if ws.WorkspaceID() == "" || ws.instructions != "local={workdir}" || ws.mcpFactory == nil {
		t.Fatalf("workspace options/defaults mismatch: %#v", ws)
	}
	if ws.WorkspaceRoot() == "" || (*Workspace)(nil).WorkspaceRoot() != "" {
		t.Fatalf("WorkspaceRoot mismatch: %q", ws.WorkspaceRoot())
	}
	instructions, err := ws.GetInstructions(context.Background())
	if err != nil {
		t.Fatalf("GetInstructions returned error: %v", err)
	}
	if !strings.HasPrefix(instructions, "local=") || !strings.Contains(instructions, ws.workdir) {
		t.Fatalf("instructions substitution mismatch: %q", instructions)
	}
	tools, err := ws.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if got := len(tools); got != 6 {
		t.Fatalf("ListTools length = %d, want 6", got)
	}
	if err := ws.Close(context.Background()); err != nil || ws.IsAlive() {
		t.Fatalf("Close should mark workspace inactive, alive=%v err=%v", ws.IsAlive(), err)
	}

	var nilWS *Workspace
	if nilWS.WorkspaceID() != "" || nilWS.IsAlive() {
		t.Fatalf("nil workspace identity/lifecycle mismatch")
	}
	if err := nilWS.Initialize(context.Background()); err == nil || !strings.Contains(err.Error(), "nil local workspace") {
		t.Fatalf("nil Initialize error = %v", err)
	}
	if err := nilWS.Close(context.Background()); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if err := nilWS.Reset(context.Background()); err != nil {
		t.Fatalf("nil Reset returned error: %v", err)
	}
	if _, err := nilWS.GetInstructions(context.Background()); err == nil || !strings.Contains(err.Error(), "nil local workspace") {
		t.Fatalf("nil GetInstructions error = %v", err)
	}
	if mcps, err := nilWS.ListMCPs(context.Background()); err != nil || len(mcps) != 0 {
		t.Fatalf("nil ListMCPs = %#v, %v", mcps, err)
	}
	if _, err := nilWS.ListSkills(context.Background()); err == nil || !strings.Contains(err.Error(), "nil local workspace") {
		t.Fatalf("nil ListSkills error = %v", err)
	}
	if err := nilWS.AddMCP(context.Background(), &testLocalMCP{name: "nil"}); err == nil || !strings.Contains(err.Error(), "nil local workspace") {
		t.Fatalf("nil AddMCP error = %v", err)
	}
	if err := nilWS.RemoveMCP(context.Background(), "nil"); err != nil {
		t.Fatalf("nil RemoveMCP returned error: %v", err)
	}
	if err := nilWS.AddSkill(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "nil local workspace") {
		t.Fatalf("nil AddSkill error = %v", err)
	}
	if err := nilWS.RemoveSkill(context.Background(), "skill"); err != nil {
		t.Fatalf("nil RemoveSkill returned error: %v", err)
	}
	if _, err := nilWS.OffloadContext(context.Background(), "session", nil); err == nil || !strings.Contains(err.Error(), "nil local workspace") {
		t.Fatalf("nil OffloadContext error = %v", err)
	}
	if _, err := nilWS.OffloadToolResult(context.Background(), "session", nil); err == nil || !strings.Contains(err.Error(), "nil local workspace") {
		t.Fatalf("nil OffloadToolResult error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ws.Initialize(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Initialize canceled error = %v", err)
	}
	if err := ws.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close canceled error = %v", err)
	}
	if err := ws.Reset(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reset canceled error = %v", err)
	}
	if _, err := ws.GetInstructions(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetInstructions canceled error = %v", err)
	}
	if _, err := ws.ListTools(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListTools canceled error = %v", err)
	}
	if _, err := ws.ListMCPs(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListMCPs canceled error = %v", err)
	}
	if _, err := ws.ListSkills(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListSkills canceled error = %v", err)
	}
	if err := ws.AddMCP(canceled, &testLocalMCP{name: "canceled"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("AddMCP canceled error = %v", err)
	}
	if err := ws.RemoveMCP(canceled, "canceled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RemoveMCP canceled error = %v", err)
	}
	if err := ws.AddSkill(canceled, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("AddSkill canceled error = %v", err)
	}
	if err := ws.RemoveSkill(canceled, "skill"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RemoveSkill canceled error = %v", err)
	}
	if _, err := ws.OffloadContext(canceled, "session", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("OffloadContext canceled error = %v", err)
	}
	if _, err := ws.OffloadToolResult(canceled, "session", message.NewToolResultBlock("id", "Tool", message.ToolResultOutput{Raw: "x"}, message.ToolResultSuccess)); !errors.Is(err, context.Canceled) {
		t.Fatalf("OffloadToolResult canceled error = %v", err)
	}
	if _, err := ws.OffloadDataBlock(canceled, message.NewDataBlock(message.NewBase64Source("eA==", "text/plain"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("OffloadDataBlock canceled error = %v", err)
	}
}

func TestWorkspaceMCPRegistrationPersistenceAndFactoryBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	stateful := &testLocalMCP{
		name:      "stateful",
		stateful:  true,
		connected: false,
		config: asworkspace.MCPClientConfig{
			Name:     "stateful",
			Type:     asworkspace.MCPClientTypeHTTP,
			Stateful: true,
			HTTP:     &asworkspace.MCPHTTPConfig{URL: "http://localhost/stateful"},
		},
	}
	ws, err := NewWorkspace(workdir, WithMCPs(nil, stateful))
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if !ws.IsAlive() || !stateful.connectCalled || !stateful.connected {
		t.Fatalf("Initialize should connect stateful MCP, alive=%v mcp=%#v", ws.IsAlive(), stateful)
	}
	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize should be idempotent: %v", err)
	}
	if err := ws.AddMCP(ctx, nil); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("AddMCP nil error = %v", err)
	}
	if err := ws.AddMCP(ctx, stateful); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("AddMCP duplicate error = %v", err)
	}
	stateless := &testLocalMCP{
		name: "stateless",
		config: asworkspace.MCPClientConfig{
			Name: "stateless",
			Type: asworkspace.MCPClientTypeStdio,
			Stdio: &asworkspace.MCPStdioConfig{
				Command: "server",
				Args:    []string{"--port", "0"},
				Env:     map[string]string{"TOKEN": "secret"},
			},
		},
	}
	if err := ws.AddMCP(ctx, stateless); err != nil {
		t.Fatalf("AddMCP stateless returned error: %v", err)
	}
	mcps, err := ws.ListMCPs(ctx)
	if err != nil {
		t.Fatalf("ListMCPs returned error: %v", err)
	}
	last := len(mcps) - 1
	mcps[last] = nil
	if ws.mcps[last] == nil {
		t.Fatalf("ListMCPs should return a shallow copy")
	}
	if err := ws.RemoveMCP(ctx, "missing"); err != nil {
		t.Fatalf("RemoveMCP missing returned error: %v", err)
	}
	if err := ws.RemoveMCP(ctx, "stateful"); err != nil {
		t.Fatalf("RemoveMCP stateful returned error: %v", err)
	}
	if !stateful.closeCalled || stateful.connected {
		t.Fatalf("RemoveMCP should close connected stateful MCP, got %#v", stateful)
	}
	if err := ws.AddMCP(ctx, nonPersistableLocalMCP{name: "runtime"}); err == nil || !strings.Contains(err.Error(), "cannot be persisted") {
		t.Fatalf("AddMCP non-persistable error = %v", err)
	}

	var configs []asworkspace.MCPClientConfig
	data, err := os.ReadFile(filepath.Join(workdir, ".mcp"))
	if err != nil {
		t.Fatalf("read .mcp returned error: %v", err)
	}
	if err := json.Unmarshal(data, &configs); err != nil {
		t.Fatalf("unmarshal .mcp returned error: %v", err)
	}
	if len(configs) != 1 || configs[0].Name != "stateless" {
		t.Fatalf("persisted MCP configs mismatch: %#v", configs)
	}

	invalidDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(invalidDir, ".mcp"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write invalid .mcp returned error: %v", err)
	}
	invalidWS, err := NewWorkspace(invalidDir)
	if err != nil {
		t.Fatalf("NewWorkspace invalid returned error: %v", err)
	}
	if err := invalidWS.Initialize(ctx); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("Initialize invalid .mcp error = %v", err)
	}

	restoreDir := t.TempDir()
	restoreConfigs := []asworkspace.MCPClientConfig{{
		Name: "restored",
		Type: asworkspace.MCPClientTypeHTTP,
		HTTP: &asworkspace.MCPHTTPConfig{URL: "http://localhost/restored"},
	}}
	restoreData, err := json.Marshal(restoreConfigs)
	if err != nil {
		t.Fatalf("Marshal restore configs returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(restoreDir, ".mcp"), restoreData, 0o600); err != nil {
		t.Fatalf("write restore .mcp returned error: %v", err)
	}
	factoryErr := errors.New("factory failed")
	restoreWS, err := NewWorkspace(restoreDir, WithMCPClientFactory(func(asworkspace.MCPClientConfig) (asworkspace.MCPClient, error) {
		return nil, factoryErr
	}))
	if err != nil {
		t.Fatalf("NewWorkspace restore returned error: %v", err)
	}
	if err := restoreWS.Initialize(ctx); !errors.Is(err, factoryErr) {
		t.Fatalf("Initialize factory error = %v", err)
	}
}

func TestWorkspaceOffloadsContextToolResultsAndDataBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	if err := ws.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString([]byte("context-data"))
	dataBlock := message.NewDataBlock(
		message.NewBase64Source(payload, "text/plain"),
		message.WithDataBlockID("data-1"),
		message.WithDataBlockName("context.txt"),
	)
	toolResult := message.NewToolResultBlock(
		"tool-1",
		"Reader",
		message.ToolResultOutput{Blocks: message.ContentBlockList{
			message.NewTextBlock("nested "),
			message.NewDataBlock(message.NewBase64Source(payload, "image/gif"), message.WithDataBlockName("nested.gif")),
		}},
		message.ToolResultSuccess,
	)
	msg, err := message.NewAssistantMessage("assistant", []message.ContentBlock{dataBlock, toolResult})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	contextPath, err := ws.OffloadContext(ctx, "session-1", []*message.Message{nil, msg})
	if err != nil {
		t.Fatalf("OffloadContext returned error: %v", err)
	}
	contextData, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("ReadFile context returned error: %v", err)
	}
	if !strings.Contains(string(contextData), "file://") || strings.Contains(string(contextData), payload) {
		t.Fatalf("OffloadContext should replace base64 data blocks, got %s", string(contextData))
	}
	if _, ok := dataBlock.Source.(*message.Base64Source); !ok {
		t.Fatalf("OffloadContext should clone messages before replacing data blocks")
	}

	if _, err := ws.OffloadToolResult(ctx, "session-1", nil); err == nil || !strings.Contains(err.Error(), "nil tool result") {
		t.Fatalf("OffloadToolResult nil error = %v", err)
	}
	rawResult := message.NewToolResultBlock("call-1", "Bash", message.ToolResultOutput{Raw: "raw output"}, message.ToolResultSuccess)
	firstPath, err := ws.OffloadToolResult(ctx, "session-1", rawResult)
	if err != nil {
		t.Fatalf("OffloadToolResult raw returned error: %v", err)
	}
	secondPath, err := ws.OffloadToolResult(ctx, "session-1", rawResult)
	if err != nil {
		t.Fatalf("OffloadToolResult duplicate returned error: %v", err)
	}
	if firstPath == secondPath || !strings.Contains(secondPath, "(1)") {
		t.Fatalf("OffloadToolResult should use unique filenames, first=%q second=%q", firstPath, secondPath)
	}
	blockResult := message.NewToolResultBlock(
		"call-2",
		"Reader",
		message.ToolResultOutput{Blocks: message.ContentBlockList{
			message.NewTextBlock("text "),
			message.NewDataBlock(message.NewBase64Source(payload, "image/png"), message.WithDataBlockName("image.png")),
			message.NewDataBlock(message.NewURLSource("https://example.test/file.txt", "text/plain"), message.WithDataBlockName("remote.txt")),
		}},
		message.ToolResultSuccess,
	)
	blockPath, err := ws.OffloadToolResult(ctx, "session-1", blockResult)
	if err != nil {
		t.Fatalf("OffloadToolResult blocks returned error: %v", err)
	}
	blockData, err := os.ReadFile(blockPath)
	if err != nil {
		t.Fatalf("ReadFile block tool result returned error: %v", err)
	}
	if !strings.Contains(string(blockData), "text ") || !strings.Contains(string(blockData), "<data url='file://") || !strings.Contains(string(blockData), "https://example.test/file.txt") {
		t.Fatalf("OffloadToolResult block output mismatch: %s", string(blockData))
	}

	urlBlock := message.NewDataBlock(message.NewURLSource("https://example.test/image.png", "image/png"), message.WithDataBlockName("remote"))
	cloned, err := ws.OffloadDataBlock(ctx, urlBlock)
	if err != nil {
		t.Fatalf("OffloadDataBlock URL returned error: %v", err)
	}
	if cloned == urlBlock || cloned.Name == nil || *cloned.Name != "remote" {
		t.Fatalf("OffloadDataBlock URL source should return a cloned block preserving name: %#v", cloned)
	}
	if _, err := ws.OffloadDataBlock(ctx, message.NewDataBlock(message.NewBase64Source("not-base64", "text/plain"))); err == nil {
		t.Fatalf("OffloadDataBlock should reject invalid base64")
	}
	for mediaType, want := range map[string]string{
		"image/jpeg":       ".jpg",
		"image/png":        ".png",
		"image/webp":       ".webp",
		"image/svg+xml":    ".svg",
		"audio/mpeg":       ".mp3",
		"audio/wav":        ".wav",
		"audio/x-wav":      ".wav",
		"audio/ogg":        ".ogg",
		"video/mp4":        ".mp4",
		"video/webm":       ".webm",
		"application/pdf":  ".pdf",
		"text/plain":       ".txt",
		"application/json": ".json",
		"unknown/type":     "",
	} {
		if got := mediaExtension(mediaType); got != want {
			t.Fatalf("mediaExtension(%q) = %q, want %q", mediaType, got, want)
		}
	}
}

func TestWorkspaceSkillIndexAndHelperBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	ws, err := NewWorkspace(workdir)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	skillsDir := filepath.Join(workdir, "skills")
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		t.Fatalf("MkdirAll skills returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, ".skills"), []byte(`{"skills":null}`), 0o600); err != nil {
		t.Fatalf("write .skills returned error: %v", err)
	}
	index, err := ws.loadSkillsIndex(skillsDir)
	if err != nil {
		t.Fatalf("loadSkillsIndex returned error: %v", err)
	}
	if index.Skills == nil {
		t.Fatalf("loadSkillsIndex should initialize nil Skills map")
	}
	if err := os.WriteFile(filepath.Join(skillsDir, ".skills"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write invalid .skills returned error: %v", err)
	}
	index, err = ws.loadSkillsIndex(skillsDir)
	if err != nil || len(index.Skills) != 0 {
		t.Fatalf("invalid .skills should load as empty index, index=%#v err=%v", index, err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, ".skills"), nil, 0o600); err != nil {
		t.Fatalf("write empty .skills returned error: %v", err)
	}
	index, err = ws.loadSkillsIndex(skillsDir)
	if err != nil || len(index.Skills) != 0 {
		t.Fatalf("empty .skills should load as empty index, index=%#v err=%v", index, err)
	}

	first := filepath.Join(skillsDir, "first")
	duplicate := filepath.Join(skillsDir, "duplicate")
	invalid := filepath.Join(skillsDir, "invalid")
	writeLocalCoverageSkill(t, first, "review", "Review code", "body")
	writeLocalCoverageSkill(t, duplicate, "review", "Review code", "body")
	if err := os.MkdirAll(invalid, 0o700); err != nil {
		t.Fatalf("MkdirAll invalid skill returned error: %v", err)
	}
	actual, err := skillSubdirs(skillsDir)
	if err != nil {
		t.Fatalf("skillSubdirs returned error: %v", err)
	}
	index.Skills = map[string]skillIndexEntry{"missing": {Hash: "old", SkillName: "old"}}
	if !removeMissingSkillEntries(&index, actual) || len(index.Skills) != 0 {
		t.Fatalf("removeMissingSkillEntries should remove stale entries: %#v", index)
	}
	if !addUnindexedSkills(skillsDir, &index, actual, skillNameSet(index.Skills), skillHashSet(index.Skills)) {
		t.Fatalf("addUnindexedSkills should add valid skills")
	}
	if len(index.Skills) != 1 {
		t.Fatalf("duplicate hash and invalid skill should be skipped, got %#v", index.Skills)
	}
	if entry, ok := skillIndexEntryForDir(skillsDir, "invalid", skillNameSet(index.Skills), skillHashSet(index.Skills)); ok || entry.SkillName != "" {
		t.Fatalf("invalid skill should not produce an index entry: %#v", entry)
	}
	if index.hasHash("missing") {
		t.Fatalf("hasHash should return false for missing hash")
	}

	source := filepath.Join(t.TempDir(), "source")
	writeLocalCoverageSkill(t, source, "plan/review", "Plan review", "body")
	if err := ws.AddSkill(ctx, source); err != nil {
		t.Fatalf("AddSkill returned error: %v", err)
	}
	if err := ws.AddSkill(ctx, source); err != nil {
		t.Fatalf("AddSkill duplicate hash should be a no-op: %v", err)
	}
	if err := ws.RemoveSkill(ctx, "missing"); err != nil {
		t.Fatalf("RemoveSkill missing returned error: %v", err)
	}

	if sanitizeDirName("  ///  ") != "skill" || sanitizeDirName("plan/review") != "plan_review" {
		t.Fatalf("sanitizeDirName mismatch")
	}
	if got := uniqueSkillNameFromSet(map[string]struct{}{"review": {}, "review (1)": {}}, "review"); got != "review (2)" {
		t.Fatalf("uniqueSkillNameFromSet = %q", got)
	}
	if cloneStringMap(nil) != nil {
		t.Fatalf("cloneStringMap(nil) should return nil")
	}
	cloned := cloneStringMap(map[string]string{"A": "B"})
	cloned["A"] = "C"
	if cloned["A"] != "C" {
		t.Fatalf("test clone mutation sanity check failed")
	}
	name := uniqueFilePath(workdir, "artifact.txt")
	if err := os.WriteFile(name, []byte("one"), 0o600); err != nil {
		t.Fatalf("WriteFile unique name returned error: %v", err)
	}
	if got := uniqueFilePath(workdir, "artifact.txt"); !strings.Contains(filepath.Base(got), "(1)") {
		t.Fatalf("uniqueFilePath duplicate = %q", got)
	}
	if insideDir(workdir, filepath.Join(filepath.Dir(workdir), "outside.txt")) {
		t.Fatalf("insideDir should reject sibling paths")
	}
	if !insideDir(workdir, filepath.Join(workdir, "inside.txt")) {
		t.Fatalf("insideDir should accept children")
	}
	sourceFile := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(sourceFile, []byte("copy"), 0o600); err != nil {
		t.Fatalf("WriteFile source returned error: %v", err)
	}
	destinationFile := filepath.Join(t.TempDir(), "destination.txt")
	if err := copyFile(sourceFile, destinationFile); err != nil {
		t.Fatalf("copyFile returned error: %v", err)
	}
	if err := copyFile(sourceFile, destinationFile); err == nil {
		t.Fatalf("copyFile should fail when destination already exists")
	}
	linkSource := filepath.Join(t.TempDir(), "linked-source")
	if err := os.Symlink(sourceFile, linkSource); err != nil {
		t.Fatalf("Symlink returned error: %v", err)
	}
	if err := copyDir(filepath.Dir(linkSource), filepath.Join(t.TempDir(), "linked-out")); err == nil {
		t.Fatalf("copyDir should reject symlink entries")
	}
	if err := copyDir(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatalf("copyDir should fail for missing source")
	}
}

func TestDefaultMCPFactoryBranches(t *testing.T) {
	t.Parallel()

	if _, err := defaultMCPFactory(asworkspace.MCPClientConfig{Name: "bad", Type: asworkspace.MCPClientTypeStdio}); err == nil || !strings.Contains(err.Error(), "missing config") {
		t.Fatalf("defaultMCPFactory missing stdio error = %v", err)
	}
	if _, err := defaultMCPFactory(asworkspace.MCPClientConfig{Name: "bad", Type: asworkspace.MCPClientTypeHTTP}); err == nil || !strings.Contains(err.Error(), "missing config") {
		t.Fatalf("defaultMCPFactory missing HTTP error = %v", err)
	}
	if _, err := defaultMCPFactory(asworkspace.MCPClientConfig{Name: "bad", Type: "unsupported"}); err == nil || !strings.Contains(err.Error(), "unsupported MCP type") {
		t.Fatalf("defaultMCPFactory unsupported error = %v", err)
	}
	stdio, err := defaultMCPFactory(asworkspace.MCPClientConfig{
		Name:             "stdio",
		Type:             asworkspace.MCPClientTypeStdio,
		Stateful:         true,
		EnabledTools:     []string{"one"},
		DisabledTools:    []string{"two"},
		ExecutionTimeout: time.Second,
		Stdio: &asworkspace.MCPStdioConfig{
			Command: "server",
			Args:    []string{"--arg"},
			Env:     map[string]string{"TOKEN": "secret"},
		},
	})
	if err != nil {
		t.Fatalf("defaultMCPFactory stdio returned error: %v", err)
	}
	if stdio.Name() != "stdio" || !stdio.IsStateful() {
		t.Fatalf("stdio MCP mismatch: %T %q", stdio, stdio.Name())
	}
	httpClient, err := defaultMCPFactory(asworkspace.MCPClientConfig{
		Name:             "http",
		Type:             asworkspace.MCPClientTypeHTTP,
		HTTP:             &asworkspace.MCPHTTPConfig{URL: "http://localhost/mcp", Headers: map[string]string{"X": "Y"}, ContinuousListening: true},
		ExecutionTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("defaultMCPFactory HTTP returned error: %v", err)
	}
	if httpClient.Name() != "http" {
		t.Fatalf("HTTP MCP name = %q", httpClient.Name())
	}
}

func writeLocalCoverageSkill(t *testing.T, dir, name, description, body string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll skill returned error: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile skill returned error: %v", err)
	}
}

type testLocalMCP struct {
	name          string
	stateful      bool
	connected     bool
	connectCalled bool
	closeCalled   bool
	connectErr    error
	config        asworkspace.MCPClientConfig
}

func (m *testLocalMCP) Name() string { return m.name }

func (m *testLocalMCP) IsStateful() bool { return m.stateful }

func (m *testLocalMCP) IsConnected() bool { return m.connected }

func (m *testLocalMCP) Connect(context.Context) error {
	m.connectCalled = true
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *testLocalMCP) Close() error {
	m.closeCalled = true
	m.connected = false
	return nil
}

func (m *testLocalMCP) ListTools(context.Context) ([]tool.Tool, error) {
	return nil, nil
}

func (m *testLocalMCP) MCPClientConfig() (asworkspace.MCPClientConfig, error) {
	if m.config.Name == "" {
		return asworkspace.MCPClientConfig{Name: m.name, Type: asworkspace.MCPClientTypeHTTP, HTTP: &asworkspace.MCPHTTPConfig{URL: "http://localhost/" + m.name}}, nil
	}
	return m.config, nil
}

type nonPersistableLocalMCP struct {
	name string
}

func (m nonPersistableLocalMCP) Name() string { return m.name }

func (nonPersistableLocalMCP) IsStateful() bool { return false }

func (nonPersistableLocalMCP) IsConnected() bool { return false }

func (nonPersistableLocalMCP) Connect(context.Context) error { return nil }

func (nonPersistableLocalMCP) Close() error { return nil }

func (nonPersistableLocalMCP) ListTools(context.Context) ([]tool.Tool, error) {
	return nil, nil
}
