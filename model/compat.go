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

package model

import (
	"fmt"
	"net/url"
	"strings"
)

// CompatibleProviderConfig is the shared base config for OpenAI-compatible providers.
type CompatibleProviderConfig struct {
	ProviderName string            `json:"provider_name"`
	Model        string            `json:"model"`
	APIKey       string            `json:"-"`
	BaseURL      string            `json:"base_url,omitempty"`
	MaxRetries   int               `json:"max_retries"`
	Headers      map[string]string `json:"headers,omitempty"`
	Query        map[string]string `json:"query,omitempty"`
}

// CompatibleProviderOption configures an OpenAI-compatible provider.
type CompatibleProviderOption func(*CompatibleProviderConfig)

// NewCompatibleProviderConfig creates base config for an OpenAI-compatible provider.
func NewCompatibleProviderConfig(providerName, model, apiKey string, opts ...CompatibleProviderOption) *CompatibleProviderConfig {
	cfg := &CompatibleProviderConfig{
		ProviderName: providerName,
		Model:        model,
		APIKey:       apiKey,
		MaxRetries:   3,
		Headers:      map[string]string{},
		Query:        map[string]string{},
	}
	for _, opt := range opts {
		opt(cfg)
	}
	cfg.BaseURL = normalizeBaseURL(cfg.BaseURL)
	return cfg
}

// WithBaseURL sets the base URL for an OpenAI-compatible provider.
func WithBaseURL(baseURL string) CompatibleProviderOption {
	return func(cfg *CompatibleProviderConfig) {
		cfg.BaseURL = normalizeBaseURL(baseURL)
	}
}

// WithMaxRetries sets the provider max retry count.
func WithMaxRetries(maxRetries int) CompatibleProviderOption {
	return func(cfg *CompatibleProviderConfig) {
		cfg.MaxRetries = maxRetries
	}
}

// WithProviderHeader appends a provider request header.
func WithProviderHeader(key, value string) CompatibleProviderOption {
	return func(cfg *CompatibleProviderConfig) {
		if cfg.Headers == nil {
			cfg.Headers = map[string]string{}
		}
		cfg.Headers[key] = value
	}
}

// WithProviderQuery appends a provider request query parameter.
func WithProviderQuery(key, value string) CompatibleProviderOption {
	return func(cfg *CompatibleProviderConfig) {
		if cfg.Query == nil {
			cfg.Query = map[string]string{}
		}
		cfg.Query[key] = value
	}
}

// Validate validates base config for an OpenAI-compatible provider.
func (c *CompatibleProviderConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("agentscope/model: nil compatible provider config")
	}
	c.BaseURL = normalizeBaseURL(c.BaseURL)
	if c.ProviderName == "" {
		return fmt.Errorf("agentscope/model: provider name is empty")
	}
	if c.Model == "" {
		return fmt.Errorf("agentscope/model: model is empty")
	}
	if c.APIKey == "" {
		return fmt.Errorf("agentscope/model: api key is empty")
	}
	if c.MaxRetries <= 0 {
		return fmt.Errorf("agentscope/model: max retries must be positive")
	}
	if c.BaseURL != "" {
		parsed, err := url.Parse(c.BaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("agentscope/model: invalid base url %q", c.BaseURL)
		}
	}
	return nil
}

// Clone returns a deep copy of the provider config.
func (c *CompatibleProviderConfig) Clone() *CompatibleProviderConfig {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Headers = cloneStringMap(c.Headers)
	cp.Query = cloneStringMap(c.Query)
	return &cp
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
