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
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/yuluo-yx/agentscope-go/audio/stt"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestRealtimeModelOptionsURLAndValidation(t *testing.T) {
	t.Parallel()

	dialer := &websocket.Dialer{}
	model, err := NewRealtimeModel(
		NewCredential("dash-key", WithBaseURL("https://dashscope.example.com/")),
		" qwen-realtime ",
		WithRealtimeEndpoint(" http://localhost/realtime "),
		WithRealtimeDialer(dialer),
		WithRealtimeConnectTimeout(123*time.Millisecond),
		WithRealtimeWorkspace(" workspace-1 "),
		WithRealtimeDataInspection(" disable "),
		WithRealtimeParameters(RealtimeParameters{
			Language:           " zh ",
			InputAudioFormat:   " opus ",
			SampleRate:         8000,
			Mode:               RealtimeModeManual,
			VADThreshold:       0.25,
			VADSilenceDuration: 900 * time.Millisecond,
			Extra:              map[string]any{"custom": []any{"value"}},
		}),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel returned error: %v", err)
	}
	if model.Name() != "dashscope:qwen-realtime" || !model.Realtime() || (*RealtimeModel)(nil).Name() != "dashscope:<nil>" {
		t.Fatalf("realtime model metadata mismatch: name=%s realtime=%v", model.Name(), model.Realtime())
	}
	if model.endpoint != "http://localhost/realtime" || model.dialer != dialer || model.connectTimeout != 123*time.Millisecond ||
		model.workspace != "workspace-1" || model.dataInspection != "disable" {
		t.Fatalf("realtime options mismatch: %#v", model)
	}
	if model.parameters.Language != "zh" || model.parameters.InputAudioFormat != "opus" || model.parameters.SampleRate != 8000 ||
		model.parameters.Mode != RealtimeModeManual || model.parameters.Extra["custom"] == nil {
		t.Fatalf("realtime parameters mismatch: %#v", model.parameters)
	}
	realtimeURL, err := model.realtimeURL()
	if err != nil {
		t.Fatalf("realtimeURL returned error: %v", err)
	}
	if realtimeURL != "ws://localhost/realtime?model=qwen-realtime" {
		t.Fatalf("realtimeURL mismatch: %s", realtimeURL)
	}

	defaultDialerModel, err := NewRealtimeModel(
		NewCredential("dash-key", WithBaseURL("http://dashscope.example.com/base/")),
		"model",
		WithRealtimeDialer(nil),
		WithRealtimeConnectTimeout(-time.Second),
	)
	if err != nil {
		t.Fatalf("NewRealtimeModel default dialer returned error: %v", err)
	}
	if defaultDialerModel.dialer == nil || defaultDialerModel.connectTimeout != defaultConnectTimeout {
		t.Fatalf("nil dialer/non-positive timeout should fall back to defaults")
	}
	defaultURL, err := defaultDialerModel.realtimeURL()
	if err != nil {
		t.Fatalf("default realtimeURL returned error: %v", err)
	}
	if defaultURL != "ws://dashscope.example.com/base/api-ws/v1/realtime?model=model" {
		t.Fatalf("default realtimeURL mismatch: %s", defaultURL)
	}

	for name, fn := range map[string]func() error{
		"nil model recognize": func() error {
			_, err := (*RealtimeModel)(nil).Recognize(context.Background(), stt.Request{Audio: stt.NewAudioBlock([]byte("x"), "audio/pcm")})
			return err
		},
		"nil model session": func() error {
			_, err := (*RealtimeModel)(nil).NewSession(context.Background(), stt.SessionRequest{})
			return err
		},
		"missing audio": func() error {
			_, err := model.Recognize(context.Background(), stt.Request{})
			return err
		},
		"empty model": func() error {
			_, err := NewRealtimeModel(NewCredential("dash-key"), " ")
			return err
		},
		"bad endpoint": func() error {
			bad := *model
			bad.endpoint = "ftp://localhost/realtime"
			_, err := bad.realtimeURL()
			return err
		},
		"invalid endpoint": func() error {
			bad := *model
			bad.endpoint = "http://[::1"
			_, err := bad.realtimeURL()
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := fn(); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestRealtimeParameterExtractionValidationAndAudio(t *testing.T) {
	t.Parallel()

	base := RealtimeParameters{
		Language:           "en",
		InputAudioFormat:   "pcm",
		SampleRate:         16000,
		Mode:               RealtimeModeVAD,
		VADThreshold:       0.1,
		VADSilenceDuration: 400 * time.Millisecond,
		Extra:              map[string]any{"base": map[string]any{"keep": true}},
	}
	request := stt.SessionRequest{Parameters: map[string]any{
		"language":            " zh ",
		"input_audio_format":  "opus",
		"sample_rate":         json.Number("8000"),
		"mode":                "manual",
		"vad_threshold":       json.Number("0.5"),
		"silence_duration_ms": float64(750),
		"custom":              map[string]any{"nested": "value"},
	}}
	parameters := realtimeParametersForSession(base, request)
	if parameters.Language != "zh" || parameters.InputAudioFormat != "opus" || parameters.SampleRate != 8000 ||
		parameters.Mode != RealtimeModeManual || parameters.VADThreshold != 0.5 ||
		parameters.VADSilenceDuration != 750*time.Millisecond {
		t.Fatalf("session parameters mismatch: %#v", parameters)
	}
	if parameters.Extra["custom"].(map[string]any)["nested"] != "value" || parameters.Extra["base"].(map[string]any)["keep"] != true {
		t.Fatalf("session extra parameters mismatch: %#v", parameters.Extra)
	}

	if got, ok := stringRealtimeParameter(" value "); !ok || got != "value" {
		t.Fatalf("stringRealtimeParameter string = %q, %v", got, ok)
	}
	if _, ok := stringRealtimeParameter(1); ok {
		t.Fatalf("stringRealtimeParameter should reject non-strings")
	}
	for _, input := range []any{int(8), int64(8), float64(8), json.Number("8"), json.Number("8.5")} {
		if got, ok := intRealtimeParameter(input); !ok || got != 8 {
			t.Fatalf("intRealtimeParameter(%#v) = %d, %v", input, got, ok)
		}
	}
	if _, ok := intRealtimeParameter(json.Number("bad")); ok {
		t.Fatalf("intRealtimeParameter should reject invalid json.Number")
	}
	if _, ok := intRealtimeParameter(true); ok {
		t.Fatalf("intRealtimeParameter should reject unsupported types")
	}
	for _, input := range []any{float64(0.75), float32(0.75), int(1), int64(1), json.Number("0.75")} {
		if _, ok := floatRealtimeParameter(input); !ok {
			t.Fatalf("floatRealtimeParameter(%#v) should parse", input)
		}
	}
	if _, ok := floatRealtimeParameter(json.Number("bad")); ok {
		t.Fatalf("floatRealtimeParameter should reject invalid json.Number")
	}
	if _, ok := floatRealtimeParameter(true); ok {
		t.Fatalf("floatRealtimeParameter should reject unsupported types")
	}

	for name, parameters := range map[string]RealtimeParameters{
		"bad format":    {InputAudioFormat: "wav", SampleRate: 16000, Mode: RealtimeModeVAD, VADSilenceDuration: 400 * time.Millisecond},
		"bad rate":      {InputAudioFormat: "pcm", SampleRate: 44100, Mode: RealtimeModeVAD, VADSilenceDuration: 400 * time.Millisecond},
		"bad mode":      {InputAudioFormat: "pcm", SampleRate: 16000, Mode: "auto", VADSilenceDuration: 400 * time.Millisecond},
		"bad threshold": {InputAudioFormat: "pcm", SampleRate: 16000, Mode: RealtimeModeVAD, VADThreshold: 2, VADSilenceDuration: 400 * time.Millisecond},
		"bad silence":   {InputAudioFormat: "pcm", SampleRate: 16000, Mode: RealtimeModeVAD, VADSilenceDuration: 100 * time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validateRealtimeParameters(parameters); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}

	validAudio := base64.StdEncoding.EncodeToString([]byte("pcm"))
	if got, err := realtimeAudioBase64(message.NewDataBlock(message.NewBase64Source(validAudio, "audio/pcm"))); err != nil || got != validAudio {
		t.Fatalf("realtimeAudioBase64 valid = %q, %v", got, err)
	}
	for name, audio := range map[string]*message.DataBlock{
		"nil":     nil,
		"url":     message.NewDataBlock(message.NewURLSource("https://example.test/audio.wav", "audio/wav")),
		"empty":   message.NewDataBlock(message.NewBase64Source(" ", "audio/pcm")),
		"bad b64": message.NewDataBlock(message.NewBase64Source("not base64", "audio/pcm")),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := realtimeAudioBase64(audio); err == nil {
				t.Fatalf("expected audio error")
			}
		})
	}
}

func TestRealtimeSessionEventsAndClose(t *testing.T) {
	t.Parallel()

	var nilSession *realtimeSession
	if nilSession.ID() != "" {
		t.Fatalf("nil session ID should be empty")
	}
	if _, ok := <-nilSession.Responses(); ok {
		t.Fatalf("nil session responses should be closed")
	}
	if err := nilSession.Push(context.Background(), stt.NewAudioBlock([]byte("x"), "audio/pcm")); err == nil || !strings.Contains(err.Error(), "nil session") {
		t.Fatalf("nil Push error = %v", err)
	}
	if err := nilSession.Commit(context.Background()); err == nil || !strings.Contains(err.Error(), "nil session") {
		t.Fatalf("nil Commit error = %v", err)
	}
	if err := nilSession.Finish(context.Background()); err == nil || !strings.Contains(err.Error(), "nil session") {
		t.Fatalf("nil Finish error = %v", err)
	}
	if err := nilSession.Close(context.Background()); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}

	session := newRealtimeSession(nil, RealtimeParameters{
		Language:           "zh",
		InputAudioFormat:   "pcm",
		SampleRate:         16000,
		Mode:               RealtimeModeVAD,
		VADThreshold:       0.2,
		VADSilenceDuration: 500 * time.Millisecond,
		Extra: map[string]any{
			"custom":      map[string]any{"nested": "value"},
			"sample_rate": 999,
		},
	}, map[string]any{"trace_id": "trace-1"})
	payload := session.sessionPayload()
	if payload["sample_rate"] != 16000 || payload["custom"].(map[string]any)["nested"] != "value" {
		t.Fatalf("session payload should keep built-ins and copy extras: %#v", payload)
	}
	turnDetection := payload["turn_detection"].(map[string]any)
	if turnDetection["type"] != "server_vad" || turnDetection["threshold"] != 0.2 || turnDetection["silence_duration_ms"] != 500 {
		t.Fatalf("turn detection payload mismatch: %#v", turnDetection)
	}
	if err := session.Push(context.Background(), stt.NewAudioBlock([]byte("pcm"), "audio/pcm")); err == nil || !strings.Contains(err.Error(), "websocket is not connected") {
		t.Fatalf("Push without conn error = %v", err)
	}
	if err := session.Commit(context.Background()); err == nil || !strings.Contains(err.Error(), "websocket is not connected") {
		t.Fatalf("Commit without conn error = %v", err)
	}
	if err := session.Finish(context.Background()); err == nil || !strings.Contains(err.Error(), "websocket is not connected") {
		t.Fatalf("Finish without conn error = %v", err)
	}
	if err := session.handleEvent([]byte(`{"type":"session.updated","session":{"id":"sess-1"}}`)); err != nil {
		t.Fatalf("handle session.updated returned error: %v", err)
	}
	if session.ID() != "sess-1" {
		t.Fatalf("session ID mismatch: %q", session.ID())
	}
	if err := session.handleEvent([]byte(`{"type":"conversation.item.input_audio_transcription.text","event_id":"evt-1","item_id":"item-1","content_index":2,"language":"zh","emotion":"happy","text":"你","stash":"好"}`)); err != nil {
		t.Fatalf("handle partial text returned error: %v", err)
	}
	partial := receiveSTTResponse(t, session.Responses())
	if partial.Text != "你好" || partial.IsLast || partial.Language != "zh" || partial.Metadata["event_id"] != "evt-1" ||
		partial.Metadata["session_id"] != "sess-1" || partial.Metadata["emotion"] != "happy" {
		t.Fatalf("partial response mismatch: %#v", partial)
	}
	if err := session.handleEvent([]byte(`{"type":"conversation.item.input_audio_transcription.completed","event_id":"evt-2","item_id":"item-1","content_index":2,"language":"zh","transcript":"你好"}`)); err != nil {
		t.Fatalf("handle completed returned error: %v", err)
	}
	final := receiveSTTResponse(t, session.Responses())
	if final.Text != "你好" || !final.IsLast || final.Metadata["event_type"] != "conversation.item.input_audio_transcription.completed" {
		t.Fatalf("final response mismatch: %#v", final)
	}
	if err := session.handleEvent([]byte(`{bad json`)); err == nil {
		t.Fatalf("invalid event JSON should fail")
	}

	failedSession := newRealtimeSession(nil, defaultRealtimeParameters(), nil)
	if err := failedSession.handleEvent([]byte(`{"type":"conversation.item.input_audio_transcription.failed","error":{"code":"InvalidAudio","message":"bad audio","param":"audio"}}`)); err != nil {
		t.Fatalf("handle failed transcription returned error: %v", err)
	}
	failed := receiveSTTResponse(t, failedSession.Responses())
	if failed.Error == nil || !failed.IsLast || failed.Metadata["provider_error_param"] != "audio" {
		t.Fatalf("failed response mismatch: %#v", failed)
	}

	errorSession := newRealtimeSession(nil, defaultRealtimeParameters(), nil)
	if err := errorSession.handleEvent([]byte(`{"type":"error","code":"BadRequest","message":"bad request"}`)); err != nil {
		t.Fatalf("handle error event returned error: %v", err)
	}
	errorResponse := receiveSTTResponse(t, errorSession.Responses())
	var providerErr *asmodel.ProviderError
	if errorResponse.Error == nil || !errors.As(errorResponse.Error, &providerErr) || providerErr.Code != "BadRequest" {
		t.Fatalf("error response mismatch: %#v", errorResponse)
	}
	assertSTTResponsesClosed(t, errorSession.Responses())

	finishedSession := newRealtimeSession(nil, defaultRealtimeParameters(), nil)
	if err := finishedSession.handleEvent([]byte(`{"type":"session.finished"}`)); err != nil {
		t.Fatalf("handle session.finished returned error: %v", err)
	}
	assertSTTResponsesClosed(t, finishedSession.Responses())

	eventErr := providerSTTRealtimeEventError(realtimeSTTEvent{Code: "Top", Message: "top-level"})
	if !strings.Contains(eventErr.Error(), "top-level") {
		t.Fatalf("providerSTTRealtimeEventError mismatch: %v", eventErr)
	}
	if isClosedWebSocketError(nil) || !isClosedWebSocketError(errors.New("websocket: close 1000 normal")) ||
		!isClosedWebSocketError(errors.New("use of closed network connection")) ||
		isClosedWebSocketError(errors.New("other")) {
		t.Fatalf("closed websocket error detection mismatch")
	}
}

func receiveSTTResponse(t *testing.T, ch <-chan stt.Response) stt.Response {
	t.Helper()
	select {
	case response, ok := <-ch:
		if !ok {
			t.Fatal("response channel closed unexpectedly")
		}
		return response
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for realtime response")
		return stt.Response{}
	}
}

func assertSTTResponsesClosed(t *testing.T, ch <-chan stt.Response) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("response channel should be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response channel close")
	}
}
