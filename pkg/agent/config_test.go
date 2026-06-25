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

package agent_test

import (
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
)

func TestConfigDefaultsAndValidation(t *testing.T) {
	t.Parallel()

	contextConfig := agentpkg.DefaultContextConfig()
	if contextConfig.TriggerRatio != 0.8 || contextConfig.ReserveRatio != 0.1 || contextConfig.ToolResultLimit != 50000 {
		t.Fatalf("unexpected context defaults: %#v", contextConfig)
	}
	for name, schema := range contextConfig.SummarySchema["properties"].(map[string]any) {
		property := schema.(map[string]any)
		if _, ok := property["maxLength"]; ok {
			t.Fatalf("summary schema property %q should not use maxLength: %#v", name, property)
		}
	}
	if err := contextConfig.Validate(); err != nil {
		t.Fatalf("default context config should validate: %v", err)
	}

	contextConfig.TriggerRatio = 0.95
	if err := contextConfig.Validate(); err == nil {
		t.Fatal("trigger ratio >= 0.9 should return validation error")
	}

	reactConfig := agentpkg.DefaultReActConfig()
	if reactConfig.MaxIters != 20 || reactConfig.StopOnReject {
		t.Fatalf("unexpected ReAct defaults: %#v", reactConfig)
	}

	modelConfig := agentpkg.DefaultModelConfig()
	if modelConfig.MaxRetries != 3 {
		t.Fatalf("unexpected model defaults: %#v", modelConfig)
	}
	modelConfig.MaxRetries = 0
	if err := modelConfig.Validate(); err == nil {
		t.Fatal("max retries <= 0 should return validation error")
	}
}

func TestConfigValidationFailureBranches(t *testing.T) {
	t.Parallel()

	contextConfig := agentpkg.DefaultContextConfig()
	contextConfig.ReserveRatio = 0
	if err := contextConfig.Validate(); err == nil {
		t.Fatal("reserve ratio <= 0 should return validation error")
	}
	contextConfig = agentpkg.DefaultContextConfig()
	contextConfig.ReserveRatio = 0.85
	contextConfig.TriggerRatio = 0.8
	if err := contextConfig.Validate(); err == nil {
		t.Fatal("reserve ratio >= trigger ratio should return validation error")
	}
	contextConfig = agentpkg.DefaultContextConfig()
	contextConfig.ToolResultLimit = 0
	if err := contextConfig.Validate(); err == nil {
		t.Fatal("tool result limit <= 0 should return validation error")
	}

	reactConfig := agentpkg.DefaultReActConfig()
	reactConfig.MaxIters = 0
	if err := reactConfig.Validate(); err == nil {
		t.Fatal("max iters <= 0 should return validation error")
	}
	if err := agentpkg.DefaultReActConfig().Validate(); err != nil {
		t.Fatalf("default react config should validate: %v", err)
	}
	if err := agentpkg.DefaultModelConfig().Validate(); err != nil {
		t.Fatalf("default model config should validate: %v", err)
	}
}
