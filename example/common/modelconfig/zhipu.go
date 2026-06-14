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

// ZhipuConfig is the shared Zhipu AI runtime config used by examples.
type ZhipuConfig struct {
	APIKey  string
	Model   string
	BaseURL string
	Live    bool
}

// Zhipu resolves example model settings from unified environment variables.
//
// Priority:
// 1) AI_ZHIPU_API_KEY / AI_ZHIPU_MODEL / AI_ZHIPU_BASE_URL
// 2) ZHIPU_API_KEY, ZHIPUAI_API_KEY, BIGMODEL_API_KEY / provider model and base URL envs
// 3) AI_API_KEY / AI_MODEL
// 4) demo defaults for offline examples
func Zhipu(defaultModel string) ZhipuConfig {
	apiKey := firstEnv("AI_ZHIPU_API_KEY", "ZHIPU_API_KEY", "ZHIPUAI_API_KEY", "BIGMODEL_API_KEY", "AI_API_KEY")
	model := firstEnv("AI_ZHIPU_MODEL", "ZHIPU_MODEL", "ZHIPUAI_MODEL", "BIGMODEL_MODEL", "AI_MODEL")
	baseURL := firstEnv("AI_ZHIPU_BASE_URL", "ZHIPU_BASE_URL", "ZHIPUAI_BASE_URL", "BIGMODEL_BASE_URL")

	cfg := ZhipuConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: strings.TrimSpace(baseURL),
		Live:    strings.TrimSpace(apiKey) != "",
	}
	if cfg.APIKey == "" {
		cfg.APIKey = "demo-zhipu-key"
	}
	if cfg.Model == "" {
		cfg.Model = strings.TrimSpace(defaultModel)
	}
	return cfg
}
