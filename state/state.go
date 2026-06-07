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

// SummaryContent represents a compressed context summary as text or blocks.
type SummaryContent struct {
	Text   string                   `json:"text,omitempty"`
	Blocks message.ContentBlockList `json:"blocks,omitempty"`
}

// Clone returns a deep copy of the summary content.
func (s SummaryContent) Clone() SummaryContent {
	return SummaryContent{Text: s.Text, Blocks: s.Blocks.Clone()}
}

// ReadCacheEntry stores file-read cache data used by Read/Edit/Write tools.
type ReadCacheEntry struct {
	Lines     []string `json:"lines"`
	UpdatedAt int64    `json:"updated_at"`
	Bytes     float64  `json:"bytes"`
	FilePath  string   `json:"file_path"`
}

// Clone returns a deep copy of the file cache entry.
func (e ReadCacheEntry) Clone() ReadCacheEntry {
	cp := e
	cp.Lines = append([]string(nil), e.Lines...)
	return cp
}

// ToolContext stores tool execution caches and active groups.
type ToolContext struct {
	MaxCacheFiles   int              `json:"max_cache_files"`
	MaxCacheBytes   float64          `json:"max_cache_bytes"`
	ReadFileCache   []ReadCacheEntry `json:"read_file_cache"`
	ActivatedGroups []string         `json:"activated_groups"`
}

// NewToolContext creates a tool context with default limits.
func NewToolContext() *ToolContext {
	return &ToolContext{
		MaxCacheFiles:   100,
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
		c.removeCache(filePath)
		return nil, false
	}
	for index := range c.ReadFileCache {
		entry := &c.ReadFileCache[index]
		if entry.FilePath != filePath {
			continue
		}
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
	for len(c.ReadFileCache) >= c.MaxCacheFiles {
		c.ReadFileCache = c.ReadFileCache[1:]
	}
	currentBytes := c.cacheBytes()
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
	SessionID         string              `json:"session_id"`
	Summary           SummaryContent      `json:"summary"`
	Context           []*message.Message  `json:"context"`
	ReplyID           string              `json:"reply_id"`
	CurIter           int                 `json:"cur_iter"`
	PermissionContext *permission.Context `json:"permission_context"`
	ToolContext       *ToolContext        `json:"tool_context"`
	TaskContext       *TaskContext        `json:"tasks_context"`
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
