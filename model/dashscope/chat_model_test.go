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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
)

func TestListModelsAndCompatibilityBoundary(t *testing.T) {
	t.Parallel()

	cards, err := dashscope.ListModels()
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("DashScope model cards should not be empty")
	}
	qwenPlus := findCard(cards, "qwen-plus")
	if qwenPlus.Name == "" || !qwenPlus.Supports(asmodel.ModelCapabilityText) || qwenPlus.Supports(asmodel.ModelCapabilityAudio) {
		t.Fatalf("DashScope qwen-plus metadata mismatch: %#v", qwenPlus)
	}
	qwenPlusProperties := dashscopeModelCardProperties(t, qwenPlus)
	if _, exists := qwenPlusProperties["voice"]; exists {
		t.Fatalf("DashScope text-only model should hide voice: %#v", qwenPlusProperties["voice"])
	}
	omni := findCard(cards, "qwen3.5-omni-plus")
	if omni.Name == "" || !omni.Supports(asmodel.ModelCapabilityAudio) || !omni.Supports(asmodel.ModelCapabilityVideo) {
		t.Fatalf("DashScope omni metadata should preserve Python multimodal types: %#v", omni)
	}
	voice := dashscopeModelCardProperties(t, omni)["voice"].(map[string]any)
	if voice["default"] != "Tina" {
		t.Fatalf("DashScope omni voice schema not merged: %#v", voice)
	}
	qwen35Plus := findCard(cards, "qwen3.5-plus")
	if qwen35Plus.Name == "" || !qwen35Plus.Supports(asmodel.ModelCapabilityText) || qwen35Plus.ContextSize <= 0 {
		t.Fatalf("DashScope qwen3.5-plus should be loaded from Python model cards: %#v", qwen35Plus)
	}

	boundary := dashscope.CompatibilityBoundaryInfo()
	if boundary.API != "openai_compatible_chat_completions" || !boundary.CompatibleCapabilities[asmodel.ModelCapabilityTools] {
		t.Fatalf("DashScope compatibility boundary mismatch: %#v", boundary)
	}
	if len(boundary.NativeOnlyCapabilities) == 0 || len(boundary.UnsupportedViaCompatible) == 0 {
		t.Fatalf("DashScope native-only boundary should be explicit: %#v", boundary)
	}
}

func TestChatModelParsesCompatibleAudioOutput(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		if body["model"] != "qwen-plus" {
			t.Fatalf("model not forwarded to compatible endpoint: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"id":      "dashscope-audio",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "qwen-plus",
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role": "assistant",
					"audio": map[string]any{
						"id":         "audio-1",
						"data":       "UklGRg==",
						"expires_at": time.Now().Add(time.Hour).Unix(),
						"transcript": "dashscope speech",
					},
				},
				"finish_reason": "stop",
			}},
		}); err != nil {
			t.Fatalf("Encode response returned error: %v", err)
		}
	}))
	defer server.Close()

	model, err := dashscope.NewChatModel(
		dashscope.NewCredential("test-key", dashscope.WithBaseURL(server.URL)),
		"qwen-plus",
		dashscope.WithStream(false),
	)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "speak")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	resp, err := model.Call(context.Background(), asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("expected audio data and transcript: %#v", resp.Content)
	}
	audio := resp.Content[0].(*message.DataBlock)
	source := audio.Source.(*message.Base64Source)
	if audio.ID != "audio-1" || source.MediaType != "audio/wav" || source.Data != "UklGRg==" {
		t.Fatalf("audio output not parsed: %#v source=%#v", audio, source)
	}
	if text := resp.Content[1].(*message.TextBlock).Text; text != "dashscope speech" {
		t.Fatalf("audio transcript mismatch: %q", text)
	}
}

func findCard(cards []asmodel.ModelCard, name string) asmodel.ModelCard {
	for _, card := range cards {
		if card.Name == name {
			return card
		}
	}
	return asmodel.ModelCard{}
}

func dashscopeModelCardProperties(t *testing.T, card asmodel.ModelCard) map[string]any {
	t.Helper()
	properties, ok := card.ParameterSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("model card schema properties missing for %s: %#v", card.Name, card.ParameterSchema)
	}
	return properties
}
