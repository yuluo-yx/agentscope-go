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

// Package credential exposes Python-compatible credential metadata and model discovery helpers.
package credential

import (
	"strings"

	"github.com/yuluo-yx/agentscope-go/audio/stt"
	sttdashscope "github.com/yuluo-yx/agentscope-go/audio/stt/dashscope"
	"github.com/yuluo-yx/agentscope-go/audio/tts"
	ttsdashscope "github.com/yuluo-yx/agentscope-go/audio/tts/dashscope"
	asembedding "github.com/yuluo-yx/agentscope-go/embedding"
	embeddingdashscope "github.com/yuluo-yx/agentscope-go/embedding/dashscope"
	embeddinggemini "github.com/yuluo-yx/agentscope-go/embedding/gemini"
	embeddingollama "github.com/yuluo-yx/agentscope-go/embedding/ollama"
	embeddingopenai "github.com/yuluo-yx/agentscope-go/embedding/openai"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	modelanthropic "github.com/yuluo-yx/agentscope-go/model/anthropic"
	modeldashscope "github.com/yuluo-yx/agentscope-go/model/dashscope"
	modelgemini "github.com/yuluo-yx/agentscope-go/model/gemini"
	modelmoonshot "github.com/yuluo-yx/agentscope-go/model/moonshot"
	modelollama "github.com/yuluo-yx/agentscope-go/model/ollama"
	modelopenai "github.com/yuluo-yx/agentscope-go/model/openai"
	modelopenairesponse "github.com/yuluo-yx/agentscope-go/model/openairesponse"
	"github.com/yuluo-yx/agentscope-go/utils"
)

const (
	dashScopeCompatibleBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	dashScopeNativeBaseURL     = "https://dashscope.aliyuncs.com"
	moonshotBaseURL            = "https://api.moonshot.cn/v1"
)

type (
	// ChatModelCard aliases the shared chat model card type.
	ChatModelCard = asmodel.ModelCard
	// EmbeddingModelCard aliases the shared embedding model card type.
	EmbeddingModelCard = asembedding.ModelCard
	// TTSModelCard aliases the shared TTS model card type.
	TTSModelCard = tts.ModelCard
	// STTModelCard aliases the shared STT model card type.
	STTModelCard = stt.ModelCard
)

// Type identifies the serialized credential type.
type Type string

const (
	// TypeAnthropic matches Python AnthropicCredential.type.
	TypeAnthropic Type = "anthropic_credential"
	// TypeDashScope matches Python DashScopeCredential.type.
	TypeDashScope Type = "dashscope_credential"
	// TypeGemini matches Python GeminiCredential.type.
	TypeGemini Type = "gemini_credential"
	// TypeMoonshot matches Python MoonshotCredential.type.
	TypeMoonshot Type = "moonshot_credential"
	// TypeOllama matches Python OllamaCredential.type.
	TypeOllama Type = "ollama_credential"
	// TypeOpenAI matches Python OpenAICredential.type.
	TypeOpenAI Type = "openai_credential"
)

// Provider describes the Go package that backs one model family.
type Provider struct {
	Name    string `json:"name"`
	Package string `json:"package"`
}

// Credential is the common model discovery surface shared by provider credentials.
type Credential interface {
	CredentialID() string
	CredentialName() string
	CredentialType() Type
	ChatProvider() Provider
	EmbeddingProvider() (Provider, bool)
	TTSProviders() []Provider
	STTProviders() []Provider
	ListChatModels() ([]ChatModelCard, error)
	ListEmbeddingModels() ([]EmbeddingModelCard, error)
	ListTTSModels() ([]TTSModelCard, error)
	ListSTTModels() ([]STTModelCard, error)
}

// Anthropic represents Anthropic API credential settings.
type Anthropic struct {
	Base
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url,omitempty"`
}

// NewAnthropic creates Anthropic credentials.
func NewAnthropic(apiKey string, opts ...Option) Anthropic {
	options := collectOptions(opts)
	return Anthropic{
		Base:    newBase(TypeAnthropic, options),
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: options.baseURL,
	}
}

// ChatProvider returns the Anthropic chat provider descriptor.
func (c Anthropic) ChatProvider() Provider {
	return Provider{Name: "anthropic", Package: "model/anthropic"}
}

// EmbeddingProvider reports that Anthropic has no embedding provider.
func (c Anthropic) EmbeddingProvider() (Provider, bool) {
	return Provider{}, false
}

// TTSProviders returns no Anthropic TTS providers.
func (c Anthropic) TTSProviders() []Provider {
	return nil
}

// STTProviders returns no Anthropic STT providers.
func (c Anthropic) STTProviders() []Provider {
	return nil
}

// ListChatModels lists Anthropic chat model cards.
func (c Anthropic) ListChatModels() ([]ChatModelCard, error) {
	return modelanthropic.ListModels()
}

// ListEmbeddingModels returns no Anthropic embedding model cards.
func (c Anthropic) ListEmbeddingModels() ([]EmbeddingModelCard, error) {
	return nil, nil
}

// ListTTSModels returns no Anthropic TTS model cards.
func (c Anthropic) ListTTSModels() ([]TTSModelCard, error) {
	return nil, nil
}

// ListSTTModels returns no Anthropic STT model cards.
func (c Anthropic) ListSTTModels() ([]STTModelCard, error) {
	return nil, nil
}

// ChatCredential returns credentials accepted by model/anthropic.
func (c Anthropic) ChatCredential() modelanthropic.Credential {
	options := make([]modelanthropic.CredentialOption, 0, 1)
	if c.BaseURL != "" {
		options = append(options, modelanthropic.WithBaseURL(c.BaseURL))
	}
	return modelanthropic.NewCredential(c.APIKey, options...)
}

// Option configures provider credential metadata and optional endpoints.
type Option func(*options)

type options struct {
	id           string
	name         string
	baseURL      string
	host         string
	organization string
}

// WithID sets the credential id. A new id is generated when omitted.
func WithID(id string) Option {
	return func(options *options) {
		options.id = strings.TrimSpace(id)
	}
}

// WithName sets a user-facing credential name.
func WithName(name string) Option {
	return func(options *options) {
		options.name = strings.TrimSpace(name)
	}
}

// WithBaseURL sets the provider API base URL.
func WithBaseURL(baseURL string) Option {
	return func(options *options) {
		options.baseURL = normalizeBaseURL(baseURL)
	}
}

// WithHost sets the Ollama host URL.
func WithHost(host string) Option {
	return func(options *options) {
		options.host = normalizeBaseURL(host)
	}
}

// WithOrganization sets the OpenAI organization id.
func WithOrganization(organization string) Option {
	return func(options *options) {
		options.organization = strings.TrimSpace(organization)
	}
}

// Base stores Python-compatible common credential metadata.
type Base struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type Type   `json:"type"`
}

// CredentialID returns the credential id.
func (b Base) CredentialID() string {
	return b.ID
}

// CredentialName returns the user-facing credential name.
func (b Base) CredentialName() string {
	return b.Name
}

// CredentialType returns the credential type.
func (b Base) CredentialType() Type {
	return b.Type
}

// DashScope represents DashScope API credential settings.
type DashScope struct {
	Base
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
}

// NewDashScope creates DashScope credentials.
func NewDashScope(apiKey string, opts ...Option) DashScope {
	options := collectOptions(opts)
	baseURL := options.baseURL
	if baseURL == "" {
		baseURL = dashScopeCompatibleBaseURL
	}
	return DashScope{
		Base:    newBase(TypeDashScope, options),
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: baseURL,
	}
}

// ChatProvider returns the DashScope chat provider descriptor.
func (c DashScope) ChatProvider() Provider {
	return Provider{Name: "dashscope", Package: "model/dashscope"}
}

// EmbeddingProvider returns the DashScope embedding provider descriptor.
func (c DashScope) EmbeddingProvider() (Provider, bool) {
	return Provider{Name: "dashscope", Package: "embedding/dashscope"}, true
}

// TTSProviders returns DashScope TTS provider descriptors.
func (c DashScope) TTSProviders() []Provider {
	return []Provider{{Name: "dashscope", Package: "audio/tts/dashscope"}}
}

// STTProviders returns DashScope STT provider descriptors.
func (c DashScope) STTProviders() []Provider {
	return []Provider{{Name: "dashscope", Package: "audio/stt/dashscope"}}
}

// ListChatModels lists DashScope chat model cards.
func (c DashScope) ListChatModels() ([]ChatModelCard, error) {
	return modeldashscope.ListModels()
}

// ListEmbeddingModels lists DashScope embedding model cards.
func (c DashScope) ListEmbeddingModels() ([]EmbeddingModelCard, error) {
	return embeddingdashscope.ListModels()
}

// ListTTSModels lists DashScope TTS model cards.
func (c DashScope) ListTTSModels() ([]TTSModelCard, error) {
	return ttsdashscope.ListModels()
}

// ListSTTModels lists DashScope STT model cards.
func (c DashScope) ListSTTModels() ([]STTModelCard, error) {
	return sttdashscope.ListModels()
}

// ChatCredential returns credentials accepted by model/dashscope.
func (c DashScope) ChatCredential() modeldashscope.Credential {
	return modeldashscope.NewCredential(c.APIKey, modeldashscope.WithBaseURL(c.chatBaseURL()))
}

// EmbeddingCredential returns credentials accepted by embedding/dashscope.
func (c DashScope) EmbeddingCredential() embeddingdashscope.Credential {
	return embeddingdashscope.NewCredential(c.APIKey, embeddingdashscope.WithBaseURL(c.nativeBaseURL()))
}

// TTSCredential returns credentials accepted by audio/tts/dashscope.
func (c DashScope) TTSCredential() ttsdashscope.Credential {
	return ttsdashscope.NewCredential(c.APIKey, ttsdashscope.WithBaseURL(c.nativeBaseURL()))
}

// STTCredential returns credentials accepted by audio/stt/dashscope.
func (c DashScope) STTCredential() sttdashscope.Credential {
	return sttdashscope.NewCredential(c.APIKey, sttdashscope.WithBaseURL(c.nativeBaseURL()))
}

func (c DashScope) chatBaseURL() string {
	if c.BaseURL == "" {
		return dashScopeCompatibleBaseURL
	}
	return c.BaseURL
}

func (c DashScope) nativeBaseURL() string {
	if c.BaseURL == "" || c.BaseURL == dashScopeCompatibleBaseURL {
		return dashScopeNativeBaseURL
	}
	return c.BaseURL
}

// Gemini represents Gemini API credential settings.
type Gemini struct {
	Base
	APIKey string `json:"api_key"`
}

// NewGemini creates Gemini credentials.
func NewGemini(apiKey string, opts ...Option) Gemini {
	options := collectOptions(opts)
	return Gemini{
		Base:   newBase(TypeGemini, options),
		APIKey: strings.TrimSpace(apiKey),
	}
}

// ChatProvider returns the Gemini chat provider descriptor.
func (c Gemini) ChatProvider() Provider {
	return Provider{Name: "gemini", Package: "model/gemini"}
}

// EmbeddingProvider returns the Gemini embedding provider descriptor.
func (c Gemini) EmbeddingProvider() (Provider, bool) {
	return Provider{Name: "gemini", Package: "embedding/gemini"}, true
}

// TTSProviders returns no Gemini TTS providers.
func (c Gemini) TTSProviders() []Provider {
	return nil
}

// STTProviders returns no Gemini STT providers.
func (c Gemini) STTProviders() []Provider {
	return nil
}

// ListChatModels lists Gemini chat model cards.
func (c Gemini) ListChatModels() ([]ChatModelCard, error) {
	return modelgemini.ListModels()
}

// ListEmbeddingModels lists Gemini embedding model cards.
func (c Gemini) ListEmbeddingModels() ([]EmbeddingModelCard, error) {
	return embeddinggemini.ListModels()
}

// ListTTSModels returns no Gemini TTS model cards.
func (c Gemini) ListTTSModels() ([]TTSModelCard, error) {
	return nil, nil
}

// ListSTTModels returns no Gemini STT model cards.
func (c Gemini) ListSTTModels() ([]STTModelCard, error) {
	return nil, nil
}

// ChatCredential returns credentials accepted by model/gemini.
func (c Gemini) ChatCredential() modelgemini.Credential {
	return modelgemini.NewCredential(c.APIKey)
}

// EmbeddingCredential returns credentials accepted by embedding/gemini.
func (c Gemini) EmbeddingCredential() embeddinggemini.Credential {
	return embeddinggemini.NewCredential(c.APIKey)
}

// Moonshot represents Moonshot API credential settings.
type Moonshot struct {
	Base
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
}

// NewMoonshot creates Moonshot credentials.
func NewMoonshot(apiKey string, opts ...Option) Moonshot {
	options := collectOptions(opts)
	baseURL := options.baseURL
	if baseURL == "" {
		baseURL = moonshotBaseURL
	}
	return Moonshot{
		Base:    newBase(TypeMoonshot, options),
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: baseURL,
	}
}

// ChatProvider returns the Moonshot chat provider descriptor.
func (c Moonshot) ChatProvider() Provider {
	return Provider{Name: "moonshot", Package: "model/moonshot"}
}

// EmbeddingProvider reports that Moonshot has no embedding provider.
func (c Moonshot) EmbeddingProvider() (Provider, bool) {
	return Provider{}, false
}

// TTSProviders returns no Moonshot TTS providers.
func (c Moonshot) TTSProviders() []Provider {
	return nil
}

// STTProviders returns no Moonshot STT providers.
func (c Moonshot) STTProviders() []Provider {
	return nil
}

// ListChatModels lists Moonshot chat model cards.
func (c Moonshot) ListChatModels() ([]ChatModelCard, error) {
	return modelmoonshot.ListModels()
}

// ListEmbeddingModels returns no Moonshot embedding model cards.
func (c Moonshot) ListEmbeddingModels() ([]EmbeddingModelCard, error) {
	return nil, nil
}

// ListTTSModels returns no Moonshot TTS model cards.
func (c Moonshot) ListTTSModels() ([]TTSModelCard, error) {
	return nil, nil
}

// ListSTTModels returns no Moonshot STT model cards.
func (c Moonshot) ListSTTModels() ([]STTModelCard, error) {
	return nil, nil
}

// ChatCredential returns credentials accepted by model/moonshot.
func (c Moonshot) ChatCredential() modelmoonshot.Credential {
	return modelmoonshot.NewCredential(c.APIKey, modelmoonshot.WithBaseURL(c.BaseURL))
}

// Ollama represents Ollama connection settings.
type Ollama struct {
	Base
	Host string `json:"host,omitempty"`
}

// NewOllama creates Ollama credentials.
func NewOllama(opts ...Option) Ollama {
	options := collectOptions(opts)
	return Ollama{
		Base: newBase(TypeOllama, options),
		Host: options.host,
	}
}

// ChatProvider returns the Ollama chat provider descriptor.
func (c Ollama) ChatProvider() Provider {
	return Provider{Name: "ollama", Package: "model/ollama"}
}

// EmbeddingProvider returns the Ollama embedding provider descriptor.
func (c Ollama) EmbeddingProvider() (Provider, bool) {
	return Provider{Name: "ollama", Package: "embedding/ollama"}, true
}

// TTSProviders returns no Ollama TTS providers.
func (c Ollama) TTSProviders() []Provider {
	return nil
}

// STTProviders returns no Ollama STT providers.
func (c Ollama) STTProviders() []Provider {
	return nil
}

// ListChatModels lists Ollama chat model cards.
func (c Ollama) ListChatModels() ([]ChatModelCard, error) {
	return modelollama.ListModels()
}

// ListEmbeddingModels returns no Ollama embedding cards; Ollama models are usually discovered from the server at runtime.
func (c Ollama) ListEmbeddingModels() ([]EmbeddingModelCard, error) {
	return nil, nil
}

// ListTTSModels returns no Ollama TTS model cards.
func (c Ollama) ListTTSModels() ([]TTSModelCard, error) {
	return nil, nil
}

// ListSTTModels returns no Ollama STT model cards.
func (c Ollama) ListSTTModels() ([]STTModelCard, error) {
	return nil, nil
}

// ChatCredential returns credentials accepted by model/ollama.
func (c Ollama) ChatCredential() modelollama.Credential {
	options := make([]modelollama.CredentialOption, 0, 1)
	if c.Host != "" {
		options = append(options, modelollama.WithHost(c.Host))
	}
	return modelollama.NewCredential(options...)
}

// EmbeddingCredential returns credentials accepted by embedding/ollama.
func (c Ollama) EmbeddingCredential() embeddingollama.Credential {
	options := make([]embeddingollama.CredentialOption, 0, 1)
	if c.Host != "" {
		options = append(options, embeddingollama.WithHost(c.Host))
	}
	return embeddingollama.NewCredential(options...)
}

// OpenAI represents OpenAI API credential settings.
type OpenAI struct {
	Base
	APIKey       string `json:"api_key"`
	Organization string `json:"organization,omitempty"`
	BaseURL      string `json:"base_url,omitempty"`
}

// NewOpenAI creates OpenAI credentials.
func NewOpenAI(apiKey string, opts ...Option) OpenAI {
	options := collectOptions(opts)
	return OpenAI{
		Base:         newBase(TypeOpenAI, options),
		APIKey:       strings.TrimSpace(apiKey),
		Organization: options.organization,
		BaseURL:      options.baseURL,
	}
}

// ChatProvider returns the OpenAI chat provider descriptor.
func (c OpenAI) ChatProvider() Provider {
	return Provider{Name: "openai", Package: "model/openai"}
}

// EmbeddingProvider returns the OpenAI embedding provider descriptor.
func (c OpenAI) EmbeddingProvider() (Provider, bool) {
	return Provider{Name: "openai", Package: "embedding/openai"}, true
}

// TTSProviders returns no OpenAI standalone TTS providers.
func (c OpenAI) TTSProviders() []Provider {
	return nil
}

// STTProviders returns no OpenAI standalone STT providers.
func (c OpenAI) STTProviders() []Provider {
	return nil
}

// ListChatModels lists OpenAI chat model cards.
func (c OpenAI) ListChatModels() ([]ChatModelCard, error) {
	return modelopenai.ListModels()
}

// ListEmbeddingModels lists OpenAI embedding model cards.
func (c OpenAI) ListEmbeddingModels() ([]EmbeddingModelCard, error) {
	return embeddingopenai.ListModels()
}

// ListTTSModels returns no OpenAI standalone TTS model cards.
func (c OpenAI) ListTTSModels() ([]TTSModelCard, error) {
	return nil, nil
}

// ListSTTModels returns no OpenAI standalone STT model cards.
func (c OpenAI) ListSTTModels() ([]STTModelCard, error) {
	return nil, nil
}

// ChatCredential returns credentials accepted by model/openai.
func (c OpenAI) ChatCredential() modelopenai.Credential {
	return modelopenai.NewCredential(c.APIKey, c.openAIOptions()...)
}

// ResponseCredential returns credentials accepted by model/openairesponse.
func (c OpenAI) ResponseCredential() modelopenairesponse.Credential {
	return modelopenairesponse.NewCredential(c.APIKey, c.openAIResponseOptions()...)
}

// EmbeddingCredential returns credentials accepted by embedding/openai.
func (c OpenAI) EmbeddingCredential() embeddingopenai.Credential {
	return embeddingopenai.NewCredential(c.APIKey, c.openAIEmbeddingOptions()...)
}

func (c OpenAI) openAIOptions() []modelopenai.CredentialOption {
	options := make([]modelopenai.CredentialOption, 0, 2)
	if c.BaseURL != "" {
		options = append(options, modelopenai.WithBaseURL(c.BaseURL))
	}
	if c.Organization != "" {
		options = append(options, modelopenai.WithOrganization(c.Organization))
	}
	return options
}

func (c OpenAI) openAIEmbeddingOptions() []embeddingopenai.CredentialOption {
	options := make([]embeddingopenai.CredentialOption, 0, 2)
	if c.BaseURL != "" {
		options = append(options, embeddingopenai.WithBaseURL(c.BaseURL))
	}
	if c.Organization != "" {
		options = append(options, embeddingopenai.WithOrganization(c.Organization))
	}
	return options
}

func (c OpenAI) openAIResponseOptions() []modelopenairesponse.CredentialOption {
	options := make([]modelopenairesponse.CredentialOption, 0, 2)
	if c.BaseURL != "" {
		options = append(options, modelopenairesponse.WithBaseURL(c.BaseURL))
	}
	if c.Organization != "" {
		options = append(options, modelopenairesponse.WithOrganization(c.Organization))
	}
	return options
}

func collectOptions(opts []Option) options {
	options := options{}
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

func newBase(typ Type, options options) Base {
	id := options.id
	if id == "" {
		id = utils.NewID()
	}
	return Base{
		ID:   id,
		Name: options.name,
		Type: typ,
	}
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

var (
	_ Credential = Anthropic{}
	_ Credential = DashScope{}
	_ Credential = Gemini{}
	_ Credential = Moonshot{}
	_ Credential = Ollama{}
	_ Credential = OpenAI{}
)
