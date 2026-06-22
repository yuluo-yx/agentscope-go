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

package ollama_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	asembedding "github.com/yuluo-yx/agentscope-go/pkg/embedding"
	"github.com/yuluo-yx/agentscope-go/pkg/embedding/ollama"
)

func TestTextModelFormatsRequestAndParsesResponse(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		requestCh <- body
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"model":             "nomic-embed-text",
			"embeddings":        []any{[]float32{0.1, 0.2}},
			"prompt_eval_count": 3,
		}); err != nil {
			t.Fatalf("Encode response returned error: %v", err)
		}
	}))
	defer server.Close()

	model, err := ollama.NewTextModel(
		ollama.NewCredential(ollama.WithHost(server.URL)),
		"nomic-embed-text",
		ollama.WithDimensions(2),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	resp, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("hello")}})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if model.Name() != "ollama:nomic-embed-text" || len(model.SupportedModalities()) != 1 {
		t.Fatalf("model metadata mismatch: %q %#v", model.Name(), model.SupportedModalities())
	}
	if len(resp.Embeddings) != 1 || resp.Embeddings[0][1] < 0.19 || resp.Embeddings[0][1] > 0.21 {
		t.Fatalf("embeddings not parsed: %#v", resp.Embeddings)
	}
	if resp.Usage == nil || resp.Usage.Tokens == nil || *resp.Usage.Tokens != 3 {
		t.Fatalf("usage not parsed: %#v", resp.Usage)
	}
	body := <-requestCh
	if body["model"] != "nomic-embed-text" || body["dimensions"] != float64(2) {
		t.Fatalf("request body mismatch: %#v", body)
	}
	inputs := body["input"].([]any)
	if len(inputs) != 1 || inputs[0] != "hello" {
		t.Fatalf("input mismatch: %#v", inputs)
	}
}
