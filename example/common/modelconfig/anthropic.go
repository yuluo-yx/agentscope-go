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

package modelconfig

import "strings"

// AnthropicConfig is the shared Anthropic runtime config used by examples.
type AnthropicConfig struct {
	APIKey  string
	Model   string
	BaseURL string
	Live    bool
}

// Anthropic resolves example model settings from unified environment variables.
//
// Priority:
// 1) AI_ANTHROPIC_API_KEY / AI_ANTHROPIC_MODEL / AI_ANTHROPIC_BASE_URL
// 2) ANTHROPIC_API_KEY / ANTHROPIC_MODEL / ANTHROPIC_BASE_URL
// 3) AI_API_KEY / AI_MODEL
// 4) demo defaults for offline examples
func Anthropic(defaultModel string) AnthropicConfig {
	apiKey := firstEnv("AI_ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY", "AI_API_KEY")
	model := firstEnv("AI_ANTHROPIC_MODEL", "ANTHROPIC_MODEL", "AI_MODEL")
	baseURL := firstEnv("AI_ANTHROPIC_BASE_URL", "ANTHROPIC_BASE_URL")

	cfg := AnthropicConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: strings.TrimSpace(baseURL),
		Live:    strings.TrimSpace(apiKey) != "",
	}
	if cfg.APIKey == "" {
		cfg.APIKey = "demo-anthropic-key"
	}
	if cfg.Model == "" {
		cfg.Model = strings.TrimSpace(defaultModel)
	}
	return cfg
}
