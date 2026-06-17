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

package middleware_test

import (
	"context"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/middleware"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func TestLongTermMemoryMiddlewareInjectsAndWritesBack(t *testing.T) {
	t.Parallel()

	store := &recordingMemoryStore{
		records: []middleware.MemoryRecord{{Text: "Ada prefers jasmine tea.", Score: 0.9}},
	}
	mw := middleware.NewLongTermMemoryMiddleware(
		"alice",
		store,
		middleware.WithMemoryAgentID("Friday"),
		middleware.WithMemoryTopK(3),
		middleware.WithMemoryMode(middleware.MemoryModeStaticControl),
	)
	state := statepkg.NewAgentState()
	state.SessionID = "session-1"
	state.ReplyID = "reply-1"
	assistant, err := message.NewAssistantMessage("Friday", message.ContentBlockList{
		message.NewTextBlock("Serve jasmine tea."),
	}, message.WithMessageID("reply-1"))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	agent := fakeAgent{name: "Friday", state: state}

	events, err := mw.OnReply(
		context.Background(),
		agent,
		agentpkg.HookInput{"input": "What should I serve Ada?"},
		func(context.Context) (<-chan message.Event, error) {
			out := make(chan message.Event, 2)
			out <- message.NewReplyStartEvent("session-1", "reply-1", "Friday")
			out <- message.NewReplyEndEvent("session-1", "reply-1")
			close(out)
			return out, nil
		},
	)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}

	if len(store.searches) != 1 {
		t.Fatalf("expected one memory search, got %#v", store.searches)
	}
	search := store.searches[0]
	if search.UserID != "alice" || search.AgentID != "Friday" || search.Query != "What should I serve Ada?" || search.TopK != 3 {
		t.Fatalf("memory search mismatch: %#v", search)
	}
	hints := assistant.Content.GetContentBlocks("hint")
	if len(hints) != 1 {
		t.Fatalf("expected one memory hint, got %#v", assistant.Content)
	}
	hint := hints[0].(*message.HintBlock)
	if hint.Source == nil || *hint.Source != "long-term-memory" || !strings.Contains(hint.Hint, "Ada prefers jasmine tea.") {
		t.Fatalf("memory hint mismatch: %#v", hint)
	}
	if len(store.adds) != 1 {
		t.Fatalf("expected one memory write, got %#v", store.adds)
	}
	add := store.adds[0]
	if add.UserID != "alice" || add.AgentID != "Friday" || add.Input != "What should I serve Ada?" || add.Output != "Serve jasmine tea." {
		t.Fatalf("memory write mismatch: %#v", add)
	}
}

func TestLongTermMemoryMiddlewareExposesAgentControlTools(t *testing.T) {
	t.Parallel()

	store := &recordingMemoryStore{
		records: []middleware.MemoryRecord{{Text: "Ada works in compiler tooling."}},
	}
	mw := middleware.NewLongTermMemoryMiddleware(
		"alice",
		store,
		middleware.WithMemoryMode(middleware.MemoryModeAgentControl),
	)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	prompt, err := mw.OnSystemPrompt(context.Background(), agent, "base prompt")
	if err != nil {
		t.Fatalf("OnSystemPrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "search_memory") || !strings.Contains(prompt, "add_memory") {
		t.Fatalf("memory tool instructions were not appended: %q", prompt)
	}

	tools, err := mw.ListTools(context.Background(), agent)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 2 || tools[0].Name() != "search_memory" || tools[1].Name() != "add_memory" {
		t.Fatalf("unexpected memory tools: %#v", tools)
	}
	if !tools[0].IsReadOnly() || tools[1].IsReadOnly() {
		t.Fatalf("memory tool read-only metadata mismatch")
	}

	searchResp := runMemoryTool(t, tools[0], map[string]any{"query": "Ada", "top_k": 1}, agent.AgentState())
	if text := searchResp.GetTextContent(""); text == nil || !strings.Contains(*text, "compiler tooling") {
		t.Fatalf("search memory response mismatch: %#v", searchResp)
	}
	addResp := runMemoryTool(t, tools[1], map[string]any{"memory": "Ada prefers concise status updates."}, agent.AgentState())
	if addResp.State != message.ToolResultSuccess {
		t.Fatalf("add memory response should succeed, got %#v", addResp)
	}
	if len(store.adds) != 1 || store.adds[0].Input != "Ada prefers concise status updates." {
		t.Fatalf("add_memory did not write memory: %#v", store.adds)
	}
}

func TestLongTermMemoryMiddlewareFallbackBranches(t *testing.T) {
	t.Parallel()

	if middleware.NewLongTermMemoryMiddleware("alice", nil).MiddlewareName() != "long-term-memory" {
		t.Fatal("memory middleware name mismatch")
	}
	state := statepkg.NewAgentState()
	state.SessionID = "session-fallback"
	state.ReplyID = "reply-fallback"
	agent := fakeAgent{name: "Friday", state: state}

	noStore := middleware.NewLongTermMemoryMiddleware("alice", nil)
	events, err := noStore.OnReply(context.Background(), agent, agentpkg.HookInput{}, emptyMemoryEventStream)
	if err != nil || events == nil {
		t.Fatalf("nil-store OnReply should delegate, events=%v err=%v", events, err)
	}
	for range events {
	}
	if prompt, err := noStore.OnSystemPrompt(context.Background(), agent, "base"); err != nil || !strings.Contains(prompt, "search_memory") {
		t.Fatalf("both mode should append memory instructions even without store, prompt=%q err=%v", prompt, err)
	}
	if tools, err := noStore.ListTools(context.Background(), agent); err != nil || tools != nil {
		t.Fatalf("nil-store ListTools should return nil, tools=%#v err=%v", tools, err)
	}

	staticOnly := middleware.NewLongTermMemoryMiddleware("alice", &recordingMemoryStore{}, middleware.WithMemoryMode(middleware.MemoryModeStaticControl))
	if prompt, err := staticOnly.OnSystemPrompt(context.Background(), agent, "base"); err != nil || prompt != "base" {
		t.Fatalf("static mode should not edit system prompt, prompt=%q err=%v", prompt, err)
	}
	if tools, err := staticOnly.ListTools(context.Background(), agent); err != nil || tools != nil {
		t.Fatalf("static mode should not expose tools, tools=%#v err=%v", tools, err)
	}
}

func TestLongTermMemoryToolsParseInputsAndErrors(t *testing.T) {
	t.Parallel()

	store := &recordingMemoryStore{}
	mw := middleware.NewLongTermMemoryMiddleware("alice", store, middleware.WithMemoryMode(middleware.MemoryModeAgentControl))
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}
	tools, err := mw.ListTools(context.Background(), agent)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}

	searchResp := runMemoryTool(t, tools[0], map[string]any{
		"query": message.ContentBlockList{message.NewTextBlock("Ada")},
		"top_k": "2",
	}, agent.AgentState())
	if text := searchResp.GetTextContent(""); text == nil || !strings.Contains(*text, "No relevant memories") {
		t.Fatalf("empty memory search response mismatch: %#v", searchResp)
	}
	if len(store.searches) != 1 || store.searches[0].TopK != 2 || store.searches[0].Query != "Ada" {
		t.Fatalf("search input parsing mismatch: %#v", store.searches)
	}

	addResp := runMemoryTool(t, tools[1], map[string]any{"memory": ""}, agent.AgentState())
	if addResp.State != message.ToolResultError {
		t.Fatalf("empty add_memory should return an error chunk, got %#v", addResp)
	}
}

func runMemoryTool(t *testing.T, current agentpkg.Tool, input map[string]any, state *statepkg.AgentState) *tool.ToolResponse {
	t.Helper()
	chunks, err := current.Execute(context.Background(), input, state)
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

func emptyMemoryEventStream(context.Context) (<-chan message.Event, error) {
	out := make(chan message.Event)
	close(out)
	return out, nil
}

type recordingMemoryStore struct {
	records  []middleware.MemoryRecord
	searches []middleware.MemoryQuery
	adds     []middleware.MemoryEntry
}

func (s *recordingMemoryStore) Search(_ context.Context, query middleware.MemoryQuery) ([]middleware.MemoryRecord, error) {
	s.searches = append(s.searches, query)
	return append([]middleware.MemoryRecord(nil), s.records...), nil
}

func (s *recordingMemoryStore) Add(_ context.Context, entry middleware.MemoryEntry) error {
	s.adds = append(s.adds, entry)
	return nil
}
