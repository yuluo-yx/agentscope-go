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

package agentsandboxworkspaceintegration_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/test/integration/workspace/internal/workspacetest"
	agentsandboxworkspace "github.com/yuluo-yx/agentscope-go/workspace/agentsandbox"
)

func TestAgentSandboxWorkspaceToolAndOffloadIntegration(t *testing.T) {
	if os.Getenv("AGENTSCOPE_TEST_AGENT_SANDBOX") != "1" {
		t.Skip("set AGENTSCOPE_TEST_AGENT_SANDBOX=1 to run the Agent Sandbox workspace integration test")
	}

	ctx := context.Background()
	ws, err := agentsandboxworkspace.NewWorkspace(agentSandboxOptions(t, t.TempDir())...)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	workspacetest.ExerciseToolsAndOffload(t, ctx, ws, agentSandboxFilePath("/home/user/data/brief.txt"))
}

func agentSandboxOptions(t *testing.T, hostWorkdir string) []agentsandboxworkspace.Option {
	t.Helper()

	template := envOrDefault("AGENTSCOPE_AGENT_SANDBOX_TEMPLATE", "python-sandbox-template")
	namespace := envOrDefault("AGENTSCOPE_AGENT_SANDBOX_NAMESPACE", "default")
	opts := []agentsandboxworkspace.Option{
		agentsandboxworkspace.WithWorkspaceID("agent-sandbox-integration"),
		agentsandboxworkspace.WithTemplateName(template),
		agentsandboxworkspace.WithNamespace(namespace),
		agentsandboxworkspace.WithHostWorkdir(hostWorkdir),
	}
	if apiURL := strings.TrimSpace(os.Getenv("AGENTSCOPE_AGENT_SANDBOX_API_URL")); apiURL != "" {
		opts = append(opts, agentsandboxworkspace.WithAPIURL(apiURL))
		return opts
	}
	if gateway := strings.TrimSpace(os.Getenv("AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAME")); gateway != "" {
		opts = append(opts, agentsandboxworkspace.WithGateway(
			gateway,
			envOrDefault("AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAMESPACE", "default"),
		))
	}
	return opts
}

func agentSandboxFilePath(fallback string) string {
	return envOrDefault("AGENTSCOPE_AGENT_SANDBOX_TEST_FILE", fallback)
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
