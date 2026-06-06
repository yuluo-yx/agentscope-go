package embedding_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	"github.com/yuluo-yx/agentscope-go/embedding/dashscope"
	"github.com/yuluo-yx/agentscope-go/embedding/gemini"
	"github.com/yuluo-yx/agentscope-go/embedding/ollama"
	"github.com/yuluo-yx/agentscope-go/embedding/openai"
)

const embeddingSmokeEnv = "AGENTSCOPE_GO_EMBEDDING_SMOKE"

func TestSmokeOpenAITextEmbedding(t *testing.T) {
	requireEmbeddingSmoke(t)

	apiKey := requireEnv(t, "OPENAI_API_KEY")
	modelName := envOr("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small")
	dimensions := envIntOr(t, "OPENAI_EMBEDDING_DIMENSIONS", 1024)
	credentialOptions := []openai.CredentialOption{}
	if baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
		credentialOptions = append(credentialOptions, openai.WithBaseURL(baseURL))
	}

	model, err := openai.NewTextModel(
		openai.NewCredential(apiKey, credentialOptions...),
		modelName,
		openai.WithDimensions(dimensions),
		openai.WithMaxRetries(1),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	assertEmbeddingSmokeResponse(t, embedTextSmoke(t, model), 1)
}

func TestSmokeGeminiTextEmbedding(t *testing.T) {
	requireEmbeddingSmoke(t)

	model, err := gemini.NewTextModel(
		gemini.NewCredential(requireEnv(t, "GEMINI_API_KEY")),
		envOr("GEMINI_EMBEDDING_MODEL", "gemini-embedding-001"),
		gemini.WithDimensions(envIntOr(t, "GEMINI_EMBEDDING_DIMENSIONS", 3072)),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	assertEmbeddingSmokeResponse(t, embedTextSmoke(t, model), 1)
}

func TestSmokeOllamaTextEmbedding(t *testing.T) {
	requireEmbeddingSmoke(t)

	modelOptions := []ollama.TextModelOption{}
	if dimensions, ok := optionalEnvInt(t, "OLLAMA_EMBEDDING_DIMENSIONS"); ok {
		modelOptions = append(modelOptions, ollama.WithDimensions(dimensions))
	}
	model, err := ollama.NewTextModel(
		ollama.NewCredential(ollama.WithHost(requireEnv(t, "OLLAMA_HOST"))),
		envOr("OLLAMA_EMBEDDING_MODEL", "nomic-embed-text"),
		modelOptions...,
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	assertEmbeddingSmokeResponse(t, embedTextSmoke(t, model), 1)
}

func TestSmokeDashScopeTextEmbedding(t *testing.T) {
	requireEmbeddingSmoke(t)

	credentialOptions := []dashscope.CredentialOption{}
	if baseURL := strings.TrimSpace(os.Getenv("DASHSCOPE_BASE_URL")); baseURL != "" {
		credentialOptions = append(credentialOptions, dashscope.WithBaseURL(baseURL))
	}
	model, err := dashscope.NewTextModel(
		dashscope.NewCredential(requireEnv(t, "DASHSCOPE_API_KEY"), credentialOptions...),
		envOr("DASHSCOPE_TEXT_EMBEDDING_MODEL", "text-embedding-v4"),
		dashscope.WithDimensions(envIntOr(t, "DASHSCOPE_TEXT_EMBEDDING_DIMENSIONS", 1024)),
	)
	if err != nil {
		t.Fatalf("NewTextModel returned error: %v", err)
	}
	assertEmbeddingSmokeResponse(t, embedTextSmoke(t, model), 1)
}

func TestSmokeDashScopeMultiModalEmbedding(t *testing.T) {
	requireEmbeddingSmoke(t)

	credentialOptions := []dashscope.CredentialOption{}
	if baseURL := strings.TrimSpace(os.Getenv("DASHSCOPE_BASE_URL")); baseURL != "" {
		credentialOptions = append(credentialOptions, dashscope.WithBaseURL(baseURL))
	}
	modelOptions := []dashscope.ModelOption{}
	if dimensions, ok := optionalEnvInt(t, "DASHSCOPE_MULTIMODAL_EMBEDDING_DIMENSIONS"); ok {
		modelOptions = append(modelOptions, dashscope.WithDimensions(dimensions))
	}
	model, err := dashscope.NewMultiModalModel(
		dashscope.NewCredential(requireEnv(t, "DASHSCOPE_API_KEY"), credentialOptions...),
		envOr("DASHSCOPE_MULTIMODAL_EMBEDDING_MODEL", "multimodal-embedding-v1"),
		modelOptions...,
	)
	if err != nil {
		t.Fatalf("NewMultiModalModel returned error: %v", err)
	}

	imageInput := dashScopeSmokeImageInput(t)
	videoURL := requireEnv(t, "DASHSCOPE_MULTIMODAL_VIDEO_URL")
	ctx, cancel := smokeContext(t)
	defer cancel()
	resp, err := model.Embed(ctx, asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{
		asembedding.NewTextInput("AgentScope Go embedding smoke test"),
		imageInput,
		asembedding.NewVideoURLInput(videoURL, envOr("DASHSCOPE_MULTIMODAL_VIDEO_MEDIA_TYPE", "video/mp4")),
	}})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	assertEmbeddingSmokeResponse(t, resp, 3)
}

func embedTextSmoke(t *testing.T, model asembedding.EmbeddingModel) *asembedding.EmbeddingResponse {
	t.Helper()

	ctx, cancel := smokeContext(t)
	defer cancel()
	resp, err := model.Embed(ctx, asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{
		asembedding.NewTextInput("AgentScope Go embedding smoke test"),
	}})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	return resp
}

func assertEmbeddingSmokeResponse(t *testing.T, resp *asembedding.EmbeddingResponse, expected int) {
	t.Helper()

	if resp == nil {
		t.Fatal("response is nil")
	}
	if len(resp.Embeddings) != expected {
		t.Fatalf("expected %d embeddings, got %d", expected, len(resp.Embeddings))
	}
	for i, embedding := range resp.Embeddings {
		if len(embedding) == 0 {
			t.Fatalf("embedding %d is empty", i)
		}
	}
	if resp.Source != asembedding.SourceAPI {
		t.Fatalf("expected API response source, got %q", resp.Source)
	}
}

func dashScopeSmokeImageInput(t *testing.T) asembedding.EmbeddingInput {
	t.Helper()

	if imageURL := strings.TrimSpace(os.Getenv("DASHSCOPE_MULTIMODAL_IMAGE_URL")); imageURL != "" {
		return asembedding.NewImageURLInput(imageURL, envOr("DASHSCOPE_MULTIMODAL_IMAGE_MEDIA_TYPE", "image/png"))
	}
	imageBase64 := strings.TrimSpace(os.Getenv("DASHSCOPE_MULTIMODAL_IMAGE_BASE64"))
	imageMediaType := strings.TrimSpace(os.Getenv("DASHSCOPE_MULTIMODAL_IMAGE_MEDIA_TYPE"))
	if imageBase64 == "" || imageMediaType == "" {
		t.Skip("set DASHSCOPE_MULTIMODAL_IMAGE_URL or both DASHSCOPE_MULTIMODAL_IMAGE_BASE64 and DASHSCOPE_MULTIMODAL_IMAGE_MEDIA_TYPE for DashScope multimodal smoke test")
	}
	return asembedding.NewImageBase64Input(imageBase64, imageMediaType)
}

func requireEmbeddingSmoke(t *testing.T) {
	t.Helper()

	if os.Getenv(embeddingSmokeEnv) != "1" {
		t.Skipf("set %s=1 to run real embedding provider smoke tests", embeddingSmokeEnv)
	}
}

func requireEnv(t *testing.T, name string) string {
	t.Helper()

	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Skipf("set %s to run this smoke test", name)
	}
	return value
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envIntOr(t *testing.T, name string, fallback int) int {
	t.Helper()

	if value, ok := optionalEnvInt(t, name); ok {
		return value
	}
	return fallback
}

func optionalEnvInt(t *testing.T, name string) (int, bool) {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s must be an integer: %v", name, err)
	}
	return value, true
}

func smokeContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	timeoutSeconds := envIntOr(t, "EMBEDDING_SMOKE_TIMEOUT_SECONDS", 30)
	return context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
}
