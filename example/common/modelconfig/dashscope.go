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
