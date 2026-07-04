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
	"reflect"
	"strings"
	"testing"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/embedding"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/middleware"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestRAGMiddlewareExposesAgenticSearchTool(t *testing.T) {
	t.Parallel()

	kb, store, model := newRAGTestKnowledgeBase(t, []rag.VectorSearchResult{
		ragSearchResult("doc-1", 0, 0.91, "PTO policy allows 15 days.", "handbook.md"),
	})
	mw := middleware.NewRAGMiddleware(
		[]*rag.KnowledgeBase{kb},
		middleware.WithRAGMode(middleware.RAGModeAgentic),
		middleware.WithRAGTopK(3),
	)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	tools, err := mw.ListTools(context.Background(), agent)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "search_knowledge" || !tools[0].IsReadOnly() {
		t.Fatalf("unexpected RAG tools: %#v", tools)
	}
	if !strings.Contains(tools[0].Description(), "handbook") {
		t.Fatalf("tool description should include knowledge base metadata: %q", tools[0].Description())
	}

	response := runRAGTool(t, tools[0], map[string]any{"query": "pto policy"}, agent.AgentState())
	if response.State != message.ToolResultSuccess {
		t.Fatalf("search_knowledge should succeed, got %#v", response)
	}
	text := response.GetTextContent("")
	if text == nil || !strings.Contains(*text, "[1] (source: handbook.md)") ||
		!strings.Contains(*text, "PTO policy allows 15 days.") {
		t.Fatalf("search_knowledge response mismatch: %#v", response)
	}
	if got := model.inputTexts(); !reflect.DeepEqual(got, []string{"pto policy"}) {
		t.Fatalf("embedding inputs = %#v", got)
	}
	if len(store.searchCalls) != 1 || store.searchCalls[0].topK != 3 {
		t.Fatalf("vector search calls mismatch: %#v", store.searchCalls)
	}

	noResult := runRAGTool(t, tools[0], map[string]any{
		"query":           "pto policy",
		"knowledge_bases": []any{"missing"},
	}, agent.AgentState())
	if text := noResult.GetTextContent(""); text == nil || !strings.Contains(*text, "No relevant content found.") {
		t.Fatalf("unknown knowledge base subset should return no content: %#v", noResult)
	}
	if len(store.searchCalls) != 1 {
		t.Fatalf("unknown subset should not search the vector store: %#v", store.searchCalls)
	}
}

func TestRAGMiddlewareSearchesKnowledgeBasesConcurrently(t *testing.T) {
	t.Parallel()

	started := make(chan string, 2)
	release := make(chan struct{})
	kbA := newBlockingRAGTestKnowledgeBase(
		t,
		"benefits",
		started,
		release,
		[]rag.VectorSearchResult{
			ragSearchResult("doc-benefits", 0, 0.93, "Benefits handbook result.", "benefits.md"),
		},
	)
	kbB := newBlockingRAGTestKnowledgeBase(
		t,
		"payroll",
		started,
		release,
		[]rag.VectorSearchResult{
			ragSearchResult("doc-payroll", 0, 0.91, "Payroll handbook result.", "payroll.md"),
		},
	)
	mw := middleware.NewRAGMiddleware(
		[]*rag.KnowledgeBase{kbA, kbB},
		middleware.WithRAGMode(middleware.RAGModeAgentic),
		middleware.WithRAGTopK(2),
	)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}
	tools, err := mw.ListTools(context.Background(), agent)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct {
		response *tool.ToolResponse
		err      error
	}, 1)
	go func() {
		response, err := executeRAGTool(ctx, tools[0], map[string]any{"query": "handbook"}, agent.AgentState())
		done <- struct {
			response *tool.ToolResponse
			err      error
		}{response: response, err: err}
	}()

	first := waitRAGSearchStart(t, started)
	second := waitRAGSearchStart(t, started)
	if first == second {
		t.Fatalf("expected different knowledge bases to start, got %q twice", first)
	}
	close(release)

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("search_knowledge returned error: %v", result.err)
		}
		text := result.response.GetTextContent("")
		if text == nil ||
			!strings.Contains(*text, "Benefits handbook result.") ||
			!strings.Contains(*text, "Payroll handbook result.") {
			t.Fatalf("search_knowledge response mismatch: %#v", result.response)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for concurrent RAG search to finish")
	}
}

func TestRAGMiddlewareStaticModeEmitsHintBlockEvent(t *testing.T) {
	t.Parallel()

	kb, _, _ := newRAGTestKnowledgeBase(t, []rag.VectorSearchResult{
		ragSearchResult("doc-1", 0, 0.88, "PTO policy allows 15 days.", "handbook.md"),
	})
	mw := middleware.NewRAGMiddleware(
		[]*rag.KnowledgeBase{kb},
		middleware.WithRAGMode(middleware.RAGModeStatic),
		middleware.WithRAGTopK(1),
	)
	state := statepkg.NewAgentState()
	state.SessionID = "session-1"
	state.ReplyID = "reply-1"
	agent := fakeAgent{name: "Friday", state: state}

	events, err := mw.OnReply(
		context.Background(),
		agent,
		agentpkg.HookInput{"input": "How much PTO do I have?"},
		func(ctx context.Context) (<-chan message.Event, error) {
			return mw.OnReasoning(ctx, agent, agentpkg.HookInput{}, ragEmptyEventStream)
		},
	)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	got := collectRAGEvents(events)
	if len(got) != 1 {
		t.Fatalf("expected one hint event, got %#v", got)
	}
	hint, ok := got[0].(*message.HintBlockEvent)
	if !ok || hint.ReplyID() != "reply-1" || !strings.Contains(hint.Hint, "PTO policy allows 15 days.") {
		t.Fatalf("hint event mismatch: %#v", got[0])
	}
	if hint.Source == nil || *hint.Source != "knowledge-base" {
		t.Fatalf("hint source mismatch: %#v", hint)
	}
}

func TestRAGMiddlewareStaticModeDefaultInjectsHintBeforeReasoning(t *testing.T) {
	t.Parallel()

	kb, _, _ := newRAGTestKnowledgeBase(t, []rag.VectorSearchResult{
		ragSearchResult("doc-1", 0, 0.88, "PTO policy allows 15 days.", "handbook.md"),
	})
	mw := middleware.NewRAGMiddleware(
		[]*rag.KnowledgeBase{kb},
		middleware.WithRAGMode(middleware.RAGModeStatic),
	)
	state := statepkg.NewAgentState()
	state.SessionID = "session-1"
	state.ReplyID = "reply-1"
	assistant, err := message.NewAssistantMessage("Friday", nil, message.WithMessageID("reply-1"))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	agent := fakeAgent{name: "Friday", state: state}

	events, err := mw.OnReply(
		context.Background(),
		agent,
		agentpkg.HookInput{"input": "How much PTO do I have?"},
		func(ctx context.Context) (<-chan message.Event, error) {
			return mw.OnReasoning(ctx, agent, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
				hints := assistant.Content.GetContentBlocks("hint")
				if len(hints) != 1 || !strings.Contains(hints[0].(*message.HintBlock).Hint, "PTO policy allows 15 days.") {
					t.Fatalf("default RAG hint was not injected before reasoning: %#v", assistant.Content)
				}
				return ragEmptyEventStream(ctx)
			})
		},
	)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	got := collectRAGEvents(events)
	if len(got) != 1 {
		t.Fatalf("expected one hint event, got %#v", got)
	}
	for _, event := range got {
		if err := assistant.ApplyEvent(event); err != nil {
			t.Fatalf("ApplyEvent returned error: %v", err)
		}
	}
	if hints := assistant.Content.GetContentBlocks("hint"); len(hints) != 1 {
		t.Fatalf("hint event replay should not duplicate pre-injected hint: %#v", assistant.Content)
	}
}

func TestRAGMiddlewareStaticModeAppendsHintWhenEventsDisabled(t *testing.T) {
	t.Parallel()

	kb, _, _ := newRAGTestKnowledgeBase(t, []rag.VectorSearchResult{
		ragSearchResult("doc-1", 0, 0.88, "PTO policy allows 15 days.", "handbook.md"),
	})
	mw := middleware.NewRAGMiddleware(
		[]*rag.KnowledgeBase{kb},
		middleware.WithRAGMode(middleware.RAGModeStatic),
		middleware.WithRAGEmitHintEvent(false),
	)
	state := statepkg.NewAgentState()
	state.SessionID = "session-1"
	state.ReplyID = "reply-1"
	assistant, err := message.NewAssistantMessage("Friday", nil, message.WithMessageID("reply-1"))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	agent := fakeAgent{name: "Friday", state: state}

	events, err := mw.OnReply(
		context.Background(),
		agent,
		agentpkg.HookInput{"input": "How much PTO do I have?"},
		func(ctx context.Context) (<-chan message.Event, error) {
			return mw.OnReasoning(ctx, agent, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
				hints := assistant.Content.GetContentBlocks("hint")
				if len(hints) != 1 || !strings.Contains(hints[0].(*message.HintBlock).Hint, "PTO policy allows 15 days.") {
					t.Fatalf("RAG hint was not injected before reasoning: %#v", assistant.Content)
				}
				return ragEmptyEventStream(ctx)
			})
		},
	)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}
	if hints := assistant.Content.GetContentBlocks("hint"); len(hints) != 0 {
		t.Fatalf("non-persistent RAG hint should be removed after reasoning: %#v", assistant.Content)
	}
}

func TestRAGMiddlewareStaticModeSkipsAfterFirstIteration(t *testing.T) {
	t.Parallel()

	kb, store, _ := newRAGTestKnowledgeBase(t, []rag.VectorSearchResult{
		ragSearchResult("doc-1", 0, 0.88, "PTO policy allows 15 days.", "handbook.md"),
	})
	mw := middleware.NewRAGMiddleware(
		[]*rag.KnowledgeBase{kb},
		middleware.WithRAGMode(middleware.RAGModeStatic),
		middleware.WithRAGEmitHintEvent(false),
	)
	state := statepkg.NewAgentState()
	state.SessionID = "session-1"
	state.ReplyID = "reply-1"
	state.CurIter = 1
	assistant, err := message.NewAssistantMessage("Friday", nil, message.WithMessageID("reply-1"))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	agent := fakeAgent{name: "Friday", state: state}

	events, err := mw.OnReply(
		context.Background(),
		agent,
		agentpkg.HookInput{"input": "How much PTO do I have?"},
		func(ctx context.Context) (<-chan message.Event, error) {
			return mw.OnReasoning(ctx, agent, agentpkg.HookInput{}, ragEmptyEventStream)
		},
	)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	for range events {
	}
	if len(store.searchCalls) != 0 {
		t.Fatalf("static RAG should skip searches after first iteration: %#v", store.searchCalls)
	}
	if len(assistant.Content) != 0 {
		t.Fatalf("no hint should be injected after first iteration: %#v", assistant.Content)
	}
}

func runRAGTool(t *testing.T, current agentpkg.Tool, input map[string]any, state *statepkg.AgentState) *tool.ToolResponse {
	t.Helper()
	response, err := executeRAGTool(context.Background(), current, input, state)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return response
}

func executeRAGTool(
	ctx context.Context,
	current agentpkg.Tool,
	input map[string]any,
	state *statepkg.AgentState,
) (*tool.ToolResponse, error) {
	chunks, err := current.Execute(ctx, input, state)
	if err != nil {
		return nil, err
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func waitRAGSearchStart(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case name := <-started:
		return name
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for RAG knowledge base search to start")
		return ""
	}
}

func collectRAGEvents(events <-chan message.Event) []message.Event {
	var out []message.Event
	for event := range events {
		out = append(out, event)
	}
	return out
}

func ragEmptyEventStream(context.Context) (<-chan message.Event, error) {
	out := make(chan message.Event)
	close(out)
	return out, nil
}

func ragSearchResult(documentID string, chunkIndex int, score float64, text string, source string) rag.VectorSearchResult {
	return rag.VectorSearchResult{
		Score:      score,
		DocumentID: documentID,
		Chunk: rag.Chunk{
			Content:     message.NewTextBlock(text),
			Source:      source,
			ChunkIndex:  chunkIndex,
			TotalChunks: 1,
		},
	}
}

func newRAGTestKnowledgeBase(t *testing.T, results []rag.VectorSearchResult) (
	*rag.KnowledgeBase,
	*recordingRAGVectorStore,
	*recordingRAGEmbeddingModel,
) {
	t.Helper()
	model := &recordingRAGEmbeddingModel{
		dimensions: 2,
		embeddings: []types.Embedding{
			{1, 0},
		},
	}
	store := &recordingRAGVectorStore{
		hasCollection: true,
		searchResults: [][]rag.VectorSearchResult{
			results,
		},
	}
	kb, err := rag.NewKnowledgeBase("handbook", "Company handbook.", model, store, "handbook")
	if err != nil {
		t.Fatalf("NewKnowledgeBase returned error: %v", err)
	}
	return kb, store, model
}

func newBlockingRAGTestKnowledgeBase(
	t *testing.T,
	name string,
	started chan<- string,
	release <-chan struct{},
	results []rag.VectorSearchResult,
) *rag.KnowledgeBase {
	t.Helper()
	model := &recordingRAGEmbeddingModel{
		dimensions: 2,
		embeddings: []types.Embedding{
			{1, 0},
		},
	}
	store := &blockingRAGVectorStore{
		name:    name,
		started: started,
		release: release,
		results: results,
	}
	kb, err := rag.NewKnowledgeBase(name, name+" knowledge base.", model, store, name)
	if err != nil {
		t.Fatalf("NewKnowledgeBase returned error: %v", err)
	}
	return kb
}

type recordingRAGEmbeddingModel struct {
	dimensions int
	embeddings []types.Embedding
	requests   []embedding.EmbeddingRequest
}

func (m *recordingRAGEmbeddingModel) Name() string {
	return "recording-rag-embedding"
}

func (m *recordingRAGEmbeddingModel) Dimensions() int {
	return m.dimensions
}

func (m *recordingRAGEmbeddingModel) SupportedModalities() []embedding.Modality {
	return []embedding.Modality{embedding.ModalityText}
}

func (m *recordingRAGEmbeddingModel) Embed(_ context.Context, request embedding.EmbeddingRequest) (*embedding.EmbeddingResponse, error) {
	m.requests = append(m.requests, request.Clone())
	return embedding.NewEmbeddingResponse(m.embeddings), nil
}

func (m *recordingRAGEmbeddingModel) inputTexts() []string {
	if len(m.requests) == 0 {
		return nil
	}
	texts := make([]string, 0, len(m.requests[len(m.requests)-1].Inputs))
	for _, input := range m.requests[len(m.requests)-1].Inputs {
		texts = append(texts, input.Text)
	}
	return texts
}

type recordingRAGVectorStore struct {
	hasCollection bool
	searchResults [][]rag.VectorSearchResult
	searchCalls   []ragSearchCall
}

type ragSearchCall struct {
	collection string
	topK       int
}

func (s *recordingRAGVectorStore) CreateCollection(_ context.Context, _ string, _ int) error {
	s.hasCollection = true
	return nil
}

func (s *recordingRAGVectorStore) DeleteCollection(context.Context, string) error {
	s.hasCollection = false
	return nil
}

func (s *recordingRAGVectorStore) HasCollection(context.Context, string) (bool, error) {
	return s.hasCollection, nil
}

func (s *recordingRAGVectorStore) Insert(context.Context, string, []rag.VectorRecord) error {
	return nil
}

func (s *recordingRAGVectorStore) Delete(context.Context, string, string) error {
	return nil
}

func (s *recordingRAGVectorStore) Search(
	_ context.Context,
	collection string,
	_ types.Embedding,
	topK int,
	_ map[string]any,
) ([]rag.VectorSearchResult, error) {
	s.searchCalls = append(s.searchCalls, ragSearchCall{collection: collection, topK: topK})
	index := len(s.searchCalls) - 1
	if index >= len(s.searchResults) {
		return nil, nil
	}
	return s.searchResults[index], nil
}

func (s *recordingRAGVectorStore) ListDocuments(context.Context, string, map[string]any) ([]rag.DocumentSummary, error) {
	return nil, nil
}

type blockingRAGVectorStore struct {
	name    string
	started chan<- string
	release <-chan struct{}
	results []rag.VectorSearchResult
}

func (s *blockingRAGVectorStore) CreateCollection(context.Context, string, int) error {
	return nil
}

func (s *blockingRAGVectorStore) DeleteCollection(context.Context, string) error {
	return nil
}

func (s *blockingRAGVectorStore) HasCollection(context.Context, string) (bool, error) {
	return true, nil
}

func (s *blockingRAGVectorStore) Insert(context.Context, string, []rag.VectorRecord) error {
	return nil
}

func (s *blockingRAGVectorStore) Delete(context.Context, string, string) error {
	return nil
}

func (s *blockingRAGVectorStore) Search(
	ctx context.Context,
	_ string,
	_ types.Embedding,
	_ int,
	_ map[string]any,
) ([]rag.VectorSearchResult, error) {
	select {
	case s.started <- s.name:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-s.release:
		return append([]rag.VectorSearchResult(nil), s.results...), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *blockingRAGVectorStore) ListDocuments(context.Context, string, map[string]any) ([]rag.DocumentSummary, error) {
	return nil, nil
}
