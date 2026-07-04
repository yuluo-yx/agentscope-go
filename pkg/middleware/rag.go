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
	"sort"
	"strings"
	"sync"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

const (
	defaultRAGTopK        = 5
	defaultRAGHintSource  = "knowledge-base"
	defaultRAGHintPattern = "<system-reminder>The following content is retrieved from the knowledge base(s) and may be helpful for the current request:\n<content>{context}</content></system-reminder>"
)

// RAGMode controls how an Agent uses equipped knowledge bases.
type RAGMode string

const (
	// RAGModeAgentic exposes search_knowledge and lets the Agent decide when to retrieve.
	RAGModeAgentic RAGMode = "agentic"
	// RAGModeStatic automatically retrieves on the first reasoning step of each reply.
	RAGModeStatic RAGMode = "static"
)

// RAGOption configures RAGMiddleware.
type RAGOption func(*RAGMiddleware)

// RAGMiddleware integrates one or more knowledge bases into the Agent loop.
type RAGMiddleware struct {
	knowledgeBases []*rag.KnowledgeBase
	mode           RAGMode
	topK           int

	scoreThreshold    float64
	hasScoreThreshold bool

	emitHintEvent bool
	persistHint   bool
	hintTemplate  string
	hintSource    string

	cacheMu      sync.Mutex
	cachedInputs map[string]message.ContentBlockList
}

// NewRAGMiddleware creates RAG middleware over the provided knowledge bases.
func NewRAGMiddleware(knowledgeBases []*rag.KnowledgeBase, opts ...RAGOption) *RAGMiddleware {
	m := &RAGMiddleware{
		knowledgeBases: compactKnowledgeBases(knowledgeBases),
		mode:           RAGModeAgentic,
		topK:           defaultRAGTopK,
		emitHintEvent:  true,
		hintTemplate:   defaultRAGHintPattern,
		hintSource:     defaultRAGHintSource,
		cachedInputs:   map[string]message.ContentBlockList{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	if m.mode == "" {
		m.mode = RAGModeAgentic
	}
	if m.topK <= 0 {
		m.topK = defaultRAGTopK
	}
	if strings.Count(m.hintTemplate, "{context}") != 1 {
		m.hintTemplate = defaultRAGHintPattern
	}
	if strings.TrimSpace(m.hintSource) == "" {
		m.hintSource = defaultRAGHintSource
	}
	if m.cachedInputs == nil {
		m.cachedInputs = map[string]message.ContentBlockList{}
	}
	return m
}

// WithRAGMode sets the retrieval mode.
func WithRAGMode(mode RAGMode) RAGOption {
	return func(m *RAGMiddleware) {
		m.mode = mode
	}
}

// WithRAGTopK sets the maximum merged search results.
func WithRAGTopK(topK int) RAGOption {
	return func(m *RAGMiddleware) {
		m.topK = topK
	}
}

// WithRAGScoreThreshold filters search results below the provided score.
func WithRAGScoreThreshold(score float64) RAGOption {
	return func(m *RAGMiddleware) {
		m.scoreThreshold = score
		m.hasScoreThreshold = true
	}
}

// WithRAGEmitHintEvent controls whether static mode emits HintBlockEvent.
func WithRAGEmitHintEvent(enabled bool) RAGOption {
	return func(m *RAGMiddleware) {
		m.emitHintEvent = enabled
	}
}

// WithRAGPersistHint controls whether static-mode hints remain in context after reasoning.
func WithRAGPersistHint(enabled bool) RAGOption {
	return func(m *RAGMiddleware) {
		m.persistHint = enabled
	}
}

// WithRAGHintTemplate sets the static-mode hint wrapper. It must contain one {context} placeholder.
func WithRAGHintTemplate(template string) RAGOption {
	return func(m *RAGMiddleware) {
		m.hintTemplate = template
	}
}

// MiddlewareName returns the middleware name.
func (*RAGMiddleware) MiddlewareName() string {
	return "rag"
}

// OnReply caches the current user input so static retrieval can run during reasoning.
func (m *RAGMiddleware) OnReply(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	if m == nil || m.mode != RAGModeStatic {
		return next(ctx)
	}

	key := ragCacheKey(agent)
	blocks := ragInputBlocks(input["input"])
	if len(blocks) > 0 {
		m.setCachedInputs(key, blocks)
	}

	events, err := next(ctx)
	if err != nil {
		m.clearCachedInputs(key)
		return nil, err
	}
	if events == nil {
		m.clearCachedInputs(key)
		return nil, fmt.Errorf("agentscope/middleware: nil event stream")
	}

	out := make(chan message.Event)
	go func() {
		defer close(out)
		defer m.clearCachedInputs(key)
		for event := range events {
			out <- event
		}
	}()
	return out, nil
}

// OnReasoning performs static retrieval once, before the first model call of a reply.
func (m *RAGMiddleware) OnReasoning(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	if m == nil || m.mode != RAGModeStatic || len(m.knowledgeBases) == 0 || agent == nil {
		return next(ctx)
	}
	state, hintID, hintContent, ok := m.prepareStaticHint(ctx, agent, input)
	if !ok {
		return next(ctx)
	}
	hint := message.NewHintBlock(
		hintContent,
		message.WithHintBlockID(hintID),
		message.WithHintSource(m.hintSource),
	)
	if err := appendInboxBlocks(agent, message.ContentBlockList{hint}); err != nil {
		return nil, err
	}

	events, err := next(ctx)
	if err != nil {
		if !m.persistHint {
			removeHintBlock(agent, hintID)
		}
		return nil, err
	}
	if events == nil {
		if !m.persistHint {
			removeHintBlock(agent, hintID)
		}
		return nil, fmt.Errorf("agentscope/middleware: nil event stream")
	}

	if m.emitHintEvent {
		event := message.NewHintBlockEvent(
			state.ReplyID,
			hintID,
			hintContent,
			message.WithHintBlockEventSource(m.hintSource),
		)
		return m.reasonWithHintStream(ctx, agent, events, event, hintID), nil
	}
	if m.persistHint {
		return events, nil
	}

	out := make(chan message.Event)
	go func() {
		defer close(out)
		defer removeHintBlock(agent, hintID)
		for event := range events {
			out <- event
		}
	}()
	return out, nil
}

func (m *RAGMiddleware) prepareStaticHint(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
) (*statepkg.AgentState, string, any, bool) {
	state := agent.AgentState()
	if state == nil || state.CurIter > 0 {
		return nil, "", nil, false
	}

	queries := m.getCachedInputs(ragCacheKey(agent))
	if len(queries) == 0 {
		return nil, "", nil, false
	}
	results, err := m.search(ctx, m.knowledgeBases, queries)
	if err != nil {
		input["rag_error"] = err
		return nil, "", nil, false
	}
	blocks := formatRAGResults(results)
	if len(blocks) == 0 {
		return nil, "", nil, false
	}

	return state, utils.NewID(), wrapRAGHint(m.hintTemplate, blocks), true
}

// ListTools exposes search_knowledge in agentic mode.
func (m *RAGMiddleware) ListTools(ctx context.Context, agent agentpkg.AgentAccessor) ([]agentpkg.Tool, error) {
	_ = ctx
	_ = agent
	if m == nil || m.mode != RAGModeAgentic {
		return nil, nil
	}

	searchTool, err := astool.NewFunctionTool(
		"search_knowledge",
		buildRAGToolDescription(m.knowledgeBases),
		buildRAGToolSchema(m.knowledgeBases),
		func(ctx context.Context, input map[string]any, _ *statepkg.AgentState) (message.ContentBlockList, error) {
			query := strings.TrimSpace(ragString(input["query"]))
			if query == "" {
				return nil, fmt.Errorf("rag: query is required")
			}

			targets := selectKnowledgeBases(m.knowledgeBases, input["knowledge_bases"])
			if len(targets) == 0 {
				return message.ContentBlockList{message.NewTextBlock("No relevant content found.")}, nil
			}

			results, err := m.search(ctx, targets, message.ContentBlockList{message.NewTextBlock(query)})
			if err != nil {
				return nil, err
			}
			blocks := formatRAGResults(results)
			if len(blocks) == 0 {
				return message.ContentBlockList{message.NewTextBlock("No relevant content found.")}, nil
			}
			return blocks, nil
		},
		astool.WithFunctionReadOnly(true),
	)
	if err != nil {
		return nil, err
	}
	return []agentpkg.Tool{searchTool}, nil
}

func (m *RAGMiddleware) search(
	ctx context.Context,
	knowledgeBases []*rag.KnowledgeBase,
	queries message.ContentBlockList,
) ([]rag.VectorSearchResult, error) {
	opts := []rag.SearchOption{rag.WithSearchTopK(m.topK)}
	if m.hasScoreThreshold {
		opts = append(opts, rag.WithScoreThreshold(m.scoreThreshold))
	}

	targets := compactKnowledgeBases(knowledgeBases)
	if len(targets) == 0 {
		return nil, nil
	}

	type searchResponse struct {
		results []rag.VectorSearchResult
		err     error
	}

	responses := make(chan searchResponse, len(targets))
	for _, kb := range targets {
		kb := kb
		go func() {
			results, err := kb.Search(ctx, queries, opts...)
			responses <- searchResponse{results: results, err: err}
		}()
	}

	merged := []rag.VectorSearchResult{}
	for range targets {
		select {
		case response := <-responses:
			if response.err != nil {
				return nil, response.err
			}
			merged = append(merged, response.results...)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})
	if len(merged) > m.topK {
		merged = merged[:m.topK]
	}
	return merged, nil
}

func (m *RAGMiddleware) reasonWithHintStream(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	events <-chan message.Event,
	hintEvent *message.HintBlockEvent,
	hintID string,
) <-chan message.Event {
	out := make(chan message.Event)
	go func() {
		defer close(out)
		if hintEvent != nil {
			select {
			case out <- hintEvent:
			case <-ctx.Done():
				return
			}
		}
		defer func() {
			if !m.persistHint {
				removeHintBlock(agent, hintID)
			}
		}()
		for event := range events {
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func (m *RAGMiddleware) setCachedInputs(key string, blocks message.ContentBlockList) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	m.cachedInputs[key] = blocks.Clone()
}

func (m *RAGMiddleware) getCachedInputs(key string) message.ContentBlockList {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	return m.cachedInputs[key].Clone()
}

func (m *RAGMiddleware) clearCachedInputs(key string) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	delete(m.cachedInputs, key)
}

func compactKnowledgeBases(in []*rag.KnowledgeBase) []*rag.KnowledgeBase {
	out := make([]*rag.KnowledgeBase, 0, len(in))
	for _, kb := range in {
		if kb != nil {
			out = append(out, kb)
		}
	}
	return out
}

func ragCacheKey(agent agentpkg.AgentAccessor) string {
	if agent == nil || agent.AgentState() == nil {
		return ""
	}
	return agent.AgentState().SessionID
}

func ragInputBlocks(value any) message.ContentBlockList {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		return message.ContentBlockList{message.NewTextBlock(text)}
	case fmt.Stringer:
		text := strings.TrimSpace(typed.String())
		if text == "" {
			return nil
		}
		return message.ContentBlockList{message.NewTextBlock(text)}
	case message.ContentBlockList:
		return typed.Clone()
	case *message.Message:
		return messageBlocksWithSpeaker(typed)
	case message.Message:
		return messageBlocksWithSpeaker(&typed)
	case []*message.Message:
		out := message.ContentBlockList{}
		for _, msg := range typed {
			out = append(out, messageBlocksWithSpeaker(msg)...)
		}
		return out
	case []message.Message:
		out := message.ContentBlockList{}
		for index := range typed {
			out = append(out, messageBlocksWithSpeaker(&typed[index])...)
		}
		return out
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			return nil
		}
		return message.ContentBlockList{message.NewTextBlock(text)}
	}
}

func messageBlocksWithSpeaker(msg *message.Message) message.ContentBlockList {
	if msg == nil || len(msg.Content) == 0 {
		return nil
	}
	blocks := msg.Content.Clone()
	prefix := strings.TrimSpace(msg.Name)
	if prefix == "" {
		return blocks
	}
	prefix += ": "
	if first, ok := blocks[0].(*message.TextBlock); ok {
		first.Text = prefix + first.Text
		return blocks
	}
	return append(message.ContentBlockList{message.NewTextBlock(prefix)}, blocks...)
}

func buildRAGToolDescription(knowledgeBases []*rag.KnowledgeBase) string {
	lines := []string{
		"Search the agent's equipped knowledge bases by semantic similarity and return the most relevant chunks.",
		"",
		"Use this when the user's request may depend on facts, definitions, or documents stored in the equipped knowledge bases.",
		"Phrase query as a concise, self-contained statement.",
		"",
		"## Equipped Knowledge Bases",
	}
	if len(knowledgeBases) == 0 {
		lines = append(lines, "No knowledge bases are currently equipped. Do not call this tool.")
		return strings.Join(lines, "\n")
	}
	for _, kb := range knowledgeBases {
		lines = append(lines, fmt.Sprintf("- **%s**: %s", kb.Name(), kb.Description()))
	}
	return strings.Join(lines, "\n")
}

func buildRAGToolSchema(knowledgeBases []*rag.KnowledgeBase) map[string]any {
	names := make([]any, 0, len(knowledgeBases))
	for _, kb := range knowledgeBases {
		names = append(names, kb.Name())
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Self-contained query string to search the knowledge bases with.",
			},
			"knowledge_bases": map[string]any{
				"type":        "array",
				"description": "Optional exact knowledge base names to search. Omit to search all equipped bases.",
				"items": map[string]any{
					"type": "string",
					"enum": names,
				},
			},
		},
		"required": []string{"query"},
	}
}

func selectKnowledgeBases(knowledgeBases []*rag.KnowledgeBase, value any) []*rag.KnowledgeBase {
	names, limited := ragStringList(value)
	if !limited {
		return compactKnowledgeBases(knowledgeBases)
	}
	wanted := map[string]struct{}{}
	for _, name := range names {
		wanted[name] = struct{}{}
	}
	out := make([]*rag.KnowledgeBase, 0, len(wanted))
	for _, kb := range knowledgeBases {
		if kb == nil {
			continue
		}
		if _, ok := wanted[kb.Name()]; ok {
			out = append(out, kb)
		}
	}
	return out
}

func ragStringList(value any) ([]string, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(ragString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out, true
	default:
		text := strings.TrimSpace(ragString(value))
		if text == "" {
			return nil, true
		}
		return []string{text}, true
	}
}

func ragString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case message.ContentBlockList:
		if text := typed.GetTextContent("\n"); text != nil {
			return *text
		}
	case *message.Message:
		if typed != nil {
			return ragString(typed.Content)
		}
	}
	return fmt.Sprint(value)
}

func formatRAGResults(results []rag.VectorSearchResult) message.ContentBlockList {
	if len(results) == 0 {
		return nil
	}
	entries := message.ContentBlockList{}
	for index, result := range results {
		prefix := fmt.Sprintf("[%d] (source: %s)\n", index+1, result.Chunk.Source)
		switch block := result.Chunk.Content.(type) {
		case *message.TextBlock:
			text := block.Clone().(*message.TextBlock)
			text.Text = prefix + text.Text
			entries = append(entries, text)
		case *message.DataBlock:
			entries = append(entries, message.NewTextBlock(prefix), block.Clone())
		default:
			continue
		}
		if index != len(results)-1 {
			entries = append(entries, message.NewTextBlock("\n\n"))
		}
	}
	return mergeRAGTextBlocks(entries)
}

func mergeRAGTextBlocks(blocks message.ContentBlockList) message.ContentBlockList {
	merged := message.ContentBlockList{}
	for _, block := range blocks {
		text, ok := block.(*message.TextBlock)
		if ok && len(merged) > 0 {
			if previous, ok := merged[len(merged)-1].(*message.TextBlock); ok {
				previous.Text += text.Text
				continue
			}
		}
		merged = append(merged, block.Clone())
	}
	return merged
}

func wrapRAGHint(template string, blocks message.ContentBlockList) any {
	if len(blocks) == 0 {
		return ""
	}
	if strings.Count(template, "{context}") != 1 {
		template = defaultRAGHintPattern
	}
	allText := true
	texts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		text, ok := block.(*message.TextBlock)
		if !ok {
			allText = false
			break
		}
		texts = append(texts, text.Text)
	}
	if allText {
		return strings.Replace(template, "{context}", strings.Join(texts, "\n"), 1)
	}

	prefix, suffix, _ := strings.Cut(template, "{context}")
	wrapped := blocks.Clone()
	if prefix != "" {
		if first, ok := wrapped[0].(*message.TextBlock); ok {
			first.Text = prefix + first.Text
		} else {
			wrapped = append(message.ContentBlockList{message.NewTextBlock(prefix)}, wrapped...)
		}
	}
	if suffix != "" {
		if last, ok := wrapped[len(wrapped)-1].(*message.TextBlock); ok {
			last.Text += suffix
		} else {
			wrapped = append(wrapped, message.NewTextBlock(suffix))
		}
	}
	return wrapped
}

func removeHintBlock(agent agentpkg.AgentAccessor, blockID string) {
	if agent == nil || agent.AgentState() == nil || blockID == "" {
		return
	}
	for _, msg := range agent.AgentState().Context {
		if msg == nil {
			continue
		}
		filtered := msg.Content[:0]
		for _, block := range msg.Content {
			if block == nil || block.BlockID() != blockID {
				filtered = append(filtered, block)
			}
		}
		msg.Content = filtered
	}
}
