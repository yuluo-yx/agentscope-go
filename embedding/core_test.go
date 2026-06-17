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

package embedding_test

import (
	"errors"
	"testing"
	"time"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/types"
)

func TestEmbeddingInputConstructorsAndValidation(t *testing.T) {
	t.Parallel()

	request := asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{
			asembedding.NewTextInput("hello"),
			asembedding.NewImageURLInput("https://example.com/cat.png", "image/png"),
			asembedding.NewImageBase64Input("aGVsbG8=", "image/png"),
			asembedding.NewVideoURLInput("https://example.com/movie.mp4", "video/mp4"),
		},
	}

	if err := request.Validate(asembedding.ModalityText, asembedding.ModalityImage, asembedding.ModalityVideo); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if request.Inputs[0].Text != "hello" || request.Inputs[1].Source.URL == "" || request.Inputs[2].Source.Data == "" {
		t.Fatalf("constructors produced unexpected inputs: %#v", request.Inputs)
	}

	clone := request.Clone()
	clone.Inputs[1].Source.URL = "https://example.com/changed.png"
	if request.Inputs[1].Source.URL == clone.Inputs[1].Source.URL {
		t.Fatalf("Clone should deep-copy input sources")
	}
	if (*asembedding.EmbeddingSource)(nil).Clone() != nil {
		t.Fatalf("nil source clone should stay nil")
	}
}

func TestEmbeddingRequestRejectsUnsupportedAndMalformedInputs(t *testing.T) {
	t.Parallel()

	videoBase64 := asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{{
			Type: asembedding.ModalityVideo,
			Source: &asembedding.EmbeddingSource{
				Type:      asembedding.SourceBase64,
				Data:      "AAAA",
				MediaType: "video/mp4",
			},
		}},
	}
	if err := videoBase64.Validate(asembedding.ModalityText, asembedding.ModalityImage, asembedding.ModalityVideo); !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("video base64 should be invalid input, got %v", err)
	}

	imageForTextProvider := asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{asembedding.NewImageURLInput("https://example.com/cat.png", "image/png")},
	}
	if err := imageForTextProvider.Validate(asembedding.ModalityText); !errors.Is(err, asembedding.ErrUnsupportedModality) {
		t.Fatalf("image should be unsupported for text-only provider, got %v", err)
	}

	emptyText := asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("")},
	}
	if err := emptyText.Validate(asembedding.ModalityText); !errors.Is(err, asembedding.ErrInvalidEmbeddingInput) {
		t.Fatalf("empty text should be invalid, got %v", err)
	}

	cases := []struct {
		name    string
		request asembedding.EmbeddingRequest
	}{
		{name: "empty", request: asembedding.EmbeddingRequest{}},
		{name: "image missing source", request: asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{{Type: asembedding.ModalityImage}}}},
		{name: "url missing value", request: asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{{Type: asembedding.ModalityImage, Source: &asembedding.EmbeddingSource{Type: asembedding.SourceURL}}}}},
		{name: "base64 missing media type", request: asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{{Type: asembedding.ModalityImage, Source: &asembedding.EmbeddingSource{Type: asembedding.SourceBase64, Data: "AAAA"}}}}},
		{name: "bad source type", request: asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{{Type: asembedding.ModalityImage, Source: &asembedding.EmbeddingSource{Type: "file"}}}}},
		{name: "bad modality", request: asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{{Type: "audio"}}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.request.Validate(asembedding.ModalityText, asembedding.ModalityImage, asembedding.ModalityVideo); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEmbeddingResponseDefaultsCloneAndCacheSource(t *testing.T) {
	t.Parallel()

	tokens := 12
	resp := asembedding.NewEmbeddingResponse(
		[]types.Embedding{{0.1, 0.2, 0.3}},
		asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Time: 2 * time.Second, Tokens: &tokens}),
		asembedding.WithEmbeddingSource(asembedding.SourceCache),
		asembedding.WithEmbeddingMetadata(map[string]any{"source": "unit"}),
	)

	if resp.ID == "" || resp.CreatedAt == "" || resp.Type != asembedding.ResponseTypeEmbedding {
		t.Fatalf("response defaults not set: %#v", resp)
	}
	if resp.Source != asembedding.SourceCache {
		t.Fatalf("source mismatch: got %q", resp.Source)
	}
	if resp.Usage == nil || resp.Usage.Type != asembedding.UsageTypeEmbedding || *resp.Usage.Tokens != 12 {
		t.Fatalf("usage defaults not set: %#v", resp.Usage)
	}
	if resp.Metadata["source"] != "unit" {
		t.Fatalf("metadata not set: %#v", resp.Metadata)
	}

	clone := resp.Clone()
	clone.Embeddings[0][0] = 9
	*clone.Usage.Tokens = 99
	clone.Metadata["source"] = "clone"
	if resp.Embeddings[0][0] == 9 || *resp.Usage.Tokens == 99 {
		t.Fatalf("Clone should deep-copy embeddings and usage")
	}
	if resp.Metadata["source"] == "clone" {
		t.Fatalf("Clone should deep-copy metadata")
	}
	if (*asembedding.EmbeddingUsage)(nil).Clone() != nil || (*asembedding.EmbeddingResponse)(nil).Clone() != nil {
		t.Fatalf("nil clone receivers should stay nil")
	}

	blanked := asembedding.NewEmbeddingResponse(nil, func(resp *asembedding.EmbeddingResponse) {
		resp.ID = ""
		resp.CreatedAt = ""
		resp.Type = ""
		resp.Source = ""
		resp.Metadata = nil
		resp.Usage = &asembedding.EmbeddingUsage{}
	})
	if blanked.ID == "" || blanked.CreatedAt == "" || blanked.Type != asembedding.ResponseTypeEmbedding || blanked.Source != asembedding.SourceAPI {
		t.Fatalf("NewEmbeddingResponse should restore blank defaults: %#v", blanked)
	}
	if blanked.Metadata == nil || blanked.Usage.Type != asembedding.UsageTypeEmbedding {
		t.Fatalf("NewEmbeddingResponse should normalize nil metadata and usage type: %#v", blanked)
	}
}

func TestCacheIdentifierIsStableAndProviderQualified(t *testing.T) {
	t.Parallel()

	request := asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{asembedding.NewTextInput("hello")},
		Parameters: map[string]any{
			"b": 2,
			"a": []any{"x", "y"},
		},
	}
	first := asembedding.CacheIdentifier("openai", "text-embedding-3-small", 1024, request)
	second := asembedding.CacheIdentifier("openai", "text-embedding-3-small", 1024, request)
	otherProvider := asembedding.CacheIdentifier("gemini", "text-embedding-3-small", 1024, request)

	if first != second {
		t.Fatalf("cache identifier should be stable: %q != %q", first, second)
	}
	if first == otherProvider {
		t.Fatalf("cache identifier should include provider name")
	}

	unmarshalable := asembedding.EmbeddingRequest{
		Inputs:   []asembedding.EmbeddingInput{asembedding.NewTextInput("hello")},
		Metadata: map[string]any{"bad": func() {}},
	}
	if got := asembedding.CacheIdentifier("mock", "bad-metadata", 0, unmarshalable); got == "" {
		t.Fatal("cache identifier should fall back for unmarshalable request metadata")
	}
}

func TestEmbeddingValidationWithoutSupportedModalityFilter(t *testing.T) {
	t.Parallel()

	request := asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{asembedding.NewVideoURLInput("https://example.com/movie.mp4", "video/mp4")},
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("Validate without modality filter should accept a valid video input: %v", err)
	}
}
