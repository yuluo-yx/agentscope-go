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

package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
)

func TestAgentStateDefaultsAndClone(t *testing.T) {
	t.Parallel()

	msg, err := message.NewUserMessage("Tony", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	state := statepkg.NewAgentState()
	state.Context = append(state.Context, msg)
	state.PermissionContext.AllowRules["Bash"] = []permission.Rule{{ToolName: "Bash", RuleContent: "go test:*", Behavior: permission.BehaviorAllow}}
	state.PermissionContext.AutoDenialState = permission.AutoDenialState{ConsecutiveDenials: 1, TotalDenials: 2}
	state.TaskContext.AddTask(statepkg.NewTask("Translate", "port python docs", map[string]any{"phase": 2}))
	state.ContextStatus = &statepkg.ContextStatus{Level: statepkg.ContextStatusWarning, RemainingTokens: 100}

	cloned := state.Clone()
	cloned.Context[0].Content[0].(*message.TextBlock).Text = "changed"
	cloned.PermissionContext.AllowRules["Bash"][0].RuleContent = "changed"
	cloned.PermissionContext.AutoDenialState.ConsecutiveDenials = 9
	cloned.TaskContext.Tasks[0].Subject = "changed"
	cloned.ContextStatus.Level = statepkg.ContextStatusBlocking

	if state.SessionID == "" || state.ReplyID == "" {
		t.Fatalf("state should create ids: %#v", state)
	}
	if len(state.SessionID) != 32 || strings.Contains(state.SessionID, "-") {
		t.Fatalf("session id should match Python uuid4().hex format: %q", state.SessionID)
	}
	if len(state.ReplyID) != 32 || strings.Contains(state.ReplyID, "-") {
		t.Fatalf("reply id should match Python uuid4().hex format: %q", state.ReplyID)
	}
	if got := state.Context[0].GetTextContent(""); got == nil || *got != "hello" {
		t.Fatalf("message clone mutated original: %#v", got)
	}
	if got := state.PermissionContext.AllowRules["Bash"][0].RuleContent; got != "go test:*" {
		t.Fatalf("permission context clone mutated original: %q", got)
	}
	if got := state.TaskContext.Tasks[0].Subject; got != "Translate" {
		t.Fatalf("task clone mutated original: %q", got)
	}
	if got := state.PermissionContext.AutoDenialState.ConsecutiveDenials; got != 1 {
		t.Fatalf("auto denial state clone mutated original: %d", got)
	}
	if state.ContextStatus.Level != statepkg.ContextStatusWarning {
		t.Fatalf("context status clone mutated original: %#v", state.ContextStatus)
	}
}

func TestTaskIDUsesPythonCompatibleHexUUID(t *testing.T) {
	t.Parallel()

	task := statepkg.NewTask("Track work", "Keep compatibility with Python task IDs.", nil)
	if len(task.ID) != 32 || strings.Contains(task.ID, "-") {
		t.Fatalf("task ID should match Python uuid4().hex format, got %q", task.ID)
	}
	for _, char := range task.ID {
		if !strings.ContainsRune("0123456789abcdef", char) {
			t.Fatalf("task ID should contain only lowercase hex characters, got %q", task.ID)
		}
	}
}

func TestToolContextCachesFilesWithLRUEviction(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.txt")
	secondPath := filepath.Join(dir, "second.txt")
	if err := os.WriteFile(firstPath, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFile first returned error: %v", err)
	}
	if err := os.WriteFile(secondPath, []byte("second"), 0o600); err != nil {
		t.Fatalf("WriteFile second returned error: %v", err)
	}

	toolContext := statepkg.NewToolContext()
	toolContext.MaxCacheFiles = 1
	if err := toolContext.CacheFile(firstPath, []string{"first"}); err != nil {
		t.Fatalf("CacheFile first returned error: %v", err)
	}
	if err := toolContext.CacheFile(secondPath, []string{"second"}); err != nil {
		t.Fatalf("CacheFile second returned error: %v", err)
	}
	if _, ok := toolContext.GetCache(firstPath); ok {
		t.Fatalf("first cache entry should be evicted")
	}
	if entry, ok := toolContext.GetCache(secondPath); !ok || entry.Lines[0] != "second" {
		t.Fatalf("second cache entry should remain, got %#v ok=%v", entry, ok)
	}
}

func TestToolContextCacheInvalidationAndClone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "cache.txt")
	if err := os.WriteFile(filePath, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	toolContext := statepkg.NewToolContext()
	if err := toolContext.CacheFile(filePath, []string{"first"}); err != nil {
		t.Fatalf("CacheFile returned error: %v", err)
	}
	entry, ok := toolContext.GetCache(filePath)
	if !ok {
		t.Fatal("cache entry should be available")
	}
	clonedEntry := entry.Clone()
	clonedEntry.Lines[0] = "changed"
	if entry.Lines[0] != "first" {
		t.Fatalf("cache entry clone mutated original: %#v", entry)
	}

	if err := os.WriteFile(filePath, []byte("second"), 0o600); err != nil {
		t.Fatalf("WriteFile update returned error: %v", err)
	}
	if _, ok := toolContext.GetCache(filePath); ok {
		t.Fatal("modified file should invalidate cache")
	}
	if _, ok := toolContext.GetCache(filepath.Join(dir, "missing.txt")); ok {
		t.Fatal("missing file should not have cache")
	}

	toolContext.ActivatedGroups = append(toolContext.ActivatedGroups, "basic")
	clonedContext := toolContext.Clone()
	clonedContext.ActivatedGroups[0] = "changed"
	if toolContext.ActivatedGroups[0] != "basic" {
		t.Fatalf("tool context clone mutated original: %#v", toolContext)
	}
	if (*statepkg.ToolContext)(nil).Clone() != nil {
		t.Fatal("nil tool context clone should return nil")
	}
}

func TestToolContextNilAndDefaultCacheLimits(t *testing.T) {
	t.Parallel()

	var toolContext *statepkg.ToolContext
	if err := toolContext.CacheFile("missing", nil); err != nil {
		t.Fatalf("nil tool context cache should be ignored: %v", err)
	}
	if _, ok := toolContext.GetCache("missing"); ok {
		t.Fatal("nil tool context should not return cache")
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "cache.txt")
	if err := os.WriteFile(filePath, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	defaulted := &statepkg.ToolContext{}
	if err := defaulted.CacheFile(filePath, []string{"first"}); err != nil {
		t.Fatalf("CacheFile returned error: %v", err)
	}
	if defaulted.MaxCacheFiles != 100 || defaulted.MaxCacheBytes != 25000 {
		t.Fatalf("cache defaults not restored: %#v", defaulted)
	}
	if err := defaulted.CacheFile(filepath.Join(dir, "missing.txt"), nil); err == nil {
		t.Fatal("missing file should return stat error")
	}
}

func TestToolContextEvictsByByteLimit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	firstPath := filepath.Join(dir, "first.txt")
	secondPath := filepath.Join(dir, "second.txt")
	if err := os.WriteFile(firstPath, []byte("first"), 0o600); err != nil {
		t.Fatalf("WriteFile first returned error: %v", err)
	}
	if err := os.WriteFile(secondPath, []byte("second"), 0o600); err != nil {
		t.Fatalf("WriteFile second returned error: %v", err)
	}

	toolContext := statepkg.NewToolContext()
	toolContext.MaxCacheBytes = 0.005
	if err := toolContext.CacheFile(firstPath, []string{"first"}); err != nil {
		t.Fatalf("CacheFile first returned error: %v", err)
	}
	if err := toolContext.CacheFile(secondPath, []string{"second"}); err != nil {
		t.Fatalf("CacheFile second returned error: %v", err)
	}
	if len(toolContext.ReadFileCache) != 1 || toolContext.ReadFileCache[0].FilePath != secondPath {
		t.Fatalf("byte limit should evict older entries: %#v", toolContext.ReadFileCache)
	}
}

func TestToolContextCleanFileCache(t *testing.T) {
	t.Parallel()

	var nilContext *statepkg.ToolContext
	nilContext.CleanFileCache("ignored")

	toolContext := statepkg.NewToolContext()
	toolContext.ReadFileCache = []statepkg.ReadCacheEntry{
		{FilePath: "keep", Lines: []string{"a"}},
		{FilePath: "drop", Lines: []string{"b"}},
		{FilePath: "", Lines: []string{"empty"}},
	}
	toolContext.CleanFileCache("", "keep")
	if len(toolContext.ReadFileCache) != 1 || toolContext.ReadFileCache[0].FilePath != "keep" {
		t.Fatalf("CleanFileCache should keep only reserved non-empty paths: %#v", toolContext.ReadFileCache)
	}

	toolContext.CleanFileCache()
	if len(toolContext.ReadFileCache) != 0 {
		t.Fatalf("CleanFileCache without reservations should clear cache: %#v", toolContext.ReadFileCache)
	}
}

func TestAgentStateNilClone(t *testing.T) {
	t.Parallel()

	if (*statepkg.AgentState)(nil).Clone() != nil {
		t.Fatal("nil agent state clone should return nil")
	}

	state := statepkg.NewAgentState()
	state.PermissionContext = nil
	state.ToolContext = nil
	state.TaskContext = nil
	cloned := state.Clone()
	if cloned.PermissionContext != nil || cloned.ToolContext != nil || cloned.TaskContext != nil {
		t.Fatalf("nil nested contexts should stay nil on clone: %#v", cloned)
	}
}
