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
	"errors"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
	astts "github.com/yuluo-yx/agentscope-go/tts"
)

func TestTTSMiddlewareSynthesizesAudioAfterTextBlock(t *testing.T) {
	t.Parallel()

	model := &recordingTTSModel{chunks: []astts.Response{
		*astts.NewResponse(
			message.NewDataBlock(message.NewBase64Source("QVVESU8=", "audio/wav")),
			true,
			astts.WithResponseID("tts-1"),
		),
	}}
	mw := NewTTSMiddleware(model)
	events, err := mw.OnReply(context.Background(), internalAgent{name: "Friday", state: statepkg.NewAgentState()}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 4)
		out <- message.NewTextBlockStartEvent("reply-1", "text-1")
		out <- message.NewTextBlockDeltaEvent("reply-1", "text-1", "hel")
		out <- message.NewTextBlockDeltaEvent("reply-1", "text-1", "lo")
		out <- message.NewTextBlockEndEvent("reply-1", "text-1")
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	got := collectEvents(events)
	if len(got) != 7 {
		t.Fatalf("expected original text events plus 3 audio events, got %d: %#v", len(got), got)
	}
	if model.synthesizeCalls != 1 || model.requests[0].Text != "hello" {
		t.Fatalf("text should be synthesized once after block end: %#v", model.requests)
	}
	if got[0].GetType() != message.TextBlockStartType || got[3].GetType() != message.TextBlockEndType {
		t.Fatalf("original text event order was not preserved: %#v", got[:4])
	}
	start, ok := got[4].(*message.DataBlockStartEvent)
	if !ok || start.ReplyID() != "reply-1" || start.MediaType != "audio/wav" || start.BlockID == "" {
		t.Fatalf("data start event mismatch: %#v", got[4])
	}
	delta, ok := got[5].(*message.DataBlockDeltaEvent)
	if !ok || delta.BlockID != start.BlockID || delta.Data != "QVVESU8=" || delta.MediaType != "audio/wav" {
		t.Fatalf("data delta event mismatch: %#v", got[5])
	}
	end, ok := got[6].(*message.DataBlockEndEvent)
	if !ok || end.BlockID != start.BlockID {
		t.Fatalf("data end event mismatch: %#v", got[6])
	}
}

func TestTTSMiddlewarePushesRealtimeTextAndClosesAudioBlock(t *testing.T) {
	t.Parallel()

	model := &recordingTTSModel{realtime: true}
	model.pushResponses = []*astts.Response{
		astts.NewResponse(message.NewDataBlock(message.NewBase64Source("QQ==", "audio/wav")), false),
	}
	model.chunks = []astts.Response{
		*astts.NewResponse(message.NewDataBlock(message.NewBase64Source("Qg==", "audio/wav")), true),
	}
	mw := NewTTSMiddleware(model)
	events, err := mw.OnReply(context.Background(), internalAgent{name: "Friday", state: statepkg.NewAgentState()}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 3)
		out <- message.NewTextBlockStartEvent("reply-1", "text-1")
		out <- message.NewTextBlockDeltaEvent("reply-1", "text-1", "A")
		out <- message.NewTextBlockEndEvent("reply-1", "text-1")
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	got := collectEvents(events)
	if model.connectCalls != 1 || model.closeCalls != 1 || len(model.pushedText) != 1 || model.pushedText[0] != "A" {
		t.Fatalf("realtime lifecycle mismatch: connects=%d closes=%d pushed=%#v", model.connectCalls, model.closeCalls, model.pushedText)
	}
	if model.synthesizeCalls != 1 || len(model.requests) != 1 || model.requests[0].Text != "" {
		t.Fatalf("realtime text end should flush with an empty request: %#v", model.requests)
	}
	if len(got) != 7 {
		t.Fatalf("expected text events plus realtime audio events, got %d: %#v", len(got), got)
	}
	if got[2].GetType() != message.DataBlockStartType || got[3].GetType() != message.DataBlockDeltaType || got[5].GetType() != message.DataBlockDeltaType || got[6].GetType() != message.DataBlockEndType {
		t.Fatalf("realtime audio event order mismatch: %#v", got)
	}
}

func TestTTSMiddlewarePassThroughAndErrorBranches(t *testing.T) {
	t.Parallel()

	if got := (*TTSMiddleware)(nil).MiddlewareName(); got != "tts" {
		t.Fatalf("nil middleware name mismatch: %q", got)
	}
	if got := NewTTSMiddleware(nil).MiddlewareName(); got != "tts" {
		t.Fatalf("middleware name mismatch: %q", got)
	}

	original := make(chan message.Event, 1)
	original <- message.NewTextBlockStartEvent("reply-1", "text-1")
	close(original)
	events, err := (*TTSMiddleware)(nil).OnReply(context.Background(), internalAgent{}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		return original, nil
	})
	if err != nil {
		t.Fatalf("nil middleware OnReply returned error: %v", err)
	}
	if got := collectEvents(events); len(got) != 1 || got[0].GetType() != message.TextBlockStartType {
		t.Fatalf("nil middleware should pass through events: %#v", got)
	}

	nextErr := errors.New("next failed")
	if _, err := NewTTSMiddleware(&recordingTTSModel{}).OnReply(context.Background(), internalAgent{}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		return nil, nextErr
	}); !errors.Is(err, nextErr) {
		t.Fatalf("next error mismatch: %v", err)
	}
	if _, err := NewTTSMiddleware(&recordingTTSModel{}).OnReply(context.Background(), internalAgent{}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		return nil, nil
	}); err == nil || !strings.Contains(err.Error(), "nil event stream") {
		t.Fatalf("nil stream error mismatch: %v", err)
	}

	synthErr := errors.New("synthesize failed")
	model := &recordingTTSModel{synthesizeErr: synthErr}
	events, err = NewTTSMiddleware(model).OnReply(context.Background(), internalAgent{}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 3)
		out <- message.NewTextBlockStartEvent("reply-1", "text-1")
		out <- message.NewTextBlockDeltaEvent("reply-1", "text-1", "hello")
		out <- message.NewTextBlockEndEvent("reply-1", "text-1")
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	if got := collectEvents(events); len(got) != 3 {
		t.Fatalf("synthesize error should keep original events only: %#v", got)
	}
	if model.synthesizeCalls != 1 {
		t.Fatalf("synthesize should be attempted once, got %d", model.synthesizeCalls)
	}
}

func TestTTSMiddlewareSkipsInvalidResponsesAndStopsOnRealtimeErrors(t *testing.T) {
	t.Parallel()

	model := &recordingTTSModel{chunks: []astts.Response{
		*astts.NewResponse(nil, false),
		*astts.NewResponse(message.NewDataBlock(message.NewURLSource("https://example.test/audio.wav", "audio/wav")), false),
		*astts.NewResponse(message.NewDataBlock(message.NewBase64Source("QQ==", "")), false),
		*astts.NewResponse(nil, true, astts.WithResponseError(errors.New("provider failed"))),
		*astts.NewResponse(message.NewDataBlock(message.NewBase64Source("Qg==", "audio/wav")), true),
	}}
	events, err := NewTTSMiddleware(model).OnReply(context.Background(), internalAgent{}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 3)
		out <- message.NewTextBlockStartEvent("reply-1", "text-1")
		out <- message.NewTextBlockDeltaEvent("reply-1", "text-1", "hello")
		out <- message.NewTextBlockEndEvent("reply-1", "text-1")
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	got := collectEvents(events)
	if len(got) != 6 {
		t.Fatalf("only one valid audio response should be emitted before provider error, got %#v", got)
	}
	start, ok := got[3].(*message.DataBlockStartEvent)
	if !ok || start.MediaType != "application/octet-stream" {
		t.Fatalf("empty media type should default on data start: %#v", got[3])
	}

	connectErr := errors.New("connect failed")
	realtimeConnect := &recordingTTSModel{realtime: true, connectErr: connectErr}
	events, err = NewTTSMiddleware(realtimeConnect).OnReply(context.Background(), internalAgent{}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 2)
		out <- message.NewTextBlockDeltaEvent("reply-1", "text-1", "A")
		out <- message.NewTextBlockEndEvent("reply-1", "text-1")
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply connect branch returned error: %v", err)
	}
	if got := collectEvents(events); len(got) != 2 || realtimeConnect.connectCalls != 1 || realtimeConnect.closeCalls != 1 || len(realtimeConnect.pushedText) != 0 {
		t.Fatalf("connect error should pass through only: events=%#v model=%#v", got, realtimeConnect)
	}

	pushErr := errors.New("push failed")
	realtimePush := &recordingTTSModel{realtime: true, pushErr: pushErr}
	events, err = NewTTSMiddleware(realtimePush).OnReply(context.Background(), internalAgent{}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 2)
		out <- message.NewTextBlockDeltaEvent("reply-1", "text-1", "A")
		out <- message.NewTextBlockEndEvent("reply-1", "text-1")
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply push branch returned error: %v", err)
	}
	if got := collectEvents(events); len(got) != 2 || realtimePush.synthesizeCalls != 0 {
		t.Fatalf("push error should stop further TTS handling: events=%#v synth=%d", got, realtimePush.synthesizeCalls)
	}
}

type recordingTTSModel struct {
	realtime        bool
	chunks          []astts.Response
	pushResponses   []*astts.Response
	connectErr      error
	pushErr         error
	synthesizeErr   error
	requests        []astts.Request
	pushedText      []string
	connectCalls    int
	closeCalls      int
	synthesizeCalls int
}

func (m *recordingTTSModel) Name() string {
	return "fake-tts"
}

func (m *recordingTTSModel) Realtime() bool {
	return m.realtime
}

func (m *recordingTTSModel) Connect(context.Context) error {
	m.connectCalls++
	return m.connectErr
}

func (m *recordingTTSModel) Close(context.Context) error {
	m.closeCalls++
	return nil
}

func (m *recordingTTSModel) Push(_ context.Context, text string) (*astts.Response, error) {
	m.pushedText = append(m.pushedText, text)
	if m.pushErr != nil {
		return nil, m.pushErr
	}
	if len(m.pushResponses) == 0 {
		return nil, nil
	}
	response := m.pushResponses[0]
	m.pushResponses = m.pushResponses[1:]
	return response, nil
}

func (m *recordingTTSModel) Synthesize(_ context.Context, request astts.Request) (<-chan astts.Response, error) {
	m.synthesizeCalls++
	m.requests = append(m.requests, request.Clone())
	if m.synthesizeErr != nil {
		return nil, m.synthesizeErr
	}
	out := make(chan astts.Response, len(m.chunks))
	for _, chunk := range m.chunks {
		out <- *chunk.Clone()
	}
	close(out)
	return out, nil
}
