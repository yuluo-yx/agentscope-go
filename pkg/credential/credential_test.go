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

package credential_test

import (
	"testing"

	sttdashscope "github.com/yuluo-yx/agentscope-go/pkg/audio/stt/dashscope"
	ttsdashscope "github.com/yuluo-yx/agentscope-go/pkg/audio/tts/dashscope"
	"github.com/yuluo-yx/agentscope-go/pkg/credential"
	embeddingdashscope "github.com/yuluo-yx/agentscope-go/pkg/embedding/dashscope"
	embeddinggemini "github.com/yuluo-yx/agentscope-go/pkg/embedding/gemini"
	embeddingollama "github.com/yuluo-yx/agentscope-go/pkg/embedding/ollama"
	embeddingopenai "github.com/yuluo-yx/agentscope-go/pkg/embedding/openai"
	modelanthropic "github.com/yuluo-yx/agentscope-go/pkg/model/anthropic"
	modeldashscope "github.com/yuluo-yx/agentscope-go/pkg/model/dashscope"
	modelgemini "github.com/yuluo-yx/agentscope-go/pkg/model/gemini"
	modelmoonshot "github.com/yuluo-yx/agentscope-go/pkg/model/moonshot"
	modelollama "github.com/yuluo-yx/agentscope-go/pkg/model/ollama"
	modelopenai "github.com/yuluo-yx/agentscope-go/pkg/model/openai"
	modelopenairesponse "github.com/yuluo-yx/agentscope-go/pkg/model/openairesponse"
)

func TestCredentialBaseMetadata(t *testing.T) {
	t.Parallel()

	first := credential.NewOpenAI("sk-test")
	second := credential.NewOpenAI("sk-test")
	if first.CredentialID() == "" || second.CredentialID() == "" || first.CredentialID() == second.CredentialID() {
		t.Fatalf("credential ids should be generated and unique: %q %q", first.CredentialID(), second.CredentialID())
	}

	named := credential.NewDashScope(
		"dash-key",
		credential.WithID("cred-1"),
		credential.WithName("production"),
		credential.WithBaseURL(" https://dashscope.example/v1/ "),
	)
	if named.CredentialID() != "cred-1" || named.CredentialName() != "production" {
		t.Fatalf("credential metadata mismatch: id=%q name=%q", named.CredentialID(), named.CredentialName())
	}
	if named.CredentialType() != credential.TypeDashScope {
		t.Fatalf("credential type mismatch: %q", named.CredentialType())
	}
	if named.BaseURL != "https://dashscope.example/v1" {
		t.Fatalf("base url should be normalized: %q", named.BaseURL)
	}
}

func TestCredentialModelDiscoveryMatchesPythonProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		credential            credential.Credential
		wantChat              string
		wantEmbeddingProvider bool
		wantEmbedding         string
		wantTTS               string
		wantSTT               string
	}{
		{
			name:       "anthropic",
			credential: credential.NewAnthropic("anthropic-key"),
			wantChat:   "claude-sonnet-4-5",
		},
		{
			name:                  "dashscope",
			credential:            credential.NewDashScope("dash-key"),
			wantChat:              "qwen-plus",
			wantEmbeddingProvider: true,
			wantEmbedding:         "qwen3-vl-embedding",
			wantTTS:               "qwen3-tts-flash-realtime",
			wantSTT:               "paraformer-v2",
		},
		{
			name:                  "openai",
			credential:            credential.NewOpenAI("openai-key"),
			wantChat:              "gpt-4o",
			wantEmbeddingProvider: true,
			wantEmbedding:         "text-embedding-3-large",
		},
		{
			name:                  "gemini",
			credential:            credential.NewGemini("gemini-key"),
			wantChat:              "gemini-2.5-flash",
			wantEmbeddingProvider: true,
			wantEmbedding:         "gemini-embedding-001",
		},
		{
			name:                  "ollama",
			credential:            credential.NewOllama(credential.WithHost(" http://localhost:11434/ ")),
			wantChat:              "qwen3:14b",
			wantEmbeddingProvider: true,
		},
		{
			name:       "moonshot",
			credential: credential.NewMoonshot("moonshot-key"),
			wantChat:   "moonshot-v1-8k",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			chatModels, err := tt.credential.ListChatModels()
			if err != nil {
				t.Fatalf("ListChatModels returned error: %v", err)
			}
			if !hasChatModel(chatModels, tt.wantChat) {
				t.Fatalf("chat model %q not found in %#v", tt.wantChat, chatModels)
			}
			_, hasEmbeddingProvider := tt.credential.EmbeddingProvider()
			if hasEmbeddingProvider != tt.wantEmbeddingProvider {
				t.Fatalf("embedding provider support mismatch: got %v want %v", hasEmbeddingProvider, tt.wantEmbeddingProvider)
			}

			embeddingModels, err := tt.credential.ListEmbeddingModels()
			if err != nil {
				t.Fatalf("ListEmbeddingModels returned error: %v", err)
			}
			if tt.wantEmbedding == "" {
				if len(embeddingModels) != 0 {
					t.Fatalf("provider should not expose embedding models: %#v", embeddingModels)
				}
			} else if !hasEmbeddingModel(embeddingModels, tt.wantEmbedding) {
				t.Fatalf("embedding model %q not found in %#v", tt.wantEmbedding, embeddingModels)
			}

			ttsModels, err := tt.credential.ListTTSModels()
			if err != nil {
				t.Fatalf("ListTTSModels returned error: %v", err)
			}
			if tt.wantTTS == "" {
				if len(ttsModels) != 0 {
					t.Fatalf("provider should not expose TTS models: %#v", ttsModels)
				}
			} else if !hasTTSModel(ttsModels, tt.wantTTS) {
				t.Fatalf("TTS model %q not found in %#v", tt.wantTTS, ttsModels)
			}

			sttModels, err := tt.credential.ListSTTModels()
			if err != nil {
				t.Fatalf("ListSTTModels returned error: %v", err)
			}
			if tt.wantSTT == "" {
				if len(sttModels) != 0 {
					t.Fatalf("provider should not expose STT models: %#v", sttModels)
				}
			} else if !hasSTTModel(sttModels, tt.wantSTT) {
				t.Fatalf("STT model %q not found in %#v", tt.wantSTT, sttModels)
			}
		})
	}
}

func TestCredentialProviderDescriptorsAndOptions(t *testing.T) {
	t.Parallel()

	openai := credential.NewOpenAI(
		" openai-key ",
		credential.WithID("openai-1"),
		credential.WithName("OpenAI Prod"),
		credential.WithBaseURL(" https://api.openai.test/v1/ "),
		credential.WithOrganization(" org-1 "),
	)
	if openai.APIKey != "openai-key" || openai.Organization != "org-1" || openai.BaseURL != "https://api.openai.test/v1" {
		t.Fatalf("OpenAI options mismatch: %#v", openai)
	}
	if provider := openai.ChatProvider(); provider.Name != "openai" || provider.Package != "model/openai" {
		t.Fatalf("OpenAI chat provider mismatch: %#v", provider)
	}
	if provider, ok := openai.EmbeddingProvider(); !ok || provider.Package != "embedding/openai" {
		t.Fatalf("OpenAI embedding provider mismatch: %#v ok=%v", provider, ok)
	}
	if providers := openai.TTSProviders(); len(providers) != 0 {
		t.Fatalf("OpenAI should not expose standalone TTS providers: %#v", providers)
	}
	if providers := openai.STTProviders(); len(providers) != 0 {
		t.Fatalf("OpenAI should not expose standalone STT providers: %#v", providers)
	}

	dashscope := credential.NewDashScope("dash-key")
	if provider := dashscope.ChatProvider(); provider.Name != "dashscope" {
		t.Fatalf("DashScope chat provider mismatch: %#v", provider)
	}
	if providers := dashscope.TTSProviders(); len(providers) != 1 || providers[0].Package != "audio/tts/dashscope" {
		t.Fatalf("DashScope TTS provider mismatch: %#v", providers)
	}
	if providers := dashscope.STTProviders(); len(providers) != 1 || providers[0].Package != "audio/stt/dashscope" {
		t.Fatalf("DashScope STT provider mismatch: %#v", providers)
	}

	anthropic := credential.NewAnthropic("anthropic-key")
	if provider := anthropic.ChatProvider(); provider.Package != "model/anthropic" {
		t.Fatalf("Anthropic chat provider mismatch: %#v", provider)
	}
	if providers := anthropic.TTSProviders(); len(providers) != 0 {
		t.Fatalf("Anthropic should not expose TTS providers: %#v", providers)
	}
	if providers := anthropic.STTProviders(); len(providers) != 0 {
		t.Fatalf("Anthropic should not expose STT providers: %#v", providers)
	}

	moonshot := credential.NewMoonshot("moon-key")
	if provider := moonshot.ChatProvider(); provider.Package != "model/moonshot" {
		t.Fatalf("Moonshot chat provider mismatch: %#v", provider)
	}
	if provider, ok := moonshot.EmbeddingProvider(); ok || provider.Name != "" {
		t.Fatalf("Moonshot should not expose embedding provider: %#v ok=%v", provider, ok)
	}
	if embeddings, err := moonshot.ListEmbeddingModels(); err != nil || len(embeddings) != 0 {
		t.Fatalf("Moonshot embedding models mismatch: cards=%#v err=%v", embeddings, err)
	}
	if ttsCards, err := moonshot.ListTTSModels(); err != nil || len(ttsCards) != 0 {
		t.Fatalf("Moonshot TTS models mismatch: cards=%#v err=%v", ttsCards, err)
	}
	if providers := moonshot.TTSProviders(); len(providers) != 0 {
		t.Fatalf("Moonshot should not expose TTS providers: %#v", providers)
	}
	if sttCards, err := moonshot.ListSTTModels(); err != nil || len(sttCards) != 0 {
		t.Fatalf("Moonshot STT models mismatch: cards=%#v err=%v", sttCards, err)
	}
	if providers := moonshot.STTProviders(); len(providers) != 0 {
		t.Fatalf("Moonshot should not expose STT providers: %#v", providers)
	}

	ollama := credential.NewOllama(credential.WithHost(" http://localhost:11434/ "))
	if ollama.Host != "http://localhost:11434" {
		t.Fatalf("Ollama host should be normalized: %q", ollama.Host)
	}
	if provider := ollama.ChatProvider(); provider.Package != "model/ollama" {
		t.Fatalf("Ollama chat provider mismatch: %#v", provider)
	}
	if provider, ok := ollama.EmbeddingProvider(); !ok || provider.Package != "embedding/ollama" {
		t.Fatalf("Ollama embedding provider mismatch: %#v ok=%v", provider, ok)
	}
	if embeddings, err := ollama.ListEmbeddingModels(); err != nil || len(embeddings) != 0 {
		t.Fatalf("Ollama embedding models are runtime-discovered, got cards=%#v err=%v", embeddings, err)
	}
	if ttsCards, err := ollama.ListTTSModels(); err != nil || len(ttsCards) != 0 {
		t.Fatalf("Ollama TTS models mismatch: cards=%#v err=%v", ttsCards, err)
	}
	if providers := ollama.TTSProviders(); len(providers) != 0 {
		t.Fatalf("Ollama should not expose TTS providers: %#v", providers)
	}
	if sttCards, err := ollama.ListSTTModels(); err != nil || len(sttCards) != 0 {
		t.Fatalf("Ollama STT models mismatch: cards=%#v err=%v", sttCards, err)
	}
	if providers := ollama.STTProviders(); len(providers) != 0 {
		t.Fatalf("Ollama should not expose STT providers: %#v", providers)
	}

	gemini := credential.NewGemini("gemini-key")
	if provider := gemini.ChatProvider(); provider.Package != "model/gemini" {
		t.Fatalf("Gemini chat provider mismatch: %#v", provider)
	}
	if providers := gemini.TTSProviders(); len(providers) != 0 {
		t.Fatalf("Gemini should not expose TTS providers: %#v", providers)
	}
	if ttsCards, err := gemini.ListTTSModels(); err != nil || len(ttsCards) != 0 {
		t.Fatalf("Gemini TTS models mismatch: cards=%#v err=%v", ttsCards, err)
	}
	if sttCards, err := gemini.ListSTTModels(); err != nil || len(sttCards) != 0 {
		t.Fatalf("Gemini STT models mismatch: cards=%#v err=%v", sttCards, err)
	}
	if providers := gemini.STTProviders(); len(providers) != 0 {
		t.Fatalf("Gemini should not expose STT providers: %#v", providers)
	}
}

func TestCredentialAdaptersReturnProviderCredentials(t *testing.T) {
	t.Parallel()

	anthropic := credential.NewAnthropic(
		" anthropic-key ",
		credential.WithBaseURL(" https://anthropic.example/v1/ "),
	)
	anthropicCredential := anthropic.ChatCredential()
	if anthropicCredential.APIKey != "anthropic-key" || anthropicCredential.BaseURL != "https://anthropic.example/v1" {
		t.Fatalf("Anthropic provider credential mismatch: %#v", anthropicCredential)
	}
	if _, err := modelanthropic.NewChatModel(anthropicCredential, "claude-sonnet-4-5"); err != nil {
		t.Fatalf("Anthropic chat model should accept credential adapter: %v", err)
	}

	dashscope := credential.NewDashScope(" dash-key ")
	chatCredential := dashscope.ChatCredential()
	if chatCredential.APIKey != "dash-key" || chatCredential.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("DashScope chat credential mismatch: %#v", chatCredential)
	}
	if _, err := modeldashscope.NewChatModel(chatCredential, "qwen-plus"); err != nil {
		t.Fatalf("DashScope chat model should accept credential adapter: %v", err)
	}
	embeddingCredential := dashscope.EmbeddingCredential()
	if embeddingCredential.APIKey != "dash-key" || embeddingCredential.BaseURL != "https://dashscope.aliyuncs.com" {
		t.Fatalf("DashScope embedding credential mismatch: %#v", embeddingCredential)
	}
	if _, err := embeddingdashscope.NewTextModel(embeddingCredential, "text-embedding-v4"); err != nil {
		t.Fatalf("DashScope embedding model should accept credential adapter: %v", err)
	}
	ttsCredential := dashscope.TTSCredential()
	if ttsCredential.APIKey != "dash-key" || ttsCredential.BaseURL != "https://dashscope.aliyuncs.com" {
		t.Fatalf("DashScope TTS credential mismatch: %#v", ttsCredential)
	}
	if _, err := ttsdashscope.NewModel(ttsCredential, "qwen3-tts-flash"); err != nil {
		t.Fatalf("DashScope TTS model should accept credential adapter: %v", err)
	}
	sttCredential := dashscope.STTCredential()
	if sttCredential.APIKey != "dash-key" || sttCredential.BaseURL != "https://dashscope.aliyuncs.com" {
		t.Fatalf("DashScope STT credential mismatch: %#v", sttCredential)
	}
	if _, err := sttdashscope.NewModel(sttCredential, "paraformer-v2"); err != nil {
		t.Fatalf("DashScope STT model should accept credential adapter: %v", err)
	}

	customDashScope := credential.NewDashScope("dash-key", credential.WithBaseURL(" https://proxy.example/v1/ "))
	if got := customDashScope.ChatCredential().BaseURL; got != "https://proxy.example/v1" {
		t.Fatalf("custom DashScope chat base URL mismatch: %q", got)
	}
	if got := customDashScope.EmbeddingCredential().BaseURL; got != "https://proxy.example/v1" {
		t.Fatalf("custom DashScope embedding base URL mismatch: %q", got)
	}
	if got := customDashScope.TTSCredential().BaseURL; got != "https://proxy.example/v1" {
		t.Fatalf("custom DashScope TTS base URL mismatch: %q", got)
	}
	if got := customDashScope.STTCredential().BaseURL; got != "https://proxy.example/v1" {
		t.Fatalf("custom DashScope STT base URL mismatch: %q", got)
	}

	moonshot := credential.NewMoonshot(" moon-key ")
	moonshotCredential := moonshot.ChatCredential()
	if moonshotCredential.APIKey != "moon-key" || moonshotCredential.BaseURL != "https://api.moonshot.cn/v1" {
		t.Fatalf("Moonshot provider credential mismatch: %#v", moonshotCredential)
	}
	if _, err := modelmoonshot.NewChatModel(moonshotCredential, "moonshot-v1-8k"); err != nil {
		t.Fatalf("Moonshot chat model should accept credential adapter: %v", err)
	}

	gemini := credential.NewGemini(" gemini-key ")
	geminiChatCredential := gemini.ChatCredential()
	if geminiChatCredential.APIKey != "gemini-key" {
		t.Fatalf("Gemini chat credential mismatch: %#v", geminiChatCredential)
	}
	if _, err := modelgemini.NewChatModel(geminiChatCredential, "gemini-2.5-flash"); err != nil {
		t.Fatalf("Gemini chat model should accept credential adapter: %v", err)
	}
	geminiEmbeddingCredential := gemini.EmbeddingCredential()
	if geminiEmbeddingCredential.APIKey != "gemini-key" {
		t.Fatalf("Gemini embedding credential mismatch: %#v", geminiEmbeddingCredential)
	}
	if _, err := embeddinggemini.NewTextModel(geminiEmbeddingCredential, "gemini-embedding-001"); err != nil {
		t.Fatalf("Gemini embedding model should accept credential adapter: %v", err)
	}

	ollama := credential.NewOllama(credential.WithHost(" http://localhost:11434/ "))
	if got := ollama.ChatCredential().Host; got != "http://localhost:11434" {
		t.Fatalf("Ollama chat host mismatch: %q", got)
	}
	if _, err := modelollama.NewChatModel(ollama.ChatCredential(), "qwen3:14b"); err != nil {
		t.Fatalf("Ollama chat model should accept credential adapter: %v", err)
	}
	if got := ollama.EmbeddingCredential().Host; got != "http://localhost:11434" {
		t.Fatalf("Ollama embedding host mismatch: %q", got)
	}
	if _, err := embeddingollama.NewTextModel(ollama.EmbeddingCredential(), "nomic-embed-text"); err != nil {
		t.Fatalf("Ollama embedding model should accept credential adapter: %v", err)
	}

	openai := credential.NewOpenAI(
		" openai-key ",
		credential.WithBaseURL(" https://openai.example/v1/ "),
		credential.WithOrganization(" org-1 "),
	)
	openaiChatCredential := openai.ChatCredential()
	if openaiChatCredential.APIKey != "openai-key" || openaiChatCredential.BaseURL != "https://openai.example/v1" || openaiChatCredential.Organization != "org-1" {
		t.Fatalf("OpenAI chat credential mismatch: %#v", openaiChatCredential)
	}
	if _, err := modelopenai.NewChatModel(openaiChatCredential, "gpt-4o-mini"); err != nil {
		t.Fatalf("OpenAI chat model should accept credential adapter: %v", err)
	}
	openaiEmbeddingCredential := openai.EmbeddingCredential()
	if openaiEmbeddingCredential.APIKey != "openai-key" || openaiEmbeddingCredential.BaseURL != "https://openai.example/v1" || openaiEmbeddingCredential.Organization != "org-1" {
		t.Fatalf("OpenAI embedding credential mismatch: %#v", openaiEmbeddingCredential)
	}
	if _, err := embeddingopenai.NewTextModel(openaiEmbeddingCredential, "text-embedding-3-small"); err != nil {
		t.Fatalf("OpenAI embedding model should accept credential adapter: %v", err)
	}
	openaiResponseCredential := openai.ResponseCredential()
	if openaiResponseCredential.APIKey != "openai-key" || openaiResponseCredential.BaseURL != "https://openai.example/v1" || openaiResponseCredential.Organization != "org-1" {
		t.Fatalf("OpenAI response credential mismatch: %#v", openaiResponseCredential)
	}
	if _, err := modelopenairesponse.NewResponseModel(openaiResponseCredential, "gpt-5.4"); err != nil {
		t.Fatalf("OpenAI response model should accept credential adapter: %v", err)
	}
}

func hasChatModel(cards []credential.ChatModelCard, name string) bool {
	for _, card := range cards {
		if card.Name == name {
			return true
		}
	}
	return false
}

func hasEmbeddingModel(cards []credential.EmbeddingModelCard, name string) bool {
	for _, card := range cards {
		if card.Name == name {
			return true
		}
	}
	return false
}

func hasTTSModel(cards []credential.TTSModelCard, name string) bool {
	for _, card := range cards {
		if card.Name == name {
			return true
		}
	}
	return false
}

func hasSTTModel(cards []credential.STTModelCard, name string) bool {
	for _, card := range cards {
		if card.Name == name {
			return true
		}
	}
	return false
}
