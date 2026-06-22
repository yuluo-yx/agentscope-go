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

package dashscope

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/audio/tts"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
)

func TestModelMetadataNoopRealtimeAndEmptySynthesisBranches(t *testing.T) {
	t.Parallel()

	model, err := NewModel(
		NewCredential("dash-key", WithBaseURL(" https://dashscope.example.com/ ")),
		"qwen3-tts-flash",
		WithHTTPClient(http.DefaultClient),
		WithParameters(Parameters{
			Extra: map[string]any{"voice": "ignored", "pitch": 1.2},
		}),
	)
	if err != nil {
		t.Fatalf("NewModel returned error: %v", err)
	}
	if model.Name() != "dashscope:qwen3-tts-flash" || (*Model)(nil).Name() != "dashscope:<nil>" {
		t.Fatalf("Name mismatch: %q", model.Name())
	}
	if model.Realtime() || (*Model)(nil).Realtime() {
		t.Fatalf("HTTP model should not report realtime support")
	}
	if err := model.Connect(context.Background()); err != nil {
		t.Fatalf("Connect no-op returned error: %v", err)
	}
	if err := model.Close(context.Background()); err != nil {
		t.Fatalf("Close no-op returned error: %v", err)
	}
	if _, err := model.Push(context.Background(), "hello"); err == nil {
		t.Fatalf("Push should reject non-realtime model")
	}
	responses, err := model.Synthesize(context.Background(), tts.Request{Text: " \t\n "})
	if err != nil {
		t.Fatalf("empty Synthesize returned error: %v", err)
	}
	got := collectTTSResponses(responses)
	if len(got) != 1 || !got[0].IsLast || got[0].Content != nil {
		t.Fatalf("empty synthesis response mismatch: %#v", got)
	}
	params := model.parametersForRequest(map[string]any{"speed": 1.1, "pitch": 0.9})
	if params["voice"] != defaultVoice || params["audio_format"] != defaultAudioFormat || params["speed"] != 1.1 || params["pitch"] != 0.9 {
		t.Fatalf("parameters should merge defaults, extra, and request overrides: %#v", params)
	}

	invalidCases := []struct {
		name string
		fn   func() error
	}{
		{name: "empty api key", fn: func() error { _, err := NewModel(NewCredential(""), "m"); return err }},
		{name: "empty base url", fn: func() error { _, err := NewModel(NewCredential("k", WithBaseURL(" ")), "m"); return err }},
		{name: "empty model", fn: func() error { _, err := NewModel(NewCredential("k"), " "); return err }},
		{name: "nil model synthesize", fn: func() error {
			_, err := (*Model)(nil).Synthesize(context.Background(), tts.Request{Text: "x"})
			return err
		}},
	}
	for _, tt := range invalidCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.fn(); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestParsingAggregationAndProviderErrorBranches(t *testing.T) {
	t.Parallel()

	chunks, err := parseDashScopeChunks(strings.NewReader(`
{"request_id":"req-jsonl","output":{"audio":{"data":"AQI="}},"usage":{"prompt_tokens":2}}

{"request_id":"req-jsonl","output":{"audio":{"data":"AwQ="}},"usage":{"completion_tokens":3}}
`), "application/x-ndjson")
	if err != nil {
		t.Fatalf("parseDashScopeChunks json lines returned error: %v", err)
	}
	if len(chunks) != 2 || chunks[0].RequestID != "req-jsonl" {
		t.Fatalf("json lines chunks mismatch: %#v", chunks)
	}
	if empty, err := parseDashScopeChunks(strings.NewReader(" \n "), "application/json"); err != nil || empty != nil {
		t.Fatalf("empty body should return nil chunks, got %#v err=%v", empty, err)
	}
	if _, err := parseDashScopeChunks(strings.NewReader("{bad json}\n"), "application/x-ndjson"); err == nil {
		t.Fatalf("invalid json lines should fail")
	}

	model := &Model{stream: false, parameters: defaultParameters()}
	responses, err := model.responsesFromChunks(chunks, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("responsesFromChunks non-stream returned error: %v", err)
	}
	if len(responses) != 1 || !responses[0].IsLast || responses[0].Usage.InputTokens != 2 || responses[0].Usage.OutputTokens != 3 {
		t.Fatalf("non-stream aggregated response mismatch: %#v", responses)
	}
	if _, err := (&Model{stream: true, parameters: defaultParameters()}).responsesFromChunks([]dashScopeChunk{{Output: dashScopeAudio{Audio: struct {
		Data string `json:"data"`
	}{Data: "not-base64"}}}}, time.Millisecond); err == nil {
		t.Fatalf("streaming invalid empty audio chunk should fail base64 decode")
	}
	if _, err := (&Model{stream: false, parameters: defaultParameters()}).responsesFromChunks([]dashScopeChunk{{Output: dashScopeAudio{Audio: struct {
		Data string `json:"data"`
	}{Data: "not-base64"}}}}, time.Millisecond); err == nil {
		t.Fatalf("non-stream invalid base64 should fail")
	}
	emptyResponses, err := model.responsesFromChunks(nil, time.Millisecond)
	if err != nil || len(emptyResponses) != 1 || !emptyResponses[0].IsLast {
		t.Fatalf("empty chunks response mismatch: %#v err=%v", emptyResponses, err)
	}

	resp := &http.Response{
		StatusCode: http.StatusTeapot,
		Status:     "418 I'm a teapot",
		Body:       ioNopCloser{bytes.NewBufferString(`{"code":"BadTea","message":"short and stout"}`)},
	}
	err = providerError(resp)
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != "BadTea" || providerErr.Message != "short and stout" {
		t.Fatalf("providerError decoded metadata mismatch: %#v err=%v", providerErr, err)
	}
}

func TestRealtimeErrorAndNoopBranches(t *testing.T) {
	t.Parallel()

	cards, err := ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("ListModels should load embedded DashScope TTS model cards")
	}

	model, err := NewRealtimeModel(
		NewCredential("dash-key"),
		" qwen3-tts-flash-realtime ",
		WithRealtimeDialer(nil),
		WithRealtimeEndpoint("ftp://example.com/realtime"),
		WithRealtimeConnectTimeout(-time.Second),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel returned error: %v", err)
	}
	if model.dialer == nil || model.connectTimeout != defaultConnectTimeout || model.Name() != "dashscope:qwen3-tts-flash-realtime" {
		t.Fatalf("realtime defaults mismatch: dialer=%#v timeout=%v name=%s", model.dialer, model.connectTimeout, model.Name())
	}
	if _, err := model.realtimeURL(); err == nil {
		t.Fatal("unsupported realtime endpoint scheme should fail")
	}
	if response, err := model.Push(context.Background(), ""); err != nil || response == nil || response.IsLast {
		t.Fatalf("empty Push should return non-terminal empty response, response=%#v err=%v", response, err)
	}
	if _, err := model.Push(context.Background(), "hello"); err == nil {
		t.Fatal("Push with text should fail when websocket is not connected")
	}
	if _, err := model.Synthesize(context.Background(), tts.Request{}); err == nil {
		t.Fatal("Synthesize should fail when websocket is not connected")
	}
	if _, err := (*RealtimeModel)(nil).Push(context.Background(), "hello"); err == nil {
		t.Fatal("nil realtime Push should fail")
	}
	if _, err := (*RealtimeModel)(nil).Synthesize(context.Background(), tts.Request{}); err == nil {
		t.Fatal("nil realtime Synthesize should fail")
	}

	state := &RealtimeModel{
		parameters:  defaultParameters(),
		audioSignal: make(chan struct{}, 1),
	}
	if err := state.handleRealtimeEvent([]byte("{")); err == nil {
		t.Fatal("invalid realtime event JSON should fail")
	}
	if err := state.handleRealtimeEvent([]byte(`{"type":"session.created","session":{"id":"sess-1"}}`)); err != nil {
		t.Fatalf("session.created event failed: %v", err)
	}
	if state.sessionID != "sess-1" {
		t.Fatalf("session id mismatch: %q", state.sessionID)
	}
	if err := state.handleRealtimeEvent([]byte(`{"type":"response.created","response":{"id":"resp-1"}}`)); err != nil {
		t.Fatalf("response.created event failed: %v", err)
	}
	if err := state.handleRealtimeEvent([]byte(`{"type":"response.audio.delta"}`)); err != nil {
		t.Fatalf("empty audio delta should be ignored: %v", err)
	}
	if err := state.handleRealtimeEvent([]byte(`{"type":"response.audio.delta","delta":"bad"}`)); err != nil {
		t.Fatalf("bad audio delta records read error but does not return it: %v", err)
	}
	errorResponse := state.takeAudioResponseLocked(true)
	if errorResponse.Error == nil || !strings.Contains(errorResponse.Error.Error(), "decode audio delta") {
		t.Fatalf("bad delta should surface as terminal audio error response: %#v", errorResponse)
	}
	state.resetAudio()
	if err := state.handleRealtimeEvent([]byte(`{"type":"error","error":{"code":"Nested","message":"nested error"}}`)); err != nil {
		t.Fatalf("nested provider error event failed: %v", err)
	}
	if state.readErr == nil || !strings.Contains(state.readErr.Error(), "nested error") {
		t.Fatalf("nested provider error not captured: %v", state.readErr)
	}
	if isClosedWebSocketError(nil) || !isClosedWebSocketError(errors.New("websocket: close 1000 normal")) {
		t.Fatal("closed websocket error detection mismatch")
	}
}

func collectTTSResponses(responses <-chan tts.Response) []tts.Response {
	out := []tts.Response{}
	for response := range responses {
		out = append(out, response)
	}
	return out
}

type ioNopCloser struct {
	*bytes.Buffer
}

func (c ioNopCloser) Close() error { return nil }
