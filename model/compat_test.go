// Copyright 20\d\d AgentScope Go
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

package model_test

import (
	"testing"

	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestCompatibleProviderConfigValidationAndClone(t *testing.T) {
	t.Parallel()

	cfg := asmodel.NewCompatibleProviderConfig("dashscope", "qwen-plus", "secret",
		asmodel.WithBaseURL("https://dashscope.aliyuncs.com/compatible-mode/v1/"),
		asmodel.WithProviderHeader("X-Test", "1"),
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if cfg.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("base url should be normalized, got %q", cfg.BaseURL)
	}

	cloned := cfg.Clone()
	cloned.Headers["X-Test"] = "2"
	if cfg.Headers["X-Test"] != "1" {
		t.Fatalf("clone mutated original headers: %#v", cfg.Headers)
	}
}

func TestCompatibleProviderConfigRequiresCoreFields(t *testing.T) {
	t.Parallel()

	cfg := asmodel.NewCompatibleProviderConfig("", "", "")
	if err := cfg.Validate(); err == nil {
		t.Fatal("missing provider, model and api key should return validation error")
	}
}

func TestCompatibleProviderConfigOptionsAndValidationBranches(t *testing.T) {
	t.Parallel()

	cfg := asmodel.NewCompatibleProviderConfig("openai", "gpt-4.1", "secret",
		asmodel.WithMaxRetries(2),
		asmodel.WithProviderQuery("api-version", "2026-05-28"),
	)
	if cfg.MaxRetries != 2 || cfg.Query["api-version"] != "2026-05-28" {
		t.Fatalf("options not applied: %#v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config returned error: %v", err)
	}

	cfg.MaxRetries = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("max retries <= 0 should return validation error")
	}
	cfg.MaxRetries = 1
	cfg.BaseURL = "://bad"
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid base url should return validation error")
	}
	if (*asmodel.CompatibleProviderConfig)(nil).Validate() == nil {
		t.Fatal("nil config should return validation error")
	}
	if (*asmodel.CompatibleProviderConfig)(nil).Clone() != nil {
		t.Fatal("nil config clone should return nil")
	}
}

func TestCompatibleProviderOptionsInitializeNilMaps(t *testing.T) {
	t.Parallel()

	cfg := &asmodel.CompatibleProviderConfig{}
	asmodel.WithProviderHeader("X-Test", "1")(cfg)
	asmodel.WithProviderQuery("q", "1")(cfg)
	if cfg.Headers["X-Test"] != "1" || cfg.Query["q"] != "1" {
		t.Fatalf("options should initialize nil maps: %#v", cfg)
	}
}
