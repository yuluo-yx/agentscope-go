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
}

func TestEmbeddingResponseDefaultsCloneAndCacheSource(t *testing.T) {
	t.Parallel()

	tokens := 12
	resp := asembedding.NewEmbeddingResponse(
		[]types.Embedding{{0.1, 0.2, 0.3}},
		asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Time: 2 * time.Second, Tokens: &tokens}),
		asembedding.WithEmbeddingSource(asembedding.SourceCache),
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

	clone := resp.Clone()
	clone.Embeddings[0][0] = 9
	*clone.Usage.Tokens = 99
	if resp.Embeddings[0][0] == 9 || *resp.Usage.Tokens == 99 {
		t.Fatalf("Clone should deep-copy embeddings and usage")
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
}
