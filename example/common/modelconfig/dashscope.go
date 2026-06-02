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

// Package modelconfig provides shared model runtime configuration for examples.
package modelconfig

import (
	"os"
	"strings"
)

// DashScopeConfig is the shared DashScope runtime config used by examples.
type DashScopeConfig struct {
	APIKey string
	Model  string
	Live   bool
}

// DashScope resolves example model settings from unified environment variables.
//
// Priority:
// 1) AI_DASHSCOPE_API_KEY / AI_DASHSCOPE_MODEL
// 2) AI_API_KEY / AI_MODEL
// 3) demo defaults for offline examples
func DashScope(defaultModel string) DashScopeConfig {
	apiKey := firstEnv("AI_DASHSCOPE_API_KEY", "AI_API_KEY")
	model := firstEnv("AI_DASHSCOPE_MODEL", "AI_MODEL")

	cfg := DashScopeConfig{
		APIKey: apiKey,
		Model:  model,
		Live:   strings.TrimSpace(apiKey) != "",
	}
	if cfg.APIKey == "" {
		cfg.APIKey = "demo-dashscope-key"
	}
	if cfg.Model == "" {
		cfg.Model = strings.TrimSpace(defaultModel)
	}
	return cfg
}

func firstEnv(names ...string) string {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" {
			return value
		}
	}
	return ""
}
