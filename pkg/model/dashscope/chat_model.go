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

// Package dashscope provides an OpenAI-compatible DashScope chat model.
package dashscope

import (
	"context"
	"embed"
	"fmt"

	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/model/openai"
)

const (
	providerName       = "dashscope"
	defaultBaseURL     = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultContextSize = 131072
)

//go:embed models/*.yaml
var modelFS embed.FS

type (
	// Credential configures DashScope authentication and endpoint settings.
	Credential = openai.Credential
	// CredentialOption customizes DashScope credentials.
	CredentialOption = openai.CredentialOption
	// ChatParameters configures DashScope chat completion parameters.
	ChatParameters = openai.ChatParameters
	// ChatModelOption customizes a DashScope ChatModel.
	ChatModelOption = openai.ChatModelOption
)

// CompatibilityBoundary describes which DashScope features are exposed by this provider.
type CompatibilityBoundary struct {
	Provider                 string
	API                      string
	Endpoint                 string
	CompatibleCapabilities   asmodel.ModelCapabilities
	NativeOnlyCapabilities   []string
	NativeOnlyModelFamilies  []string
	UnsupportedViaCompatible []asmodel.ModelCapability
}

// ChatModel delegates DashScope's OpenAI-compatible API to the shared OpenAI adapter.
type ChatModel struct {
	inner *openai.ChatModel
}

// CompatibilityBoundary returns the explicit boundary between DashScope
// OpenAI-compatible chat support and native DashScope APIs that need dedicated providers.
func CompatibilityBoundaryInfo() CompatibilityBoundary {
	return CompatibilityBoundary{
		Provider: "dashscope",
		API:      "openai_compatible_chat_completions",
		Endpoint: defaultBaseURL,
		CompatibleCapabilities: asmodel.ModelCapabilities{
			asmodel.ModelCapabilityText:             true,
			asmodel.ModelCapabilityTools:            true,
			asmodel.ModelCapabilityImage:            true,
			asmodel.ModelCapabilityAudio:            true,
			asmodel.ModelCapabilityStructuredOutput: false,
			asmodel.ModelCapabilityEmbedding:        false,
			asmodel.ModelCapabilityGeneration:       true,
			asmodel.ModelCapabilityVideo:            false,
		},
		NativeOnlyCapabilities: []string{
			"text_embedding",
			"multimodal_embedding",
			"native_multimodal_generation",
			"video_input",
		},
		NativeOnlyModelFamilies: []string{
			"text-embedding-*",
			"multimodal-embedding-*",
			"qwen-omni-*",
			"qwen-vl-*",
		},
		UnsupportedViaCompatible: []asmodel.ModelCapability{
			asmodel.ModelCapabilityVideo,
			asmodel.ModelCapabilityEmbedding,
			asmodel.ModelCapabilityStructuredOutput,
		},
	}
}

// ListModels returns embedded DashScope model cards for the OpenAI-compatible path.
func ListModels() ([]asmodel.ModelCard, error) {
	return asmodel.LoadModelCardsFSWithDefaults(modelFS, "models", asmodel.NewModelCardDefaults(providerName, asmodel.ModelCapabilities{
		asmodel.ModelCapabilityTools:            true,
		asmodel.ModelCapabilityStructuredOutput: false,
		asmodel.ModelCapabilityEmbedding:        false,
		asmodel.ModelCapabilityGeneration:       true,
	}, map[string]any{
		"api": "openai_compatible_chat_completions",
		"native_only_capabilities": []any{
			"text_embedding",
			"multimodal_embedding",
			"native_multimodal_generation",
			"video_input",
		},
	}))
}

// NewCredential creates DashScope credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	options := append([]CredentialOption{openai.WithBaseURL(defaultBaseURL)}, opts...)
	return openai.NewCredential(apiKey, options...)
}

// WithBaseURL overrides the default DashScope OpenAI-compatible endpoint.
func WithBaseURL(baseURL string) CredentialOption { return openai.WithBaseURL(baseURL) }

// WithChatParameters sets default DashScope chat parameters.
func WithChatParameters(parameters ChatParameters) ChatModelOption {
	return openai.WithChatParameters(parameters)
}

// WithExtraBody sets provider-specific request body fields for DashScope compatible APIs.
func WithExtraBody(extraBody map[string]any) ChatModelOption {
	return openai.WithExtraBody(extraBody)
}

// WithStream forwards the compatibility stream preference.
// Call and Stream still choose the request transport explicitly.
func WithStream(stream bool) ChatModelOption { return openai.WithStream(stream) }

// WithContextSize overrides the model context size used for validation.
func WithContextSize(contextSize int) ChatModelOption {
	return openai.WithContextSize(contextSize)
}

// WithMaxRetries sets the maximum SDK retry count.
func WithMaxRetries(maxRetries int) ChatModelOption { return openai.WithMaxRetries(maxRetries) }

// NewChatModel creates a DashScope chat model.
func NewChatModel(credential Credential, model string, opts ...ChatModelOption) (*ChatModel, error) {
	options := append([]ChatModelOption{
		openai.WithProviderName(providerName),
		openai.WithContextSize(defaultContextSize),
	}, opts...)
	inner, err := openai.NewChatModel(credential, model, options...)
	if err != nil {
		return nil, err
	}
	return &ChatModel{inner: inner}, nil
}

// Name returns the provider-qualified model name.
func (m *ChatModel) Name() string {
	if m == nil || m.inner == nil {
		return providerName + ":<nil>"
	}
	return m.inner.Name()
}

// Call sends a non-streaming chat request.
func (m *ChatModel) Call(ctx context.Context, request asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	if m == nil || m.inner == nil {
		return nil, fmt.Errorf("dashscope: nil chat model")
	}
	return m.inner.Call(ctx, request)
}

// Stream sends a streaming chat request.
func (m *ChatModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	if m == nil || m.inner == nil {
		return nil, fmt.Errorf("dashscope: nil chat model")
	}
	return m.inner.Stream(ctx, request)
}

// CountTokens estimates token usage for a request.
func (m *ChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	if m == nil || m.inner == nil {
		return 0, fmt.Errorf("dashscope: nil chat model")
	}
	return m.inner.CountTokens(request)
}

var _ asmodel.ChatModel = (*ChatModel)(nil)
