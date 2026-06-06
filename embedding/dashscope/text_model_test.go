package dashscope_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/embedding/dashscope"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestTextModelBatchesRequestsAndAggregatesUsage(t *testing.T) {
	t.Parallel()

	var requests []map[string]any
	server := newDashScopeServer(t, "/api/v1/services/embeddings/text-embedding/text-embedding", http.StatusOK, func(body map[string]any) map[string]any {
		requests = append(requests, body)
		input := body["input"].(map[string]any)
		inputs := input["texts"].([]any)
		embeddings := make([]any, 0, len(inputs))
		for i := range inputs {
			embeddings = append(embeddings, map[string]any{"text_index": i, "embedding": []float64{float64(i), float64(i + 1)}})
		}
		return map[string]any{
			"request_id": "dash-text",
			"output":     map[string]any{"embeddings": embeddings},
			"usage":      map[string]any{"total_tokens": len(inputs)},
		}
	})
	defer server.Close()

	model, err := dashscope.NewTextModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"text-embedding-v4",
		dashscope.WithDimensions(2),
		dashscope.WithBatchSizeLimit(2),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	resp, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{
		asembedding.NewTextInput("one"),
		asembedding.NewTextInput("two"),
		asembedding.NewTextInput("three"),
	}, Parameters: map[string]any{"output_type": "dense"}})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 batched requests, got %d", len(requests))
	}
	parameters := requests[0]["parameters"].(map[string]any)
	if requests[0]["model"] != "text-embedding-v4" || parameters["dimension"] != float64(2) || parameters["output_type"] != "dense" {
		t.Fatalf("request body mismatch: %#v", requests[0])
	}
	firstInput := requests[0]["input"].(map[string]any)["texts"].([]any)
	if len(firstInput) != 2 || firstInput[0] != "one" || firstInput[1] != "two" {
		t.Fatalf("text input should be wrapped under input.texts: %#v", requests[0]["input"])
	}
	if len(resp.Embeddings) != 3 || resp.Usage == nil || resp.Usage.Tokens == nil || *resp.Usage.Tokens != 3 {
		t.Fatalf("aggregated response mismatch: %#v", resp)
	}
}

func TestMultiModalModelFormatsTextImageAndVideo(t *testing.T) {
	t.Parallel()

	requestCh := make(chan map[string]any, 3)
	server := newDashScopeServer(t, "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding", http.StatusOK, func(body map[string]any) map[string]any {
		requestCh <- body
		input := body["input"].(map[string]any)
		inputs := input["contents"].([]any)
		embeddings := make([]any, 0, len(inputs))
		for i := range inputs {
			embeddings = append(embeddings, map[string]any{"embedding": []float64{float64(i), float64(i + 1)}})
		}
		return map[string]any{
			"request_id": "dash-mm",
			"output":     map[string]any{"embeddings": embeddings},
			"usage":      map[string]any{"input_tokens": len(inputs)},
		}
	})
	defer server.Close()

	model, err := dashscope.NewMultiModalModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"multimodal-embedding-v1",
	)
	if err != nil {
		t.Fatalf("NewMultiModalModel returned error: %v", err)
	}
	resp, err := model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{
		asembedding.NewTextInput("hello"),
		asembedding.NewImageBase64Input("aGVsbG8=", "image/png"),
		asembedding.NewVideoURLInput("https://example.com/movie.mp4", "video/mp4"),
	}})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if model.Dimensions() != 1024 || len(model.SupportedModalities()) != 3 {
		t.Fatalf("multimodal metadata mismatch: dimensions=%d modalities=%#v", model.Dimensions(), model.SupportedModalities())
	}
	if len(resp.Embeddings) != 3 || resp.Usage == nil || resp.Usage.Tokens == nil || *resp.Usage.Tokens != 3 {
		t.Fatalf("response mismatch: %#v", resp)
	}
	bodies := []map[string]any{<-requestCh, <-requestCh, <-requestCh}
	firstInput := bodies[0]["input"].(map[string]any)["contents"].([]any)
	secondInput := bodies[1]["input"].(map[string]any)["contents"].([]any)
	thirdInput := bodies[2]["input"].(map[string]any)["contents"].([]any)
	if firstInput[0].(map[string]any)["text"] != "hello" {
		t.Fatalf("text input not formatted: %#v", firstInput[0])
	}
	if secondInput[0].(map[string]any)["image"] != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("image base64 not formatted: %#v", secondInput[0])
	}
	if thirdInput[0].(map[string]any)["video"] != "https://example.com/movie.mp4" {
		t.Fatalf("video URL not formatted: %#v", thirdInput[0])
	}
}

func TestMultiModalModelRejectsInvalidDimensionsAndVideoBase64(t *testing.T) {
	t.Parallel()

	if _, err := dashscope.NewMultiModalModel(dashscope.NewCredential("dash-key"), "tongyi-embedding-vision-plus", dashscope.WithDimensions(1024)); !errors.Is(err, asembedding.ErrInvalidEmbeddingDimension) {
		t.Fatalf("invalid vision-plus dimension should be rejected, got %v", err)
	}

	model, err := dashscope.NewMultiModalModel(dashscope.NewCredential("dash-key"), "multimodal-embedding-v1")
	if err != nil {
		t.Fatalf("NewMultiModalModel returned error: %v", err)
	}
	_, err = model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{{
		Type: asembedding.ModalityVideo,
		Source: &asembedding.EmbeddingSource{
			Type:      asembedding.SourceBase64,
			Data:      "AAAA",
			MediaType: "video/mp4",
		},
	}}})
	if !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("video base64 should be rejected, got %v", err)
	}
}

func TestTextModelMapsDashScopeProviderErrors(t *testing.T) {
	t.Parallel()

	server := newDashScopeServer(t, "/api/v1/services/embeddings/text-embedding/text-embedding", http.StatusBadRequest, func(map[string]any) map[string]any {
		return map[string]any{"code": "InvalidInput", "message": "bad input"}
	})
	defer server.Close()

	model, err := dashscope.NewTextModel(
		dashscope.NewCredential("dash-key", dashscope.WithBaseURL(server.URL)),
		"text-embedding-v4",
		dashscope.WithDimensions(2),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	_, err = model.Embed(context.Background(), asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("hello")}})
	if err == nil {
		t.Fatal("Embed should return provider error")
	}
	var providerErr *asmodel.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Provider != "dashscope" || providerErr.StatusCode != http.StatusBadRequest || providerErr.Code != "InvalidInput" {
		t.Fatalf("provider error metadata mismatch: %#v err=%v", providerErr, err)
	}
}

func newDashScopeServer(t *testing.T, path string, status int, handler func(map[string]any) map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer dash-key" {
			t.Fatalf("Authorization header mismatch: %q", r.Header.Get("Authorization"))
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
