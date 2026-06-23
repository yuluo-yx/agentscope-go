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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/audio/stt"
	"github.com/yuluo-yx/agentscope-go/pkg/audio/stt/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
)

func TestModelRecognizeRejectsInlineAudio(t *testing.T) {
	t.Parallel()

	model, err := dashscope.NewModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL("https://dashscope.example.test")),
		"paraformer-v2",
	)
	if err != nil {
		t.Fatalf("NewModel returned error: %v", err)
	}
	_, err = model.Recognize(context.Background(), stt.Request{
		Audio: stt.NewAudioBlock([]byte("raw"), "audio/wav"),
	})
	if err == nil || !strings.Contains(err.Error(), "requires a URL audio source") {
		t.Fatalf("Recognize should reject inline audio, got %v", err)
	}
}

func TestModelRecognizeURLAudioUsesAsyncTaskAndTranscript(t *testing.T) {
	t.Parallel()

	const audioURL = "https://example.test/audio.wav"
	var taskPolls int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/services/audio/asr/transcription":
			if r.Header.Get("X-DashScope-Async") != "enable" {
				t.Fatalf("X-DashScope-Async header mismatch: %q", r.Header.Get("X-DashScope-Async"))
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("Decode request body returned error: %v", err)
			}
			input := body["input"].(map[string]any)
			if _, exists := input["audio"]; exists {
				t.Fatalf("request should not send inline audio payload: %#v", input)
			}
			fileURLs := input["file_urls"].([]any)
			if len(fileURLs) != 1 || fileURLs[0] != audioURL {
				t.Fatalf("file_urls mismatch: %#v", fileURLs)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"request_id": "submit-req",
				"output": map[string]any{
					"task_id":     "task-1",
					"task_status": "PENDING",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/task-1":
			taskPolls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"request_id": "task-req",
				"output": map[string]any{
					"task_id":     "task-1",
					"task_status": "SUCCEEDED",
					"results": []any{map[string]any{
						"file_url":          audioURL,
						"transcription_url": server.URL + "/transcript.json",
						"subtask_status":    "SUCCEEDED",
					}},
				},
				"usage": map[string]any{"duration": 4},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/transcript.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_url": audioURL,
				"transcripts": []any{map[string]any{
					"channel_id":                       0,
					"content_duration_in_milliseconds": 3720,
					"text":                             "hello world",
					"sentences": []any{map[string]any{
						"begin_time": 100,
						"end_time":   1500,
						"text":       "hello world",
					}},
				}},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
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
		Audio: message.NewDataBlock(message.NewURLSource(audioURL, "audio/wav")),
	})
	if err != nil {
		t.Fatalf("Recognize URL audio returned error: %v", err)
	}
	got := collectSTTResponses(chunks)
	if len(got) != 1 || got[0].Text != "hello world" || !got[0].IsLast {
		t.Fatalf("URL transcript response mismatch: %#v", got)
	}
	if taskPolls != 1 {
		t.Fatalf("task should be polled once, got %d", taskPolls)
	}
	if len(got[0].Segments) != 1 || got[0].Segments[0].Start != 100*time.Millisecond ||
		got[0].Segments[0].End != 1500*time.Millisecond {
		t.Fatalf("transcript segment mismatch: %#v", got[0].Segments)
	}
}

func TestModelRecognizeProviderErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services/audio/asr/transcription" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		fileURLs := body["input"].(map[string]any)["file_urls"].([]any)
		if len(fileURLs) != 1 || fileURLs[0] != "https://example.test/audio.wav" {
			t.Fatalf("file_urls mismatch: %#v", fileURLs)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "InvalidInput", "message": "bad audio"})
	}))
	defer server.Close()

	model, err := dashscope.NewModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"paraformer-v2",
	)
	if err != nil {
		t.Fatalf("NewModel returned error: %v", err)
	}
	_, err = model.Recognize(context.Background(), stt.Request{
		Audio: message.NewDataBlock(message.NewURLSource("https://example.test/audio.wav", "audio/wav")),
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
	if _, err := model.NewSession(context.Background(), stt.SessionRequest{}); err == nil {
		t.Fatalf("NewSession should reject non-realtime model")
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
	realtimeCard := findSTTCard(cards, "qwen3-asr-flash-realtime")
	if realtimeCard.Name == "" || !realtimeCard.Realtime || realtimeCard.InputTypes[0] != "audio/pcm" {
		t.Fatalf("DashScope realtime STT card mismatch: %#v", realtimeCard)
	}
	realtimeProperties := realtimeCard.ParameterSchema["properties"].(map[string]any)
	if _, ok := realtimeProperties["mode"]; !ok {
		t.Fatalf("realtime mode schema should be present: %#v", realtimeCard.ParameterSchema)
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
