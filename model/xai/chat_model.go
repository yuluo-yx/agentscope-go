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

// Package xai provides an OpenAI-compatible xAI chat model.
package xai

import (
	"context"
	"fmt"

	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/openai"
)

const (
	providerName       = "xai"
	defaultBaseURL     = "https://api.x.ai/v1"
	defaultContextSize = 131072
)

type (
	// Credential configures xAI authentication and endpoint settings.
	Credential = openai.Credential
	// CredentialOption customizes xAI credentials.
	CredentialOption = openai.CredentialOption
	// ChatParameters configures xAI chat completion parameters.
	ChatParameters = openai.ChatParameters
	// ChatModelOption customizes an xAI ChatModel.
	ChatModelOption = openai.ChatModelOption
)

// ChatModel delegates xAI's OpenAI-compatible API to the shared OpenAI adapter.
type ChatModel struct {
	inner *openai.ChatModel
}

// NewCredential creates xAI credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	options := append([]CredentialOption{openai.WithBaseURL(defaultBaseURL)}, opts...)
	return openai.NewCredential(apiKey, options...)
}

// WithBaseURL overrides the default xAI OpenAI-compatible endpoint.
func WithBaseURL(baseURL string) CredentialOption { return openai.WithBaseURL(baseURL) }

// WithChatParameters sets default xAI chat parameters.
func WithChatParameters(parameters ChatParameters) ChatModelOption {
	return openai.WithChatParameters(parameters)
}

// WithStream configures whether xAI calls use streaming by default.
func WithStream(stream bool) ChatModelOption { return openai.WithStream(stream) }

// WithContextSize overrides the model context size used for validation.
func WithContextSize(contextSize int) ChatModelOption {
	return openai.WithContextSize(contextSize)
}

// WithMaxRetries sets the maximum SDK retry count.
func WithMaxRetries(maxRetries int) ChatModelOption { return openai.WithMaxRetries(maxRetries) }

// NewChatModel creates an xAI chat model.
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
		return nil, fmt.Errorf("xai: nil chat model")
	}
	return m.inner.Call(ctx, request)
}

// Stream sends a streaming chat request.
func (m *ChatModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	if m == nil || m.inner == nil {
		return nil, fmt.Errorf("xai: nil chat model")
	}
	return m.inner.Stream(ctx, request)
}

// CountTokens estimates token usage for a request.
func (m *ChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	if m == nil || m.inner == nil {
		return 0, fmt.Errorf("xai: nil chat model")
	}
	return m.inner.CountTokens(request)
}

var _ asmodel.ChatModel = (*ChatModel)(nil)
