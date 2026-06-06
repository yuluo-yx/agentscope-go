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

package agentsandboxworkspacee2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
	agentsandboxworkspace "github.com/yuluo-yx/agentscope-go/workspace/agentsandbox"
)

func TestAgentSandboxWorkspaceAgentToolLoopE2E(t *testing.T) {
	if os.Getenv("AGENTSCOPE_E2E_AGENT_SANDBOX") != "1" {
		t.Skip("set AGENTSCOPE_E2E_AGENT_SANDBOX=1 to run the Agent Sandbox workspace E2E test")
	}

	ctx := context.Background()
	ws, err := agentsandboxworkspace.NewWorkspace(agentSandboxOptions(t, t.TempDir())...)
	requireNoErr(t, err, "NewWorkspace returned error")
	requireNoErr(t, ws.Initialize(ctx), "Initialize returned error")
	t.Cleanup(func() {
		requireNoErr(t, ws.Close(context.Background()), "Close returned error")
	})

	tools, err := ws.ListTools(ctx)
	requireNoErr(t, err, "ListTools returned error")
	kit, err := tool.NewToolkit(tools...)
	requireNoErr(t, err, "NewToolkit returned error")

	notePath := agentSandboxFilePath("/home/user/data/e2e-note.txt")
	noteText := "agent sandbox workspace note\ncreated by agent e2e"
	model := &scriptedAgentSandboxChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("write-call", "Write", jsonInput(t, map[string]any{
				"file_path": notePath,
				"content":   noteText,
			}))},
			true,
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("read-call", "Read", jsonInput(t, map[string]any{
				"file_path": notePath,
				"limit":     5,
			}))},
			true,
		),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("agent sandbox workspace verified")}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	state.PermissionContext.WorkingDirectories["agent-sandbox-workspace"] = permission.AdditionalWorkingDirectory{
		Path:   "/home/user",
		Source: "e2e",
	}
	agent, err := agentpkg.NewAgent("Friday", "Use Agent Sandbox workspace tools.", model, agentpkg.WithToolkit(kit), agentpkg.WithAgentState(state))
	requireNoErr(t, err, "NewAgent returned error")
	userMsg, err := message.NewUserMessage("Tony", "Create and read an Agent Sandbox workspace note")
	requireNoErr(t, err, "NewUserMessage returned error")

	reply, err := agent.Reply(ctx, userMsg)
	requireNoErr(t, err, "Reply returned error")

	if text := reply.GetTextContent(""); text == nil || *text != "agent sandbox workspace verified" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
	if len(model.requests) != 3 {
		t.Fatalf("expected write, read, and final model calls, got %d", len(model.requests))
	}
	result := lastToolResultFromLastModelRequest(t, model)
	if text := result.Output.Blocks.GetTextContent(""); result.Name != "Read" || text == nil || !strings.Contains(*text, "agent sandbox workspace note") {
		t.Fatalf("read tool result should be passed back to the final model call, got %#v text=%#v", result, text)
	}
}

func agentSandboxOptions(t *testing.T, hostWorkdir string) []agentsandboxworkspace.Option {
	t.Helper()

	template := envOrDefault("AGENTSCOPE_AGENT_SANDBOX_TEMPLATE", "python-sandbox-template")
	namespace := envOrDefault("AGENTSCOPE_AGENT_SANDBOX_NAMESPACE", "default")
	opts := []agentsandboxworkspace.Option{
		agentsandboxworkspace.WithWorkspaceID("agent-sandbox-e2e"),
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

type scriptedAgentSandboxChatModel struct {
	responses []*modelpkg.ChatResponse
	requests  []modelpkg.CallRequest
}

func (m *scriptedAgentSandboxChatModel) Name() string {
	return "scripted-agent-sandbox-workspace-e2e"
}

func (m *scriptedAgentSandboxChatModel) Call(_ context.Context, request modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted Agent Sandbox workspace model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func (m *scriptedAgentSandboxChatModel) Stream(ctx context.Context, request modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted Agent Sandbox workspace model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	ch := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(ch)
		delta := response.Clone()
		delta.IsLast = false
		delta.Usage = nil
		select {
		case ch <- *delta:
		case <-ctx.Done():
			return
		}
		select {
		case ch <- *response.Clone():
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (m *scriptedAgentSandboxChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func jsonInput(t *testing.T, value map[string]any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return string(data)
}

func lastToolResultFromLastModelRequest(t *testing.T, model *scriptedAgentSandboxChatModel) *message.ToolResultBlock {
	t.Helper()

	if len(model.requests) == 0 {
		t.Fatal("model has no recorded requests")
	}
	request := model.requests[len(model.requests)-1]
	if len(request.Messages) == 0 {
		t.Fatalf("last request has no messages: %#v", request)
	}
	last := request.Messages[len(request.Messages)-1]
	blocks := last.GetContentBlocks("tool_result")
	if len(blocks) == 0 {
		t.Fatalf("expected at least one tool result in last model request, got %#v", last.Content)
	}
	result, ok := blocks[len(blocks)-1].(*message.ToolResultBlock)
	if !ok {
		t.Fatalf("tool_result block has unexpected type %T", blocks[len(blocks)-1])
	}
	return result
}

func requireNoErr(t *testing.T, err error, message string) {
	t.Helper()

	if err != nil {
		t.Fatalf("%s: %v", message, err)
	}
}

var _ asworkspace.Workspace = (*agentsandboxworkspace.Workspace)(nil)
