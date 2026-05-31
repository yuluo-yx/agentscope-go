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

package openai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdk "github.com/openai/openai-go"

	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestParseStreamEmitsTerminalError(t *testing.T) {
	t.Parallel()

	stream := &failingChatCompletionStream{err: errors.New("broken openai stream")}
	out := make(chan asmodel.ChatResponse, 1)

	parseStream(context.Background(), stream, "openai", time.Now(), out)
	close(out)

	chunks := collectChatResponses(out)
	if len(chunks) != 1 {
		t.Fatalf("expected one terminal error chunk, got %d %#v", len(chunks), chunks)
	}
	if chunks[0].Error == nil || !strings.Contains(chunks[0].Error.Error(), "broken openai stream") {
		t.Fatalf("expected stream error on terminal chunk, got %#v", chunks[0].Error)
	}
	if !chunks[0].IsLast {
		t.Fatalf("terminal error chunk should be marked last: %#v", chunks[0])
	}
	if !stream.closed {
		t.Fatal("stream should be closed after parsing")
	}
}

type failingChatCompletionStream struct {
	err    error
	closed bool
}

func (s *failingChatCompletionStream) Next() bool {
	return false
}

func (s *failingChatCompletionStream) Current() sdk.ChatCompletionChunk {
	return sdk.ChatCompletionChunk{}
}

func (s *failingChatCompletionStream) Err() error {
	return s.err
}

func (s *failingChatCompletionStream) Close() error {
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
