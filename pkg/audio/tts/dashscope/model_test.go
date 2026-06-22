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
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/audio/tts"
	"github.com/yuluo-yx/agentscope-go/pkg/audio/tts/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
)

func TestModelSynthesizeStreamsDashScopeAudio(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services/aigc/multimodal-generation/generation" {
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
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"request_id\":\"req-1\",\"output\":{\"audio\":{\"data\":\"AQID\"}},\"usage\":{\"input_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("data: {\"request_id\":\"req-1\",\"output\":{\"audio\":{\"data\":\"BAU=\"}},\"usage\":{\"output_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model, err := dashscope.NewModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"qwen3-tts-flash",
		dashscope.WithParameters(dashscope.Parameters{Voice: "Cherry", SampleRate: 24000}),
		dashscope.WithStream(true),
	)
	if err != nil {
		t.Fatalf("NewModel returned error: %v", err)
	}
	chunks, err := model.Synthesize(context.Background(), tts.Request{
		Text:       "hello",
		Parameters: map[string]any{"speed": 1.1},
	})
	if err != nil {
		t.Fatalf("Synthesize returned error: %v", err)
	}
	got := collectTTSResponses(chunks)
	if len(got) != 2 {
		t.Fatalf("expected two streamed chunks, got %d: %#v", len(got), got)
	}
	if got[0].IsLast || !got[1].IsLast {
		t.Fatalf("stream chunk final flags mismatch: %#v", got)
	}
	if got[1].Usage == nil || got[1].Usage.InputTokens != 3 || got[1].Usage.OutputTokens != 5 || got[1].Usage.Type != tts.UsageTypeTTS {
		t.Fatalf("stream usage should be aggregated on final chunk: %#v", got[1].Usage)
	}
	firstSource := got[0].Content.Source.(*message.Base64Source)
	secondSource := got[1].Content.Source.(*message.Base64Source)
	if firstSource.MediaType != "audio/wav" || secondSource.MediaType != "audio/wav" {
		t.Fatalf("media type mismatch: %q %q", firstSource.MediaType, secondSource.MediaType)
	}
	firstBytes, err := base64.StdEncoding.DecodeString(firstSource.Data)
	if err != nil {
		t.Fatalf("first chunk should be base64: %v", err)
	}
	if len(firstBytes) != 47 || string(firstBytes[:4]) != "RIFF" || binary.LittleEndian.Uint32(firstBytes[24:28]) != 24000 {
		t.Fatalf("first stream chunk should carry WAV header plus PCM: len=%d header=%q", len(firstBytes), firstBytes[:12])
	}
	if payload := firstBytes[44:]; string(payload) != string([]byte{1, 2, 3}) {
		t.Fatalf("first chunk PCM mismatch: %#v", payload)
	}
	secondBytes, err := base64.StdEncoding.DecodeString(secondSource.Data)
	if err != nil {
		t.Fatalf("second chunk should be base64: %v", err)
	}
	if string(secondBytes) != string([]byte{4, 5}) {
		t.Fatalf("second chunk PCM mismatch: %#v", secondBytes)
	}

	body := <-requestCh
	parameters := body["parameters"].(map[string]any)
	input := body["input"].(map[string]any)
	if body["model"] != "qwen3-tts-flash" || body["stream"] != true || input["text"] != "hello" {
		t.Fatalf("request body mismatch: %#v", body)
	}
	if parameters["voice"] != "Cherry" || parameters["sample_rate"] != float64(24000) || parameters["speed"] != 1.1 || parameters["audio_format"] != "pcm" {
		t.Fatalf("request parameters mismatch: %#v", parameters)
	}
}

func TestListModelsLoadsPythonTTSModelCards(t *testing.T) {
	t.Parallel()

	cards, err := dashscope.ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected normal and realtime DashScope TTS model cards, got %#v", cards)
	}
	normal := findTTSCard(cards, "qwen3-tts-flash")
	realtime := findTTSCard(cards, "qwen3-tts-flash-realtime")
	if normal.Name == "" || normal.Realtime {
		t.Fatalf("normal TTS card mismatch: %#v", normal)
	}
	if realtime.Name == "" || !realtime.Realtime {
		t.Fatalf("realtime TTS card mismatch: %#v", realtime)
	}
	voice := normal.ParameterSchema["properties"].(map[string]any)["voice"].(map[string]any)
	if voice["default"] != "Cherry" || len(voice["enum"].([]any)) != 4 {
		t.Fatalf("TTS voices should be copied from Python YAML: %#v", voice)
	}
}

func TestModelSynthesizeAggregatesNonStreamingAudioAndProviderErrors(t *testing.T) {
	t.Parallel()

	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= http.StatusBadRequest {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "InvalidInput", "message": "bad input"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id": "req-aggregate",
			"output":     map[string]any{"audio": map[string]any{"data": "AQID"}},
			"usage":      map[string]any{"input_tokens": 2, "output_tokens": 4},
		})
	}))
	defer server.Close()

	model, err := dashscope.NewModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"qwen3-tts-flash",
		dashscope.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewModel returned error: %v", err)
	}
	chunks, err := model.Synthesize(context.Background(), tts.Request{Text: "hello"})
	if err != nil {
		t.Fatalf("Synthesize returned error: %v", err)
	}
	got := collectTTSResponses(chunks)
	if len(got) != 1 || !got[0].IsLast {
		t.Fatalf("expected one final aggregated response, got %#v", got)
	}
	data, err := base64.StdEncoding.DecodeString(got[0].Content.Source.(*message.Base64Source).Data)
	if err != nil {
		t.Fatalf("aggregated audio should be base64: %v", err)
	}
	if len(data) != 47 || string(data[:4]) != "RIFF" || string(data[44:]) != string([]byte{1, 2, 3}) {
		t.Fatalf("aggregated audio should be full WAV: len=%d data=%#v", len(data), data)
	}

	status = http.StatusBadRequest
	_, err = model.Synthesize(context.Background(), tts.Request{Text: "bad"})
	if err == nil {
		t.Fatal("Synthesize should return provider error")
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Provider != "dashscope" || providerErr.Code != "InvalidInput" || providerErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("provider error metadata mismatch: %#v err=%v", providerErr, err)
	}
}

func collectTTSResponses(responses <-chan tts.Response) []tts.Response {
	collected := []tts.Response{}
	for response := range responses {
		collected = append(collected, response)
	}
	return collected
}

func findTTSCard(cards []tts.ModelCard, name string) tts.ModelCard {
	for _, card := range cards {
		if card.Name == name {
			return card
		}
	}
	return tts.ModelCard{}
}
