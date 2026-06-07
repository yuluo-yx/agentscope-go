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

package provider_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/model/gemini"
	"github.com/yuluo-yx/agentscope-go/model/openai"
	"github.com/yuluo-yx/agentscope-go/model/openairesponse"
)

func TestOpenAIChatSmoke(t *testing.T) {
	requireEnvEnabled(t, "AGENTSCOPE_TEST_OPENAI")
	apiKey := requireAnyEnv(t, "OPENAI_API_KEY")
	modelName := envOr("AGENTSCOPE_TEST_OPENAI_MODEL", "gpt-4o-mini")

	model, err := openai.NewChatModel(openai.NewCredential(apiKey), modelName, openai.WithMaxRetries(1), openai.WithStream(false))
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	assertSmokeResponse(t, model)
}

func TestOpenAIResponsesSmoke(t *testing.T) {
	requireEnvEnabled(t, "AGENTSCOPE_TEST_OPENAI_RESPONSES")
	apiKey := requireAnyEnv(t, "OPENAI_API_KEY")
	modelName := envOr("AGENTSCOPE_TEST_OPENAI_RESPONSES_MODEL", "gpt-5.4")

	model, err := openairesponse.NewResponseModel(openairesponse.NewCredential(apiKey), modelName)
	if err != nil {
		t.Fatalf("NewResponseModel returned error: %v", err)
	}
	assertSmokeResponse(t, model)
}

func TestDashScopeSmoke(t *testing.T) {
	requireEnvEnabled(t, "AGENTSCOPE_TEST_DASHSCOPE")
	apiKey := requireAnyEnv(t, "DASHSCOPE_API_KEY", "AI_DASHSCOPE_API_KEY")
	modelName := envOr("AGENTSCOPE_TEST_DASHSCOPE_MODEL", "qwen-plus")

	model, err := dashscope.NewChatModel(dashscope.NewCredential(apiKey), modelName, dashscope.WithMaxRetries(1), dashscope.WithStream(false))
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	assertSmokeResponse(t, model)
}

func TestGeminiSmoke(t *testing.T) {
	requireEnvEnabled(t, "AGENTSCOPE_TEST_GEMINI")
	apiKey := requireAnyEnv(t, "GEMINI_API_KEY", "GOOGLE_API_KEY")
	modelName := envOr("AGENTSCOPE_TEST_GEMINI_MODEL", "gemini-2.5-flash")

	model, err := gemini.NewChatModel(gemini.NewCredential(apiKey), modelName)
	if err != nil {
		t.Fatalf("NewChatModel returned error: %v", err)
	}
	assertSmokeResponse(t, model)
}

func assertSmokeResponse(t *testing.T, model asmodel.ChatModel) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	userMsg, err := message.NewUserMessage("provider-smoke", "Reply with the single word ok.")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	resp, err := model.Call(ctx, asmodel.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		t.Fatalf("%s Call returned error: %v", model.Name(), err)
	}
	if resp == nil || len(resp.Content) == 0 {
		t.Fatalf("%s returned empty smoke response: %#v", model.Name(), resp)
	}
}

func requireEnvEnabled(t *testing.T, name string) {
	t.Helper()
	if os.Getenv(name) != "1" {
		t.Skipf("set %s=1 to run this real provider smoke test", name)
	}
}

func requireAnyEnv(t *testing.T, names ...string) string {
	t.Helper()
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	t.Fatalf("one of %v must be set", names)
	return ""
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
