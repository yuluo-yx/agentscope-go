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

package middleware

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

const (
	defaultMemoryTopK          = 5
	memoryHintSource           = "long-term-memory"
	defaultMemorySectionHeader = "## Relevant memories from past conversations"
	defaultMemorySectionIntro  = "The following memories about the user may be relevant. Use them only if they are pertinent to the current request."
	defaultMemoryToolPrompt    = "## Long-term memory\n\nYou have `search_memory` and `add_memory` tools available. Use them when the conversation depends on or contributes durable facts about the user."
)

// MemoryMode controls how an Agent interacts with long-term memory.
type MemoryMode string

const (
	// MemoryModeStaticControl automatically retrieves and writes memories.
	MemoryModeStaticControl MemoryMode = "static_control"
	// MemoryModeAgentControl exposes memory tools for the Agent to call.
	MemoryModeAgentControl MemoryMode = "agent_control"
	// MemoryModeBoth enables both static retrieval/write-back and memory tools.
	MemoryModeBoth MemoryMode = "both"
)

// MemoryRecord is one retrieved long-term memory.
type MemoryRecord struct {
	ID       string         `json:"id,omitempty"`
	Text     string         `json:"text"`
	Score    float64        `json:"score,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MemoryQuery describes a long-term memory search.
type MemoryQuery struct {
	UserID   string         `json:"user_id"`
	AgentID  string         `json:"agent_id,omitempty"`
	Query    string         `json:"query"`
	TopK     int            `json:"top_k,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MemoryEntry describes one memory write-back request.
type MemoryEntry struct {
	UserID    string         `json:"user_id"`
	AgentID   string         `json:"agent_id,omitempty"`
	Input     string         `json:"input,omitempty"`
	Output    string         `json:"output,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	ReplyID   string         `json:"reply_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// MemoryStore is the storage contract used by LongTermMemoryMiddleware.
type MemoryStore interface {
	Search(context.Context, MemoryQuery) ([]MemoryRecord, error)
	Add(context.Context, MemoryEntry) error
}

// LongTermMemoryOption configures LongTermMemoryMiddleware.
type LongTermMemoryOption func(*LongTermMemoryMiddleware)

// LongTermMemoryMiddleware adds static and tool-driven long-term memory.
type LongTermMemoryMiddleware struct {
	userID string
	store  MemoryStore

	agentID          string
	mode             MemoryMode
	topK             int
	sectionHeader    string
	sectionIntro     string
	toolInstructions string
}

// NewLongTermMemoryMiddleware creates long-term memory middleware.
func NewLongTermMemoryMiddleware(userID string, store MemoryStore, opts ...LongTermMemoryOption) *LongTermMemoryMiddleware {
	m := &LongTermMemoryMiddleware{
		userID:           strings.TrimSpace(userID),
		store:            store,
		mode:             MemoryModeBoth,
		topK:             defaultMemoryTopK,
		sectionHeader:    defaultMemorySectionHeader,
		sectionIntro:     defaultMemorySectionIntro,
		toolInstructions: defaultMemoryToolPrompt,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	if m.mode == "" {
		m.mode = MemoryModeBoth
	}
	if m.topK <= 0 {
		m.topK = defaultMemoryTopK
	}
	return m
}

// WithMemoryMode sets the long-term memory control mode.
func WithMemoryMode(mode MemoryMode) LongTermMemoryOption {
	return func(m *LongTermMemoryMiddleware) {
		m.mode = mode
	}
}

// WithMemoryAgentID scopes memory operations to one agent ID.
func WithMemoryAgentID(agentID string) LongTermMemoryOption {
	return func(m *LongTermMemoryMiddleware) {
		m.agentID = strings.TrimSpace(agentID)
	}
}

// WithMemoryTopK sets the default number of memories retrieved.
func WithMemoryTopK(topK int) LongTermMemoryOption {
	return func(m *LongTermMemoryMiddleware) {
		m.topK = topK
	}
}

// MiddlewareName returns the middleware name.
func (*LongTermMemoryMiddleware) MiddlewareName() string {
	return "long-term-memory"
}

// OnReply performs static-control memory retrieval and write-back.
func (m *LongTermMemoryMiddleware) OnReply(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	if m == nil || m.store == nil || !m.staticControlEnabled() {
		return next(ctx)
	}
	queryText := memoryText(input["input"])
	records, err := m.store.Search(ctx, MemoryQuery{
		UserID:  m.userID,
		AgentID: m.effectiveAgentID(agent),
		Query:   queryText,
		TopK:    m.topK,
	})
	if err != nil {
		return nil, err
	}
	events, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return nil, fmt.Errorf("agentscope/middleware: nil event stream")
	}

	out := make(chan message.Event)
	go func() {
		defer close(out)
		for event := range events {
			switch event.(type) {
			case *message.ReplyStartEvent:
				m.injectMemories(agent, records)
			case *message.ReplyEndEvent:
				_ = m.writeBack(ctx, agent, queryText)
			}
			out <- event
		}
	}()
	return out, nil
}

// OnSystemPrompt appends memory tool instructions in agent-control modes.
func (m *LongTermMemoryMiddleware) OnSystemPrompt(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	prompt string,
) (string, error) {
	_ = ctx
	_ = agent
	if m == nil || !m.agentControlEnabled() || strings.TrimSpace(m.toolInstructions) == "" {
		return prompt, nil
	}
	return appendPrompt(prompt, m.toolInstructions), nil
}

// ListTools exposes search_memory and add_memory in agent-control modes.
func (m *LongTermMemoryMiddleware) ListTools(ctx context.Context, agent agentpkg.AgentAccessor) ([]agentpkg.Tool, error) {
	_ = ctx
	if m == nil || m.store == nil || !m.agentControlEnabled() {
		return nil, nil
	}
	searchTool, err := astool.NewFunctionTool(
		"search_memory",
		"Search long-term memories relevant to the current task.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search query."},
				"top_k": map[string]any{"type": "integer", "description": "Maximum number of memories to return."},
			},
			"required": []string{"query"},
		},
		func(ctx context.Context, input map[string]any, _ *statepkg.AgentState) (message.ContentBlockList, error) {
			query := strings.TrimSpace(memoryText(input["query"]))
			records, err := m.store.Search(ctx, MemoryQuery{
				UserID:  m.userID,
				AgentID: m.effectiveAgentID(agent),
				Query:   query,
				TopK:    memoryTopK(input["top_k"], m.topK),
			})
			if err != nil {
				return nil, err
			}
			return message.ContentBlockList{message.NewTextBlock(formatMemoryRecords(records, ""))}, nil
		},
		astool.WithFunctionReadOnly(true),
		astool.WithFunctionStateInjected(true),
	)
	if err != nil {
		return nil, err
	}
	addTool, err := astool.NewFunctionTool(
		"add_memory",
		"Add one durable user memory.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"memory": map[string]any{"type": "string", "description": "Memory text to store."},
			},
			"required": []string{"memory"},
		},
		func(ctx context.Context, input map[string]any, state *statepkg.AgentState) (message.ContentBlockList, error) {
			text := strings.TrimSpace(memoryText(input["memory"]))
			if text == "" {
				return nil, fmt.Errorf("memory: memory text is required")
			}
			entry := MemoryEntry{
				UserID:  m.userID,
				AgentID: m.effectiveAgentID(agent),
				Input:   text,
			}
			if state != nil {
				entry.SessionID = state.SessionID
				entry.ReplyID = state.ReplyID
			}
			if err := m.store.Add(ctx, entry); err != nil {
				return nil, err
			}
			return message.ContentBlockList{message.NewTextBlock("Memory saved.")}, nil
		},
		astool.WithFunctionReadOnly(false),
		astool.WithFunctionStateInjected(true),
	)
	if err != nil {
		return nil, err
	}
	return []agentpkg.Tool{searchTool, addTool}, nil
}

func (m *LongTermMemoryMiddleware) staticControlEnabled() bool {
	return m.mode == MemoryModeStaticControl || m.mode == MemoryModeBoth
}

func (m *LongTermMemoryMiddleware) agentControlEnabled() bool {
	return m.mode == MemoryModeAgentControl || m.mode == MemoryModeBoth
}

func (m *LongTermMemoryMiddleware) effectiveAgentID(agent agentpkg.AgentAccessor) string {
	if m.agentID != "" {
		return m.agentID
	}
	if agent == nil {
		return ""
	}
	return agent.AgentName()
}

func (m *LongTermMemoryMiddleware) injectMemories(agent agentpkg.AgentAccessor, records []MemoryRecord) {
	hint := strings.TrimSpace(formatMemoryRecords(records, m.sectionIntro))
	if hint == "" || hint == "No relevant memories found." {
		return
	}
	blocks := message.ContentBlockList{
		message.NewHintBlock(m.sectionHeader+"\n\n"+hint, message.WithHintSource(memoryHintSource)),
	}
	_ = appendInboxBlocks(agent, blocks)
}

func (m *LongTermMemoryMiddleware) writeBack(ctx context.Context, agent agentpkg.AgentAccessor, input string) error {
	state := agent.AgentState()
	if state == nil {
		return nil
	}
	return m.store.Add(ctx, MemoryEntry{
		UserID:    m.userID,
		AgentID:   m.effectiveAgentID(agent),
		Input:     input,
		Output:    latestAssistantText(state, state.ReplyID),
		SessionID: state.SessionID,
		ReplyID:   state.ReplyID,
	})
}

func formatMemoryRecords(records []MemoryRecord, intro string) string {
	lines := []string{}
	if strings.TrimSpace(intro) != "" {
		lines = append(lines, strings.TrimSpace(intro), "")
	}
	for _, record := range records {
		text := strings.TrimSpace(record.Text)
		if text == "" {
			continue
		}
		lines = append(lines, "- "+text)
	}
	if len(lines) == 0 {
		return "No relevant memories found."
	}
	return strings.Join(lines, "\n")
}

func memoryText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case message.ContentBlockList:
		if text := typed.GetTextContent("\n"); text != nil {
			return strings.TrimSpace(*text)
		}
	case *message.Message:
		if typed != nil {
			return memoryText(typed.Content)
		}
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func memoryTopK(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return typed
		}
	case int64:
		if typed > 0 {
			return int(typed)
		}
	case float64:
		if typed > 0 {
			return int(typed)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	if fallback > 0 {
		return fallback
	}
	return defaultMemoryTopK
}

func latestAssistantText(state *statepkg.AgentState, replyID string) string {
	if state == nil {
		return ""
	}
	for index := len(state.Context) - 1; index >= 0; index-- {
		msg := state.Context[index]
		if msg == nil || msg.Role != message.RoleAssistant {
			continue
		}
		if replyID != "" && msg.ID != replyID {
			continue
		}
		if text := msg.Content.GetTextContent("\n"); text != nil {
			return strings.TrimSpace(*text)
		}
		return ""
	}
	return ""
}

func appendPrompt(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	if base == "" {
		return extra
	}
	if extra == "" {
		return base
	}
	return base + "\n\n" + extra
}

var (
	_ agentpkg.ReplyMiddleware        = (*LongTermMemoryMiddleware)(nil)
	_ agentpkg.SystemPromptMiddleware = (*LongTermMemoryMiddleware)(nil)
	_ agentpkg.ToolMiddleware         = (*LongTermMemoryMiddleware)(nil)
)
