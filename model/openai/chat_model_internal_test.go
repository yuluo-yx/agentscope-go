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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdk "github.com/openai/openai-go"

	"github.com/yuluo-yx/agentscope-go/message"
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

func TestParseStreamAccumulatesAudioDeltas(t *testing.T) {
	t.Parallel()

	audioOne := base64.StdEncoding.EncodeToString([]byte("abc"))
	audioTwo := base64.StdEncoding.EncodeToString([]byte("def"))
	stream := &sequenceChatCompletionStream{chunks: []sdk.ChatCompletionChunk{
		mustChunk(t, `{"id":"chatcmpl-audio-stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"audio":{"id":"audio-1","data":"`+audioOne+`","transcript":"he"}},"finish_reason":null}]}`),
		mustChunk(t, `{"id":"chatcmpl-audio-stream","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"audio":{"data":"`+audioTwo+`","transcript":"llo"}},"finish_reason":"stop"}]}`),
		mustChunk(t, `{"id":"chatcmpl-audio-stream","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`),
	}}
	out := make(chan asmodel.ChatResponse, 4)

	parseStream(context.Background(), stream, "openai", time.Now(), out)
	close(out)

	chunks := collectChatResponses(out)
	if len(chunks) != 3 {
		t.Fatalf("expected two audio deltas and one final response, got %d %#v", len(chunks), chunks)
	}
	if text := chunks[0].Content.GetTextContent(""); text == nil || *text != "he" {
		t.Fatalf("first transcript delta not folded into text: %#v", chunks[0])
	}
	firstPayload := audioPayload(t, chunks[0].Content[1])
	if !bytes.HasPrefix(firstPayload, []byte("RIFF")) || !bytes.Contains(firstPayload, []byte("abc")) {
		t.Fatalf("first audio delta should include streaming WAV header and PCM bytes: %q", firstPayload)
	}
	if text := chunks[1].Content.GetTextContent(""); text == nil || *text != "llo" {
		t.Fatalf("second transcript delta not folded into text: %#v", chunks[1])
	}
	secondPayload := audioPayload(t, chunks[1].Content[1])
	if !bytes.Equal(secondPayload, []byte("def")) {
		t.Fatalf("subsequent audio delta should be raw PCM bytes, got %q", secondPayload)
	}
	final := chunks[2]
	if !final.IsLast || final.ID != "chatcmpl-audio-stream" {
		t.Fatalf("final response metadata mismatch: %#v", final)
	}
	if text := final.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("final transcript text not accumulated: %#v", final.Content)
	}
	finalPayload := audioPayload(t, final.Content[1])
	if !bytes.HasPrefix(finalPayload, []byte("RIFF")) || !bytes.Contains(finalPayload, []byte("abcdef")) {
		t.Fatalf("final audio should be a complete WAV wrapping accumulated PCM: %q", finalPayload)
	}
	if final.Usage.InputTokens != 3 || final.Usage.OutputTokens != 2 {
		t.Fatalf("final usage not attached: %#v", final.Usage)
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

type sequenceChatCompletionStream struct {
	chunks []sdk.ChatCompletionChunk
	index  int
	closed bool
}

func (s *sequenceChatCompletionStream) Next() bool {
	if s.index >= len(s.chunks) {
		return false
	}
	s.index++
	return true
}

func (s *sequenceChatCompletionStream) Current() sdk.ChatCompletionChunk {
	return s.chunks[s.index-1]
}

func (s *sequenceChatCompletionStream) Err() error {
	return nil
}

func (s *sequenceChatCompletionStream) Close() error {
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

func mustChunk(t *testing.T, raw string) sdk.ChatCompletionChunk {
	t.Helper()
	var chunk sdk.ChatCompletionChunk
	if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
		t.Fatalf("Unmarshal chunk returned error: %v", err)
	}
	return chunk
}

func audioPayload(t *testing.T, block message.ContentBlock) []byte {
	t.Helper()
	audio, ok := block.(*message.DataBlock)
	if !ok {
		t.Fatalf("expected DataBlock, got %T", block)
	}
	source, ok := audio.Source.(*message.Base64Source)
	if !ok {
		t.Fatalf("expected Base64Source, got %T", audio.Source)
	}
	if source.MediaType != "audio/wav" {
		t.Fatalf("expected audio/wav, got %q", source.MediaType)
	}
	payload, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	return payload
}
