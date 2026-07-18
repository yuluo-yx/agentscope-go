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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/middleware"
	"github.com/yuluo-yx/agentscope-go/pkg/model"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
)

type fakeStructuredModel struct {
	mu       sync.Mutex
	requests []model.StructuredOutputRequest
	content  map[string]any
	err      error
}

func (m *fakeStructuredModel) Name() string { return "fake-structured" }

func (m *fakeStructuredModel) Call(context.Context, model.CallRequest) (*model.ChatResponse, error) {
	return nil, nil
}

func (m *fakeStructuredModel) Stream(context.Context, model.CallRequest) (<-chan model.ChatResponse, error) {
	out := make(chan model.ChatResponse)
	close(out)
	return out, nil
}

func (m *fakeStructuredModel) CountTokens(model.CallRequest) (int, error) { return 0, nil }

func (m *fakeStructuredModel) GenerateStructured(
	_ context.Context,
	request model.StructuredOutputRequest,
) (*model.StructuredResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, request)
	if m.err != nil {
		return nil, m.err
	}
	return &model.StructuredResponse{Content: m.content, Type: model.StructuredResponseType}, nil
}

func (m *fakeStructuredModel) requestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

func (m *fakeStructuredModel) firstRequest() model.StructuredOutputRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests[0]
}

func writeMemoryFile(t *testing.T, workdir, name, content string) time.Time {
	t.Helper()
	path := filepath.Join(workdir, "Memory", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	mtime := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes returned error: %v", err)
	}
	return mtime
}

func emptyHandler(context.Context) (<-chan message.Event, error) {
	out := make(chan message.Event)
	close(out)
	return out, nil
}

func TestAgenticMemoryOnSystemPromptCreatesLayoutAndInjectsSnapshot(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	writeMemoryFile(t, workdir, "user_role.md",
		"---\nname: User role\ndescription: When you are about to discuss tea, Read it first\ntype: user\n---\n\nUser likes jasmine tea.\n")
	mw := middleware.NewAgenticMemoryMiddleware(workdir)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	prompt, err := mw.OnSystemPrompt(context.Background(), agent, "Base prompt.")
	if err != nil {
		t.Fatalf("OnSystemPrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "Base prompt.") {
		t.Fatalf("base prompt was not preserved: %q", prompt)
	}
	if !strings.Contains(prompt, "# Auto Memory") {
		t.Fatalf("memory instructions missing: %q", prompt)
	}
	if !strings.Contains(prompt, filepath.Join(workdir, "Memory")) {
		t.Fatalf("memory dir was not substituted into instructions: %q", prompt)
	}
	if !strings.Contains(prompt, "## MEMORY.md") {
		t.Fatalf("MEMORY.md section missing: %q", prompt)
	}
	info, err := os.Stat(filepath.Join(workdir, "Memory", "MEMORY.md"))
	if err != nil {
		t.Fatalf("MEMORY.md was not created: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("MEMORY.md permissions = %o, want 600", got)
	}
}

func TestAgenticMemoryOnSystemPromptEmptyIndexNotice(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	mw := middleware.NewAgenticMemoryMiddleware(workdir)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	prompt, err := mw.OnSystemPrompt(context.Background(), agent, "")
	if err != nil {
		t.Fatalf("OnSystemPrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "Your MEMORY.md is currently empty.") {
		t.Fatalf("empty-index notice missing: %q", prompt)
	}
}

func TestAgenticMemoryOnSystemPromptTruncatesLongIndex(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	lines := make([]string, 0, 100)
	for index := 0; index < 100; index++ {
		lines = append(lines, "- [Entry](entry.md) — a one-line index hook for the entry")
	}
	writeMemoryFile(t, workdir, "MEMORY.md", strings.Join(lines, "\n"))
	mw := middleware.NewAgenticMemoryMiddleware(
		workdir,
		middleware.WithAgenticMemoryMaxTokens(120),
	)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	prompt, err := mw.OnSystemPrompt(context.Background(), agent, "")
	if err != nil {
		t.Fatalf("OnSystemPrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "<<<TRUNCATED>>>") {
		t.Fatalf("truncation marker missing: %q", prompt)
	}
	if !strings.Contains(prompt, "Use the `Read` tool with offset") {
		t.Fatalf("truncation reminder missing: %q", prompt)
	}
	truncatedSection, _, _ := strings.Cut(prompt, "<<<TRUNCATED>>>")
	if got := strings.Count(truncatedSection, "- [Entry](entry.md)"); got == len(lines) {
		t.Fatalf("index was not truncated, all %d entries present", got)
	}
}

func TestAgenticMemoryReasoningInjectsSelectedMemoryHint(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	writeMemoryFile(t, workdir, "user_role.md",
		"---\nname: User role\ndescription: When you are about to discuss tea, Read it first\ntype: user\n---\n\nUser likes jasmine tea.\n")
	writeMemoryFile(t, workdir, "feedback_style.md",
		"---\nname: Style\ndescription: When you are about to respond, Read it first\ntype: feedback\n---\n\nBe terse.\n")
	writeMemoryFile(t, workdir, "MEMORY.md", "- [User role](user_role.md) — tea\n- [Style](feedback_style.md) — terse\n")

	retrievalModel := &fakeStructuredModel{
		content: map[string]any{"selected_files": []any{"user_role.md"}},
	}
	mw := middleware.NewAgenticMemoryMiddleware(
		workdir,
		middleware.WithAgenticRetrievalModel(retrievalModel),
	)

	state := statepkg.NewAgentState()
	assistant, err := message.NewAssistantMessage("Friday", nil)
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	agent := fakeAgent{name: "Friday", state: state}

	userMsg, err := message.NewUserMessage("alice", "What tea should I serve?")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	ctx := context.Background()
	events, err := mw.OnReply(ctx, agent, agentpkg.HookInput{"input": []*message.Message{userMsg}}, emptyHandler)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}

	// The retrieval task is consumed by OnReasoning. Poll until it completes.
	deadline := time.Now().Add(2 * time.Second)
	for {
		events, err = mw.OnReasoning(ctx, agent, agentpkg.HookInput{}, emptyHandler)
		if err != nil {
			t.Fatalf("OnReasoning returned error: %v", err)
		}
		for range events {
		}
		if len(assistant.GetContentBlocks("hint")) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retrieval hint was not injected before deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}

	hints := assistant.GetContentBlocks("hint")
	if len(hints) != 1 {
		t.Fatalf("expected exactly one hint block, got %d", len(hints))
	}
	hint, ok := hints[0].(*message.HintBlock)
	if !ok {
		t.Fatalf("unexpected hint block type: %#v", hints[0])
	}
	hintText := hint.Hint
	if !strings.Contains(hintText, "User likes jasmine tea.") {
		t.Fatalf("selected memory content missing from hint: %q", hintText)
	}
	if !strings.Contains(hintText, "saved 2 days ago") {
		t.Fatalf("memory age header missing from hint: %q", hintText)
	}
	if strings.Contains(hintText, "Be terse.") {
		t.Fatalf("unselected memory leaked into hint: %q", hintText)
	}
	if hint.Source == nil || *hint.Source != "agentic-memory" {
		t.Fatalf("hint source mismatch: %#v", hint.Source)
	}

	if retrievalModel.requestCount() != 1 {
		t.Fatalf("expected one structured retrieval call, got %d", retrievalModel.requestCount())
	}
	request := retrievalModel.firstRequest()
	if len(request.Messages) != 2 || request.Messages[0].Role != message.RoleSystem || request.Messages[1].Role != message.RoleUser {
		t.Fatalf("retrieval request messages mismatch: %#v", request.Messages)
	}
	userText := request.Messages[1].GetTextContent()
	if userText == nil || !strings.Contains(*userText, "Query: alice: What tea should I serve?") {
		t.Fatalf("retrieval query mismatch: %#v", userText)
	}
	if !strings.Contains(*userText, "[user] user_role.md") {
		t.Fatalf("manifest missing type tag: %q", *userText)
	}
}

func TestAgenticMemoryFiltersHallucinatedFilenames(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	writeMemoryFile(t, workdir, "user_role.md",
		"---\ndescription: tea trigger\ntype: user\n---\n\nUser likes jasmine tea.\n")
	retrievalModel := &fakeStructuredModel{
		content: map[string]any{"selected_files": []any{"does_not_exist.md"}},
	}
	mw := middleware.NewAgenticMemoryMiddleware(
		workdir,
		middleware.WithAgenticRetrievalModel(retrievalModel),
	)

	state := statepkg.NewAgentState()
	assistant, err := message.NewAssistantMessage("Friday", nil)
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	agent := fakeAgent{name: "Friday", state: state}

	ctx := context.Background()
	events, err := mw.OnReply(ctx, agent, agentpkg.HookInput{"input": "tea?"}, emptyHandler)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		events, err = mw.OnReasoning(ctx, agent, agentpkg.HookInput{}, emptyHandler)
		if err != nil {
			t.Fatalf("OnReasoning returned error: %v", err)
		}
		for range events {
		}
		if retrievalModel.requestCount() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retrieval call was not observed before deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Give the poll a moment to consume the completed task.
	time.Sleep(20 * time.Millisecond)
	if _, err := mw.OnReasoning(ctx, agent, agentpkg.HookInput{}, emptyHandler); err != nil {
		t.Fatalf("OnReasoning returned error: %v", err)
	}
	if hints := assistant.GetContentBlocks("hint"); len(hints) != 0 {
		t.Fatalf("hallucinated filenames must not produce hints: %#v", hints)
	}
}

func TestAgenticMemorySkipsRetrievalWithoutTopicFiles(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	retrievalModel := &fakeStructuredModel{
		content: map[string]any{"selected_files": []any{"user_role.md"}},
	}
	mw := middleware.NewAgenticMemoryMiddleware(
		workdir,
		middleware.WithAgenticRetrievalModel(retrievalModel),
	)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	ctx := context.Background()
	events, err := mw.OnReply(ctx, agent, agentpkg.HookInput{"input": "hello"}, emptyHandler)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if retrievalModel.requestCount() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if retrievalModel.requestCount() != 0 {
		t.Fatalf("retrieval must not run when only MEMORY.md exists, got %d calls", retrievalModel.requestCount())
	}
}

func TestAgenticMemorySkipsRetrievalWhenAsyncDisabled(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	writeMemoryFile(t, workdir, "user_role.md",
		"---\ndescription: tea trigger\ntype: user\n---\n\nUser likes jasmine tea.\n")
	retrievalModel := &fakeStructuredModel{
		content: map[string]any{"selected_files": []any{"user_role.md"}},
	}
	mw := middleware.NewAgenticMemoryMiddleware(
		workdir,
		middleware.WithAgenticRetrievalModel(retrievalModel),
		middleware.WithAgenticRetrievalAsync(false),
	)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	ctx := context.Background()
	events, err := mw.OnReply(ctx, agent, agentpkg.HookInput{"input": "tea?"}, emptyHandler)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}
	if _, err := mw.OnReasoning(ctx, agent, agentpkg.HookInput{}, emptyHandler); err != nil {
		t.Fatalf("OnReasoning returned error: %v", err)
	}
	if retrievalModel.requestCount() != 0 {
		t.Fatalf("async retrieval disabled but retrieval ran %d times", retrievalModel.requestCount())
	}
}

func TestAgenticMemorySkipsRetrievalWithoutModel(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	writeMemoryFile(t, workdir, "user_role.md",
		"---\ndescription: tea trigger\ntype: user\n---\n\nUser likes jasmine tea.\n")
	mw := middleware.NewAgenticMemoryMiddleware(workdir)

	state := statepkg.NewAgentState()
	assistant, err := message.NewAssistantMessage("Friday", nil)
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	agent := fakeAgent{name: "Friday", state: state}

	ctx := context.Background()
	events, err := mw.OnReply(ctx, agent, agentpkg.HookInput{"input": "tea?"}, emptyHandler)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}
	if _, err := mw.OnReasoning(ctx, agent, agentpkg.HookInput{}, emptyHandler); err != nil {
		t.Fatalf("OnReasoning returned error: %v", err)
	}
	if hints := assistant.GetContentBlocks("hint"); len(hints) != 0 {
		t.Fatalf("retrieval without a model must not produce hints: %#v", hints)
	}
}

func TestAgenticMemoryCustomStoreAndMemoryDir(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := &recordingAgenticMemoryStore{files: map[string][]byte{}}
	mw := middleware.NewAgenticMemoryMiddleware(
		workdir,
		middleware.WithAgenticMemoryDir("Notes"),
		middleware.WithAgenticMemoryStore(store),
	)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	prompt, err := mw.OnSystemPrompt(context.Background(), agent, "")
	if err != nil {
		t.Fatalf("OnSystemPrompt returned error: %v", err)
	}
	indexPath := filepath.Join(workdir, "Notes", "MEMORY.md")
	if _, ok := store.files[indexPath]; !ok {
		t.Fatalf("MEMORY.md was not written through the custom store: %#v", store.files)
	}
	if !strings.Contains(prompt, filepath.Join(workdir, "Notes")) {
		t.Fatalf("custom memory dir missing from instructions: %q", prompt)
	}
}

type recordingAgenticMemoryStore struct {
	files map[string][]byte
}

func (s *recordingAgenticMemoryStore) ReadFile(_ context.Context, path string) ([]byte, error) {
	content, ok := s.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return content, nil
}

func (s *recordingAgenticMemoryStore) WriteFile(_ context.Context, path string, data []byte) error {
	s.files[path] = append([]byte(nil), data...)
	return nil
}

func (s *recordingAgenticMemoryStore) FileExists(_ context.Context, path string) (bool, error) {
	_, ok := s.files[path]
	return ok, nil
}

func (s *recordingAgenticMemoryStore) ListMarkdownFiles(_ context.Context, dir string) ([]string, error) {
	files := []string{}
	for path := range s.files {
		if strings.HasPrefix(path, dir+string(os.PathSeparator)) && strings.HasSuffix(path, ".md") {
			files = append(files, strings.TrimPrefix(path, dir+string(os.PathSeparator)))
		}
	}
	return files, nil
}

func (s *recordingAgenticMemoryStore) StatMTime(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}
