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
	"reflect"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/embedding"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

func TestRAGInternalInputAndStringHelpers(t *testing.T) {
	if blocks := ragInputBlocks(nil); blocks != nil {
		t.Fatalf("nil input should produce nil blocks, got %#v", blocks)
	}
	if blocks := ragInputBlocks("  "); blocks != nil {
		t.Fatalf("blank string should produce nil blocks, got %#v", blocks)
	}
	if got := internalRAGText(ragInputBlocks("  hello  ")); got != "hello" {
		t.Fatalf("string input text = %q", got)
	}
	if got := internalRAGText(ragInputBlocks(internalRAGStringer("from stringer"))); got != "from stringer" {
		t.Fatalf("stringer input text = %q", got)
	}

	list := message.ContentBlockList{message.NewTextBlock("original")}
	cloned := ragInputBlocks(list)
	list[0].(*message.TextBlock).Text = "changed"
	if got := internalRAGText(cloned); got != "original" {
		t.Fatalf("content block list should be cloned, got %q", got)
	}

	user, err := message.NewUserMessage("Tony", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	if got := internalRAGText(ragInputBlocks(user)); got != "Tony: hello" {
		t.Fatalf("message input text = %q", got)
	}
	if got := internalRAGText(ragInputBlocks(*user)); got != "Tony: hello" {
		t.Fatalf("message value input text = %q", got)
	}
	if got := internalRAGText(ragInputBlocks([]*message.Message{nil, user})); got != "Tony: hello" {
		t.Fatalf("message pointer slice input text = %q", got)
	}
	if got := internalRAGText(ragInputBlocks([]message.Message{*user})); got != "Tony: hello" {
		t.Fatalf("message value slice input text = %q", got)
	}
	if got := internalRAGText(ragInputBlocks(42)); got != "42" {
		t.Fatalf("default input text = %q", got)
	}

	data := message.NewDataBlock(message.NewBase64Source("AA==", "image/png"))
	dataMsg := &message.Message{Name: "Friday", Content: message.ContentBlockList{data}}
	withPrefix := messageBlocksWithSpeaker(dataMsg)
	if len(withPrefix) != 2 || internalRAGText(withPrefix[:1]) != "Friday: " {
		t.Fatalf("non-text first block should get prefix block, got %#v", withPrefix)
	}

	if names, limited := ragStringList(nil); limited || names != nil {
		t.Fatalf("nil knowledge_bases should be unlimited, names=%#v limited=%t", names, limited)
	}
	if names, limited := ragStringList([]string{" alpha ", " "}); !limited || !reflect.DeepEqual(names, []string{"alpha"}) {
		t.Fatalf("[]string names mismatch: %#v limited=%t", names, limited)
	}
	if names, limited := ragStringList([]any{"alpha", 12, " "}); !limited || !reflect.DeepEqual(names, []string{"alpha", "12"}) {
		t.Fatalf("[]any names mismatch: %#v limited=%t", names, limited)
	}
	if names, limited := ragStringList(" beta "); !limited || !reflect.DeepEqual(names, []string{"beta"}) {
		t.Fatalf("single name mismatch: %#v limited=%t", names, limited)
	}
	if names, limited := ragStringList(" "); !limited || names != nil {
		t.Fatalf("blank name should be limited empty, names=%#v limited=%t", names, limited)
	}
	if got := ragString(message.ContentBlockList{message.NewTextBlock("a"), message.NewTextBlock("b")}); got != "a\nb" {
		t.Fatalf("block list string = %q", got)
	}
	if got := ragString(user); got != "hello" {
		t.Fatalf("message string = %q", got)
	}
	if got := ragString(7); got != "7" {
		t.Fatalf("default ragString = %q", got)
	}
}

func TestRAGInternalKnowledgeBaseSelectionAndSchema(t *testing.T) {
	alpha := newInternalRAGKnowledgeBase(t, "alpha")
	beta := newInternalRAGKnowledgeBase(t, "beta")
	kbs := []*rag.KnowledgeBase{alpha, nil, beta}

	if compacted := compactKnowledgeBases(kbs); len(compacted) != 2 {
		t.Fatalf("compactKnowledgeBases mismatch: %#v", compacted)
	}
	if description := buildRAGToolDescription(nil); !strings.Contains(description, "No knowledge bases") {
		t.Fatalf("empty description mismatch: %q", description)
	}
	description := buildRAGToolDescription([]*rag.KnowledgeBase{alpha, beta})
	if !strings.Contains(description, "**alpha**") || !strings.Contains(description, "**beta**") {
		t.Fatalf("description should include knowledge base names: %q", description)
	}
	schema := buildRAGToolSchema([]*rag.KnowledgeBase{alpha, beta})
	properties := schema["properties"].(map[string]any)
	kbItems := properties["knowledge_bases"].(map[string]any)["items"].(map[string]any)
	if !reflect.DeepEqual(kbItems["enum"], []any{"alpha", "beta"}) {
		t.Fatalf("schema enum mismatch: %#v", kbItems["enum"])
	}

	if selected := selectKnowledgeBases(kbs, nil); len(selected) != 2 {
		t.Fatalf("nil selector should choose all compacted bases: %#v", selected)
	}
	if selected := selectKnowledgeBases(kbs, []string{"beta"}); len(selected) != 1 || selected[0].Name() != "beta" {
		t.Fatalf("[]string selector mismatch: %#v", selected)
	}
	if selected := selectKnowledgeBases(kbs, []any{"alpha", "missing"}); len(selected) != 1 || selected[0].Name() != "alpha" {
		t.Fatalf("[]any selector mismatch: %#v", selected)
	}
	if selected := selectKnowledgeBases(kbs, " "); len(selected) != 0 {
		t.Fatalf("blank selector should choose no bases, got %#v", selected)
	}
}

func TestRAGInternalFormattingWrappingAndRemoval(t *testing.T) {
	if blocks := formatRAGResults(nil); blocks != nil {
		t.Fatalf("empty RAG results should format to nil, got %#v", blocks)
	}

	image := message.NewDataBlock(message.NewBase64Source("AA==", "image/png"))
	results := []rag.VectorSearchResult{
		internalRAGResult("doc-text", 0.9, message.NewTextBlock("text result"), "text.md"),
		internalRAGResult("doc-image", 0.8, image, "image.png"),
		internalRAGResult("doc-thinking", 0.7, message.NewThinkingBlock("hidden"), "hidden.md"),
	}
	blocks := formatRAGResults(results)
	if len(blocks) != 3 {
		t.Fatalf("expected merged text, data block, trailing separator; got %#v", blocks)
	}
	formattedText := internalRAGText(blocks[:1])
	if !strings.Contains(formattedText, "[1] (source: text.md)") ||
		!strings.Contains(formattedText, "[2] (source: image.png)") {
		t.Fatalf("formatted text missing source prefixes: %q", formattedText)
	}
	if _, ok := blocks[1].(*message.DataBlock); !ok {
		t.Fatalf("second formatted block should be data, got %T", blocks[1])
	}

	wrapped := wrapRAGHint("before {context} after", message.ContentBlockList{message.NewTextBlock("context")})
	if wrapped != "before context after" {
		t.Fatalf("text-only wrapped hint mismatch: %#v", wrapped)
	}
	fallback := wrapRAGHint("{context}{context}", message.ContentBlockList{message.NewTextBlock("context")})
	if text, ok := fallback.(string); !ok || !strings.Contains(text, "<system-reminder>") {
		t.Fatalf("invalid template should use default wrapper, got %#v", fallback)
	}
	mixed := wrapRAGHint("before {context} after", blocks).(message.ContentBlockList)
	if !strings.HasPrefix(internalRAGText(mixed[:1]), "before ") ||
		!strings.HasSuffix(internalRAGText(mixed[len(mixed)-1:]), " after") {
		t.Fatalf("mixed wrapped hint mismatch: %#v", mixed)
	}

	state := statepkg.NewAgentState()
	msg := &message.Message{
		Name: "Friday",
		Content: message.ContentBlockList{
			message.NewHintBlock("remove", message.WithHintBlockID("hint-remove")),
			nil,
			message.NewTextBlock("keep"),
		},
	}
	state.Context = append(state.Context, msg)
	removeHintBlock(internalRAGAgent{state: state}, "hint-remove")
	if len(msg.Content) != 2 || internalRAGText(msg.Content[1:]) != "keep" {
		t.Fatalf("hint block should be removed while keeping other blocks: %#v", msg.Content)
	}
	removeHintBlock(nil, "hint-remove")
	removeHintBlock(internalRAGAgent{state: nil}, "hint-remove")
	removeHintBlock(internalRAGAgent{state: state}, "")
}

func internalRAGText(blocks message.ContentBlockList) string {
	text := blocks.GetTextContent("")
	if text == nil {
		return ""
	}
	return *text
}

type internalRAGStringer string

func (s internalRAGStringer) String() string {
	return string(s)
}

type internalRAGAgent struct {
	state *statepkg.AgentState
}

func (a internalRAGAgent) AgentName() string {
	return "Friday"
}

func (a internalRAGAgent) AgentState() *statepkg.AgentState {
	return a.state
}

var _ agentpkg.AgentAccessor = internalRAGAgent{}

func newInternalRAGKnowledgeBase(t *testing.T, name string) *rag.KnowledgeBase {
	t.Helper()
	kb, err := rag.NewKnowledgeBase(
		name,
		name+" description",
		internalRAGEmbeddingModel{},
		internalRAGVectorStore{},
		name,
	)
	if err != nil {
		t.Fatalf("NewKnowledgeBase returned error: %v", err)
	}
	return kb
}

type internalRAGEmbeddingModel struct{}

func (internalRAGEmbeddingModel) Name() string {
	return "internal-rag-embedding"
}

func (internalRAGEmbeddingModel) Dimensions() int {
	return 2
}

func (internalRAGEmbeddingModel) SupportedModalities() []embedding.Modality {
	return []embedding.Modality{embedding.ModalityText}
}

func (internalRAGEmbeddingModel) Embed(context.Context, embedding.EmbeddingRequest) (*embedding.EmbeddingResponse, error) {
	return embedding.NewEmbeddingResponse([]types.Embedding{{1, 0}}), nil
}

type internalRAGVectorStore struct{}

func (internalRAGVectorStore) CreateCollection(context.Context, string, int) error {
	return nil
}

func (internalRAGVectorStore) DeleteCollection(context.Context, string) error {
	return nil
}

func (internalRAGVectorStore) HasCollection(context.Context, string) (bool, error) {
	return true, nil
}

func (internalRAGVectorStore) Insert(context.Context, string, []rag.VectorRecord) error {
	return nil
}

func (internalRAGVectorStore) Delete(context.Context, string, string) error {
	return nil
}

func (internalRAGVectorStore) Search(
	context.Context,
	string,
	types.Embedding,
	int,
	map[string]any,
) ([]rag.VectorSearchResult, error) {
	return nil, nil
}

func (internalRAGVectorStore) ListDocuments(context.Context, string, map[string]any) ([]rag.DocumentSummary, error) {
	return nil, nil
}

func internalRAGResult(
	documentID string,
	score float64,
	block message.ContentBlock,
	source string,
) rag.VectorSearchResult {
	return rag.VectorSearchResult{
		Score:      score,
		DocumentID: documentID,
		Chunk: rag.Chunk{
			Content: block,
			Source:  source,
		},
	}
}
