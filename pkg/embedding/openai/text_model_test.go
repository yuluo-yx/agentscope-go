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

package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	asembedding "github.com/yuluo-yx/agentscope-go/pkg/embedding"
	"github.com/yuluo-yx/agentscope-go/pkg/embedding/openai"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
)

func TestTextModelFormatsRequestParsesResponseAndUsesCache(t *testing.T) {
	t.Parallel()

	var calls int32
	requestCh := make(chan map[string]any, 1)
	server := newEmbeddingServer(t, http.StatusOK, func(body map[string]any) map[string]any {
		atomic.AddInt32(&calls, 1)
		requestCh <- body
		return map[string]any{
			"object": "list",
			"model":  "text-embedding-3-small",
			"data": []any{
				map[string]any{"object": "embedding", "index": 0, "embedding": []float64{0.1, 0.2}},
				map[string]any{"object": "embedding", "index": 1, "embedding": []float64{0.3, 0.4}},
			},
			"usage": map[string]any{"prompt_tokens": 5, "total_tokens": 5},
		}
	})
	defer server.Close()

	cache, err := asembedding.NewFileCache(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileCache returned error: %v", err)
	}
	model, err := openai.NewTextModel(
		openai.NewCredential("test-key", openai.WithBaseURL(server.URL)),
		"text-embedding-3-small",
		openai.WithDimensions(2),
		openai.WithCache(cache),
		openai.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}

	request := asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{
			asembedding.NewTextInput("hello"),
			asembedding.NewTextInput("world"),
		},
	}
	resp, err := model.Embed(context.Background(), request)
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if model.Name() != "openai:text-embedding-3-small" || model.Dimensions() != 2 {
		t.Fatalf("model metadata mismatch: name=%q dimensions=%d", model.Name(), model.Dimensions())
	}
	if len(resp.Embeddings) != 2 || resp.Embeddings[1][1] != 0.4 {
		t.Fatalf("embeddings not parsed: %#v", resp.Embeddings)
	}
	if resp.Source != asembedding.SourceAPI || resp.Usage == nil || resp.Usage.Tokens == nil || *resp.Usage.Tokens != 5 {
		t.Fatalf("response usage/source mismatch: %#v", resp)
	}
	body := <-requestCh
	if body["model"] != "text-embedding-3-small" || body["encoding_format"] != "float" || body["dimensions"] != float64(2) {
		t.Fatalf("OpenAI request body mismatch: %#v", body)
	}
	inputs := body["input"].([]any)
	if len(inputs) != 2 || inputs[0] != "hello" || inputs[1] != "world" {
		t.Fatalf("OpenAI input mismatch: %#v", inputs)
	}

	cached, err := model.Embed(context.Background(), request)
	if err != nil {
		t.Fatalf("cached Embed returned error: %v", err)
	}
	if cached.Source != asembedding.SourceCache || cached.Usage == nil || cached.Usage.Tokens == nil || *cached.Usage.Tokens != 0 {
		t.Fatalf("cache response mismatch: %#v", cached)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("cached call should not hit provider, calls=%d", calls)
	}
}

func TestTextModelCanOmitDimensionsFromRequest(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 1)
	server := newEmbeddingServer(t, http.StatusOK, func(body map[string]any) map[string]any {
		requestCh <- body
		return map[string]any{
			"object": "list",
			"model":  "text-embedding-3-small",
			"data": []any{
				map[string]any{"object": "embedding", "index": 0, "embedding": []float64{0.1, 0.2}},
			},
			"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		}
	})
	defer server.Close()

	model, err := openai.NewTextModel(
		openai.NewCredential("test-key", openai.WithBaseURL(server.URL)),
		"text-embedding-3-small",
		openai.WithDimensions(2),
		openai.WithPassDimensions(false),
		openai.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	if model.Dimensions() != 2 {
		t.Fatalf("model dimensions metadata should be retained, got %d", model.Dimensions())
	}

	if _, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("hello")},
	}); err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}

	body := <-requestCh
	if _, ok := body["dimensions"]; ok {
		t.Fatalf("OpenAI request should omit dimensions when pass_dimensions=false: %#v", body)
	}
}

func TestTextModelMapsProviderErrors(t *testing.T) {
	t.Parallel()

	server := newEmbeddingServer(t, http.StatusTooManyRequests, func(map[string]any) map[string]any {
		return map[string]any{"error": map[string]any{"message": "rate limited", "code": "rate_limit"}}
	})
	defer server.Close()

	model, err := openai.NewTextModel(
		openai.NewCredential("test-key", openai.WithBaseURL(server.URL)),
		"text-embedding-3-small",
		openai.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	_, err = model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("hello")}})
	if err == nil {
		t.Fatal("Embed should return provider error")
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error should expose ProviderError, got %T %v", err, err)
	}
	if providerErr.Provider != "openai" || providerErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("provider error metadata mismatch: %#v", providerErr)
	}
}

func newEmbeddingServer(t *testing.T, status int, handler func(map[string]any) map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body returned error: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(handler(body)); err != nil {
			t.Fatalf("Encode response returned error: %v", err)
		}
	}))
}
