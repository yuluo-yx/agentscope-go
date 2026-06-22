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

package anthropic

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"

	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
)

func TestParseStreamEmitsTerminalError(t *testing.T) {
	t.Parallel()

	stream := &failingMessageStream{err: errors.New("broken anthropic stream")}
	out := make(chan asmodel.ChatResponse, 1)

	parseStream(context.Background(), stream, time.Now(), out)
	close(out)

	chunks := collectChatResponses(out)
	if len(chunks) != 1 {
		t.Fatalf("expected one terminal error chunk, got %d %#v", len(chunks), chunks)
	}
	if chunks[0].Error == nil || !strings.Contains(chunks[0].Error.Error(), "broken anthropic stream") {
		t.Fatalf("expected stream error on terminal chunk, got %#v", chunks[0].Error)
	}
	if !chunks[0].IsLast {
		t.Fatalf("terminal error chunk should be marked last: %#v", chunks[0])
	}
	if !stream.closed {
		t.Fatal("stream should be closed after parsing")
	}
}

func TestListModelsLoadsAnthropicModelCards(t *testing.T) {
	t.Parallel()

	cards, err := ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatalf("expected embedded Anthropic model cards")
	}
	found := false
	for _, card := range cards {
		if card.Name == "claude-sonnet-4-5" {
			found = true
			if card.Type != asmodel.ModelCardTypeChat || card.Extra["provider"] != "anthropic" || !card.Capabilities[asmodel.ModelCapabilityGeneration] || !card.Capabilities[asmodel.ModelCapabilityTools] {
				t.Fatalf("Anthropic model card metadata mismatch: %#v", card)
			}
		}
	}
	if !found {
		t.Fatalf("missing claude-sonnet-4-5 card: %#v", cards)
	}
}

type failingMessageStream struct {
	err    error
	closed bool
}

func (s *failingMessageStream) Next() bool {
	return false
}

func (s *failingMessageStream) Current() sdk.MessageStreamEventUnion {
	return sdk.MessageStreamEventUnion{}
}

func (s *failingMessageStream) Err() error {
	return s.err
}

func (s *failingMessageStream) Close() error {
	s.closed = true
	return nil
}

func collectChatResponses(out <-chan asmodel.ChatResponse) []asmodel.ChatResponse {
	chunks := []asmodel.ChatResponse{}
	for chunk := range out {
		chunks = append(chunks, chunk)
	}
	return chunks
}
