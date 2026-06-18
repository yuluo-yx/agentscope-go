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

package dashscope_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/audio/stt"
	"github.com/yuluo-yx/agentscope-go/audio/stt/dashscope"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestModelRecognizeDashScopeAudio(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services/audio/asr/transcription" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer dash-key" {
			t.Fatalf("Authorization header mismatch: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type header mismatch: %q", r.Header.Get("Content-Type"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		requestCh <- body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id": "req-1",
			"output": map[string]any{
				"text":     "hello world",
				"language": "en",
				"segments": []any{
					map[string]any{"text": "hello", "start_time_ms": 1000, "end_time_ms": 1500},
					map[string]any{"text": "world", "start_time_ms": 1500, "end_time_ms": 2200},
				},
			},
			"usage": map[string]any{
				"input_tokens":      3,
				"output_tokens":     5,
				"audio_duration_ms": 2200,
			},
		})
	}))
	defer server.Close()

	model, err := dashscope.NewModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"paraformer-v2",
		dashscope.WithParameters(dashscope.Parameters{
			Language:   "en",
			SampleRate: 16000,
			Extra:      map[string]any{"diarization": true},
		}),
	)
	if err != nil {
		t.Fatalf("NewModel returned error: %v", err)
	}
	chunks, err := model.Recognize(context.Background(), stt.Request{
		Audio:      stt.NewAudioBlock([]byte("raw"), "audio/wav"),
		Parameters: map[string]any{"language": "zh", "punctuation": true},
	})
	if err != nil {
		t.Fatalf("Recognize returned error: %v", err)
	}
	got := collectSTTResponses(chunks)
	if len(got) != 1 || !got[0].IsLast {
		t.Fatalf("expected one final response, got %#v", got)
	}
	if got[0].Text != "hello world" || got[0].Language != "en" || len(got[0].Segments) != 2 {
		t.Fatalf("recognized text mismatch: %#v", got[0])
	}
	if got[0].Segments[0].Start != time.Second || got[0].Segments[1].End != 2200*time.Millisecond {
		t.Fatalf("segment timing mismatch: %#v", got[0].Segments)
	}
	if got[0].Usage == nil || got[0].Usage.InputTokens != 3 || got[0].Usage.OutputTokens != 5 ||
		got[0].Usage.AudioDuration != 2200*time.Millisecond || got[0].Usage.Type != stt.UsageTypeSTT {
		t.Fatalf("usage mismatch: %#v", got[0].Usage)
	}
	if got[0].Metadata["request_id"] != "req-1" || got[0].Metadata["provider"] != "dashscope" {
		t.Fatalf("metadata mismatch: %#v", got[0].Metadata)
	}

	body := <-requestCh
	parameters := body["parameters"].(map[string]any)
	input := body["input"].(map[string]any)
	audio := input["audio"].(map[string]any)
	if body["model"] != "paraformer-v2" {
		t.Fatalf("model mismatch: %#v", body)
	}
	if audio["data"] != base64.StdEncoding.EncodeToString([]byte("raw")) || audio["media_type"] != "audio/wav" {
		t.Fatalf("audio payload mismatch: %#v", audio)
	}
	if parameters["language"] != "zh" || parameters["sample_rate"] != float64(16000) ||
		parameters["diarization"] != true || parameters["punctuation"] != true {
		t.Fatalf("request parameters mismatch: %#v", parameters)
	}
}

func TestModelRecognizeURLAudioAndProviderErrors(t *testing.T) {
	t.Parallel()

	status := http.StatusOK
	requestCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		requestCh <- body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= http.StatusBadRequest {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "InvalidInput", "message": "bad audio"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id": "req-url",
			"output":     map[string]any{"text": "from url"},
		})
	}))
	defer server.Close()

	model, err := dashscope.NewModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"paraformer-v2",
	)
	if err != nil {
		t.Fatalf("NewModel returned error: %v", err)
	}
	chunks, err := model.Recognize(context.Background(), stt.Request{
		Audio: message.NewDataBlock(message.NewURLSource("https://example.test/audio.wav", "audio/wav")),
	})
	if err != nil {
		t.Fatalf("Recognize URL audio returned error: %v", err)
	}
	got := collectSTTResponses(chunks)
	if len(got) != 1 || got[0].Text != "from url" {
		t.Fatalf("URL audio response mismatch: %#v", got)
	}
	body := <-requestCh
	audio := body["input"].(map[string]any)["audio"].(map[string]any)
	if audio["url"] != "https://example.test/audio.wav" || audio["media_type"] != "audio/wav" {
		t.Fatalf("URL audio payload mismatch: %#v", audio)
	}

	status = http.StatusBadRequest
	_, err = model.Recognize(context.Background(), stt.Request{
		Audio: stt.NewAudioBlock([]byte("bad"), "audio/wav"),
	})
	if err == nil {
		t.Fatal("Recognize should return provider error")
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Provider != "dashscope" ||
		providerErr.Code != "InvalidInput" || providerErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("provider error metadata mismatch: %#v err=%v", providerErr, err)
	}
}

func TestModelMetadataNoopRealtimeAndValidation(t *testing.T) {
	t.Parallel()

	model, err := dashscope.NewModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(" https://dashscope.example.com/ ")),
		"paraformer-v2",
		dashscope.WithHTTPClient(http.DefaultClient),
		dashscope.WithParameters(dashscope.Parameters{
			Extra: map[string]any{"language": "ignored", "format": "wav"},
		}),
	)
	if err != nil {
		t.Fatalf("NewModel returned error: %v", err)
	}
	if model.Name() != "dashscope:paraformer-v2" || (*dashscope.Model)(nil).Name() != "dashscope:<nil>" {
		t.Fatalf("Name mismatch: %q", model.Name())
	}
	if model.Realtime() || (*dashscope.Model)(nil).Realtime() {
		t.Fatalf("HTTP model should not report realtime support")
	}
	if err := model.Connect(context.Background()); err != nil {
		t.Fatalf("Connect no-op returned error: %v", err)
	}
	if err := model.Close(context.Background()); err != nil {
		t.Fatalf("Close no-op returned error: %v", err)
	}
	if _, err := model.Push(context.Background(), stt.NewAudioBlock([]byte("raw"), "audio/wav")); err == nil {
		t.Fatalf("Push should reject non-realtime model")
	}
	if _, err := model.Recognize(context.Background(), stt.Request{}); err == nil {
		t.Fatalf("Recognize should reject empty audio")
	}

	invalidCases := []struct {
		name string
		fn   func() error
	}{
		{name: "empty api key", fn: func() error { _, err := dashscope.NewModel(dashscope.NewCredential(""), "m"); return err }},
		{name: "empty base url", fn: func() error {
			_, err := dashscope.NewModel(dashscope.NewCredential("k", dashscope.WithBaseURL(" ")), "m")
			return err
		}},
		{name: "empty model", fn: func() error { _, err := dashscope.NewModel(dashscope.NewCredential("k"), " "); return err }},
		{name: "nil model recognize", fn: func() error {
			_, err := (*dashscope.Model)(nil).Recognize(context.Background(), stt.Request{Audio: stt.NewAudioBlock([]byte("x"), "audio/wav")})
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

func TestListModelsLoadsDashScopeSTTModelCards(t *testing.T) {
	t.Parallel()

	cards, err := dashscope.ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	card := findSTTCard(cards, "paraformer-v2")
	if card.Name == "" || card.Realtime || card.InputTypes[0] != "audio/wav" || card.OutputTypes[0] != "text/plain" {
		t.Fatalf("DashScope STT card mismatch: %#v", card)
	}
	if _, ok := card.ParameterSchema["properties"].(map[string]any)["language"]; !ok {
		t.Fatalf("language schema should be present: %#v", card.ParameterSchema)
	}
}

func collectSTTResponses(responses <-chan stt.Response) []stt.Response {
	collected := []stt.Response{}
	for response := range responses {
		collected = append(collected, response)
	}
	return collected
}

func findSTTCard(cards []stt.ModelCard, name string) stt.ModelCard {
	for _, card := range cards {
		if card.Name == name {
			return card
		}
	}
	return stt.ModelCard{}
}
