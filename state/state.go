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

package state

import (
	"os"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// SummaryContent When the current conversation history becomes too long and approaches the context
// window limit, the Agent's contextStrategy compresses messages and generates summary digests.
// Compression workflow:
//
//	Messages in the context are compressed and summarized
//		→ written to the summary field
//		→ the context retains only recent messages.
//	On the next construction, the summary is injected into the system prompt or prepended to the context.
type SummaryContent struct {
	// Plain-text summary
	Text string `json:"text,omitempty"`
	// A structured list of content blocks that preserves
	// multimodal messages, such as text and images.
	Blocks message.ContentBlockList `json:"blocks,omitempty"`
}

func (s SummaryContent) Clone() SummaryContent {
	return SummaryContent{Text: s.Text, Blocks: s.Blocks.Clone()}
}

// ReadCacheEntry a simple file cache implementation that
// serves file editing tools such as Read and Edit.
type ReadCacheEntry struct {
	// Lines of the file content; Edit/Read tools operate directly on line numbers.
	Lines []string `json:"lines"`
	// Timestamp of the file's last modification, used to determine whether the current cache is stale.
	UpdatedAt int64 `json:"updated_at"`
	// Size of the file content in bytes.
	Bytes float64 `json:"bytes"`
	// File path, used as the cache key.
	FilePath string `json:"file_path"`
}

func (e ReadCacheEntry) Clone() ReadCacheEntry {
	cp := e
	cp.Lines = append([]string(nil), e.Lines...)
	return cp
}

// ToolContext stores runtime caches and group state during tool execution.
type ToolContext struct {
	// Maximum number of files to cache.
	MaxCacheFiles int `json:"max_cache_files"`
	// Total cache size limit; entries are evicted when exceeded.
	MaxCacheBytes float64 `json:"max_cache_bytes"`
	// Cached file list; new files are appended.
	ReadFileCache []ReadCacheEntry `json:"read_file_cache"`
	// List of activated tool groups.
	ActivatedGroups []string `json:"activated_groups"`
}

// NewToolContext creates a tool context with default limits.
func NewToolContext() *ToolContext {
	return &ToolContext{
		MaxCacheFiles: 100,
		// 25000KB，25MB
		MaxCacheBytes:   25000,
		ReadFileCache:   []ReadCacheEntry{},
		ActivatedGroups: []string{},
	}
}

// GetCache returns a cache entry when the file mtime has not changed.
func (c *ToolContext) GetCache(filePath string) (*ReadCacheEntry, bool) {
	if c == nil {
		return nil, false
	}

	info, err := os.Stat(filePath)
	if err != nil {
		// cache is invalid, remove it.
		c.removeCache(filePath)
		return nil, false
	}

	for index := range c.ReadFileCache {
		entry := &c.ReadFileCache[index]
		if entry.FilePath != filePath {
			continue
		}

		// Current already modify for outer, so remove file cache.
		if entry.UpdatedAt != info.ModTime().UnixNano() {
			c.removeCache(filePath)
			return nil, false
		}
		return entry, true
	}

	return nil, false
}

// CacheFile caches file content and evicts old entries by LRU limits.
func (c *ToolContext) CacheFile(filePath string, lines []string) error {
	if c == nil {
		return nil
	}

	if c.MaxCacheFiles <= 0 {
		c.MaxCacheFiles = 100
	}

	if c.MaxCacheBytes <= 0 {
		c.MaxCacheBytes = 25000
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	c.removeCache(filePath)
	newBytes := cacheLinesBytes(lines)

	// FIFO (First-In-First-Out) file cache eviction.
	// if the file count exceeds the limit, drop the oldest entry.
	for len(c.ReadFileCache) >= c.MaxCacheFiles {
		c.ReadFileCache = c.ReadFileCache[1:]
	}

	currentBytes := c.cacheBytes()
	// Byte limit exceeded; apply the same eviction strategy.
	for len(c.ReadFileCache) > 0 && currentBytes+newBytes > c.MaxCacheBytes {
		currentBytes -= c.ReadFileCache[0].Bytes
		c.ReadFileCache = c.ReadFileCache[1:]
	}

	c.ReadFileCache = append(c.ReadFileCache, ReadCacheEntry{
		Lines:     append([]string(nil), lines...),
		UpdatedAt: info.ModTime().UnixNano(),
		Bytes:     newBytes,
		FilePath:  filePath,
	})

	return nil
}

// CleanFileCache drops read caches whose paths are not in reservedFilePaths.
// Passing no reserved paths clears the read cache.
func (c *ToolContext) CleanFileCache(reservedFilePaths ...string) {
	if c == nil {
		return
	}

	reserved := make(map[string]struct{}, len(reservedFilePaths))
	for _, path := range reservedFilePaths {
		if path != "" {
			reserved[path] = struct{}{}
		}
	}

	filtered := c.ReadFileCache[:0]
	for _, entry := range c.ReadFileCache {
		if _, ok := reserved[entry.FilePath]; ok {
			filtered = append(filtered, entry)
		}
	}

	c.ReadFileCache = filtered
}

// Clone returns a deep copy of the tool context.
func (c *ToolContext) Clone() *ToolContext {
	if c == nil {
		return nil
	}
	cp := *c
	cp.ReadFileCache = make([]ReadCacheEntry, 0, len(c.ReadFileCache))
	for _, entry := range c.ReadFileCache {
		cp.ReadFileCache = append(cp.ReadFileCache, entry.Clone())
	}
	cp.ActivatedGroups = append([]string(nil), c.ActivatedGroups...)
	return &cp
}

// AgentState is the Agent runtime state that can be persisted and restored.
type AgentState struct {
	SessionID string `json:"session_id"`
	// Compressed summary of conversation messages.
	Summary SummaryContent `json:"summary"`
	// Context message list; the Reasoning component builds the prompt from this.
	Context []*message.Message `json:"context"`
	ReplyID string             `json:"reply_id"`
	// Current agent iteration count, used to prevent infinite loops.
	CurIter int `json:"cur_iter"`
	// Permission rules; used to build the permission engine for pre-execution checks before tool invocation.
	PermissionContext *permission.Context `json:"permission_context"`
	// Tool context: file caches and activated tool groups.
	ToolContext *ToolContext `json:"tool_context"`
	// Task list tracked by the agent.
	TaskContext *TaskContext `json:"tasks_context"`
}

// NewAgentState creates a fully initialized Agent state.
func NewAgentState() *AgentState {
	return &AgentState{
		SessionID:         utils.NewID(),
		Context:           []*message.Message{},
		ReplyID:           utils.NewID(),
		PermissionContext: permission.NewContext(permission.ModeDefault),
		ToolContext:       NewToolContext(),
		TaskContext:       NewTaskContext(),
	}
}

// Clone returns a deep copy of the Agent state.
func (s *AgentState) Clone() *AgentState {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Summary = s.Summary.Clone()
	cp.Context = make([]*message.Message, 0, len(s.Context))
	for _, msg := range s.Context {
		if msg == nil {
			cp.Context = append(cp.Context, nil)
			continue
		}
		cp.Context = append(cp.Context, msg.Clone())
	}
	cp.PermissionContext = clonePermissionContext(s.PermissionContext)
	cp.ToolContext = s.ToolContext.Clone()
	cp.TaskContext = s.TaskContext.Clone()
	return &cp
}

func (c *ToolContext) removeCache(filePath string) {
	filtered := c.ReadFileCache[:0]
	for _, entry := range c.ReadFileCache {
		if entry.FilePath != filePath {
			filtered = append(filtered, entry)
		}
	}
	c.ReadFileCache = filtered
}

func (c *ToolContext) cacheBytes() float64 {
	var total float64
	for _, entry := range c.ReadFileCache {
		total += entry.Bytes
	}
	return total
}

func cacheLinesBytes(lines []string) float64 {
	var total int
	for _, line := range lines {
		total += len([]byte(line))
	}
	return float64(total) / 1024
}

func clonePermissionContext(ctx *permission.Context) *permission.Context {
	if ctx == nil {
		return nil
	}
	cp := &permission.Context{
		Mode:               ctx.Mode,
		WorkingDirectories: make(map[string]permission.AdditionalWorkingDirectory, len(ctx.WorkingDirectories)),
		AllowRules:         cloneRuleMap(ctx.AllowRules),
		DenyRules:          cloneRuleMap(ctx.DenyRules),
		AskRules:           cloneRuleMap(ctx.AskRules),
	}
	for key, value := range ctx.WorkingDirectories {
		cp.WorkingDirectories[key] = value
	}
	return cp
}

func cloneRuleMap(in map[string][]permission.Rule) map[string][]permission.Rule {
	if in == nil {
		return nil
	}
	out := make(map[string][]permission.Rule, len(in))
	for key, rules := range in {
		out[key] = append([]permission.Rule(nil), rules...)
	}
	return out
}
