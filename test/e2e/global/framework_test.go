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

package global_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	asmcp "github.com/yuluo-yx/agentscope-go/tool/mcp"
	"github.com/yuluo-yx/agentscope-go/workspace"
)

func TestGlobalAgentStreamFunctionToolE2E(t *testing.T) {
	t.Parallel()

	greet, err := tool.NewFunctionTool(
		"Greet",
		"Return a greeting for one name.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"required": []string{"name"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			name, _ := input["name"].(string)
			return message.ContentBlockList{message.NewTextBlock("hello " + name)}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
	requireNoErr(t, err, "NewFunctionTool returned error")
	kit, err := tool.NewToolkit(greet)
	requireNoErr(t, err, "NewToolkit returned error")
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("greet-call", "Greet", jsonInput(t, map[string]any{"name": "Ada"}))},
			true,
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("hello Ada acknowledged", message.WithBlockID("final-text"))},
			true,
		),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeExplore)
	agent, err := agentpkg.NewAgent("Friday", "Use tools when useful.", model, agentpkg.WithToolkit(kit), agentpkg.WithAgentState(state))
	requireNoErr(t, err, "NewAgent returned error")
	userMsg, err := message.NewUserMessage("Tony", "Greet Ada")
	requireNoErr(t, err, "NewUserMessage returned error")

	var events []message.Event
	replyErr := agent.ReplyStream(context.Background(), userMsg, func(evt message.Event) error {
		events = append(events, evt)
		return nil
	})
	requireNoErr(t, replyErr, "ReplyStream returned error")

	assertEventOrder(t, events, message.ToolCallStartType, message.ToolResultEndType, message.TextBlockDeltaType, message.ReplyEndType)
	if text := lastAssistantText(t, agent); text != "hello Ada acknowledged" {
		t.Fatalf("final assistant text mismatch: %q", text)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model should be called before and after tool execution, got %d calls", len(model.requests))
	}
	result := onlyToolResultFromLastModelRequest(t, model)
	if result.State != message.ToolResultSuccess || blocksText(result.Output.Blocks) != "hello Ada" {
		t.Fatalf("unexpected tool result passed back to model: %#v", result)
	}
}

func TestGlobalPermissionConfirmationResumeE2E(t *testing.T) {
	t.Parallel()

	executed := false
	publish, err := tool.NewFunctionTool(
		"Publish",
		"Publish one topic after user confirmation.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic": map[string]any{"type": "string"},
			},
			"required": []string{"topic"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			executed = true
			topic, _ := input["topic"].(string)
			return message.ContentBlockList{message.NewTextBlock("published " + topic)}, nil
		},
	)
	requireNoErr(t, err, "NewFunctionTool returned error")
	kit, err := tool.NewToolkit(publish)
	requireNoErr(t, err, "NewToolkit returned error")
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("publish-call", "Publish", jsonInput(t, map[string]any{"topic": "release"}))},
			true,
		),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("release published")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Ask before publishing.", model, agentpkg.WithToolkit(kit))
	requireNoErr(t, err, "NewAgent returned error")
	userMsg, err := message.NewUserMessage("Tony", "Publish the release")
	requireNoErr(t, err, "NewUserMessage returned error")

	var confirm *message.RequireUserConfirmEvent
	replyStreamErr := agent.ReplyStream(context.Background(), userMsg, func(evt message.Event) error {
		if e, ok := evt.(*message.RequireUserConfirmEvent); ok {
			confirm = e
		}
		return nil
	})
	requireNoErr(t, replyStreamErr, "initial ReplyStream returned error")
	if executed {
		t.Fatal("tool should not execute before user confirmation")
	}
	if confirm == nil || len(confirm.ToolCalls) != 1 {
		t.Fatalf("expected one confirmation event, got %#v", confirm)
	}
	if len(model.requests) != 1 {
		t.Fatalf("model should pause after the first tool request, got %d calls", len(model.requests))
	}

	reply, err := agent.Reply(context.Background(), message.NewUserConfirmResultEvent(confirm.ReplyID(), []message.ConfirmResult{{
		Confirmed: true,
		ToolCall:  confirm.ToolCalls[0],
		Rules:     confirm.ToolCalls[0].SuggestedRules,
	}}))
	requireNoErr(t, err, "resume Reply returned error")
	if !executed {
		t.Fatal("confirmed tool call should execute on resume")
	}
	if text := reply.GetTextContent(""); text == nil || *text != "release published" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
	result := onlyToolResultFromLastModelRequest(t, model)
	if result.State != message.ToolResultSuccess || blocksText(result.Output.Blocks) != "published release" {
		t.Fatalf("unexpected resumed tool result: %#v", result)
	}
}

func TestGlobalWorkspaceFileToolsE2E(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	ws, err := workspace.NewLocalWorkspace(workdir, workspace.WithWorkspaceID("global-workspace-e2e"))
	requireNoErr(t, err, "NewLocalWorkspace returned error")
	requireNoErr(t, ws.Initialize(ctx), "Initialize returned error")
	t.Cleanup(func() {
		requireNoErr(t, ws.Close(context.Background()), "Close returned error")
	})
	tools, err := ws.ListTools(ctx)
	requireNoErr(t, err, "ListTools returned error")
	kit, err := tool.NewToolkit(tools...)
	requireNoErr(t, err, "NewToolkit returned error")
	notePath := filepath.Join(workdir, "notes.txt")
	noteText := "workspace note\ncreated by e2e"
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
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
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("workspace note verified")}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	state.PermissionContext.WorkingDirectories["workspace"] = permission.AdditionalWorkingDirectory{Path: workdir, Source: "e2e"}
	agent, err := agentpkg.NewAgent("Friday", "Use workspace tools.", model, agentpkg.WithToolkit(kit), agentpkg.WithAgentState(state))
	requireNoErr(t, err, "NewAgent returned error")
	userMsg, err := message.NewUserMessage("Tony", "Create and read a workspace note")
	requireNoErr(t, err, "NewUserMessage returned error")

	reply, err := agent.Reply(ctx, userMsg)
	requireNoErr(t, err, "Reply returned error")

	if text := reply.GetTextContent(""); text == nil || *text != "workspace note verified" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
	data, err := os.ReadFile(notePath)
	requireNoErr(t, err, "workspace file was not written")
	if string(data) != noteText {
		t.Fatalf("workspace file content mismatch: %q", string(data))
	}
	if len(model.requests) != 3 {
		t.Fatalf("expected write, read, and final model calls, got %d", len(model.requests))
	}
	result := lastToolResultFromLastModelRequest(t, model)
	if result.Name != "Read" || !strings.Contains(blocksText(result.Output.Blocks), "workspace note") {
		t.Fatalf("read tool result should be passed back to the final model call, got %#v", result)
	}
}

func TestGlobalMCPToolAgentE2E(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client, err := asmcp.NewInProcessClient("people", newPeopleMCPServer())
	requireNoErr(t, err, "NewInProcessClient returned error")
	requireNoErr(t, client.Connect(ctx), "Connect returned error")
	t.Cleanup(func() {
		requireNoErr(t, client.Close(), "Close returned error")
	})
	tools, err := client.ListTools(ctx)
	requireNoErr(t, err, "ListTools returned error")
	kit, err := tool.NewToolkit(tools...)
	requireNoErr(t, err, "NewToolkit returned error")
	lookup := findToolByName(t, tools, "mcp__people__lookup_profile")
	if !lookup.IsMCP() || lookup.MCPName() != "people" || !lookup.IsReadOnly() {
		t.Fatalf("unexpected MCP metadata: is_mcp=%t mcp=%q read_only=%t", lookup.IsMCP(), lookup.MCPName(), lookup.IsReadOnly())
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("mcp-call", lookup.Name(), jsonInput(t, map[string]any{"name": "Ada"}))},
			true,
		),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("profile loaded")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Use MCP tools.", model, agentpkg.WithToolkit(kit))
	requireNoErr(t, err, "NewAgent returned error")
	userMsg, err := message.NewUserMessage("Tony", "Load Ada profile")
	requireNoErr(t, err, "NewUserMessage returned error")

	reply, err := agent.Reply(ctx, userMsg)
	requireNoErr(t, err, "Reply returned error")

	if text := reply.GetTextContent(""); text == nil || *text != "profile loaded" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
	if !requestIncludesTool(model.requests[0], lookup.Name()) {
		t.Fatalf("initial model request should expose MCP schema: %#v", model.requests[0].Tools)
	}
	result := onlyToolResultFromLastModelRequest(t, model)
	if result.State != message.ToolResultSuccess || blocksText(result.Output.Blocks) != "profile:Ada" {
		t.Fatalf("unexpected MCP tool result passed back to model: %#v", result)
	}
}

func TestGlobalWorkspaceOffloadMessageAndToolDataE2E(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	ws, err := workspace.NewLocalWorkspace(workdir)
	requireNoErr(t, err, "NewLocalWorkspace returned error")
	requireNoErr(t, ws.Initialize(ctx), "Initialize returned error")
	userMsg, err := message.NewUserMessage("Tony", message.ContentBlockList{
		message.NewTextBlock("Inspect this badge."),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "image/png"), message.WithDataBlockName("badge.png")),
	})
	requireNoErr(t, err, "NewUserMessage returned error")

	contextPath, err := ws.OffloadContext(ctx, "session-1", []*message.Message{userMsg})
	requireNoErr(t, err, "OffloadContext returned error")
	contextData, err := os.ReadFile(contextPath)
	requireNoErr(t, err, "reading offloaded context failed")
	if strings.Contains(string(contextData), `"type":"base64"`) || !strings.Contains(string(contextData), `"type":"url"`) {
		t.Fatalf("offloaded context should replace base64 data with a URL source: %s", contextData)
	}

	resultPath, err := ws.OffloadToolResult(ctx, "session-1", message.NewToolResultBlock(
		"badge-call",
		"RenderBadge",
		message.ToolResultOutput{Blocks: message.ContentBlockList{
			message.NewTextBlock("badge ready"),
			message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "image/png"), message.WithDataBlockName("badge.png")),
		}},
		message.ToolResultSuccess,
	))
	requireNoErr(t, err, "OffloadToolResult returned error")
	resultData, err := os.ReadFile(resultPath)
	requireNoErr(t, err, "reading offloaded tool result failed")
	if !strings.Contains(string(resultData), "badge ready") || !strings.Contains(string(resultData), "<data url='file://") {
		t.Fatalf("offloaded tool result should include text and data URL reference: %s", resultData)
	}
	dataFiles, err := os.ReadDir(filepath.Join(workdir, "data"))
	requireNoErr(t, err, "reading workspace data dir failed")
	if len(dataFiles) != 1 {
		t.Fatalf("expected one de-duplicated data file, got %d", len(dataFiles))
	}
	offloadedBytes, err := os.ReadFile(filepath.Join(workdir, "data", dataFiles[0].Name()))
	requireNoErr(t, err, "reading offloaded data file failed")
	if string(offloadedBytes) != "hello" {
		t.Fatalf("offloaded base64 payload mismatch: %q", string(offloadedBytes))
	}
}

func newPeopleMCPServer() *mcpserver.MCPServer {
	server := mcpserver.NewMCPServer(
		"people-server",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	server.AddTool(
		gomcp.NewTool(
			"lookup_profile",
			gomcp.WithDescription("Look up one profile by name."),
			gomcp.WithReadOnlyHintAnnotation(true),
			gomcp.WithString("name", gomcp.Required(), gomcp.Description("Name to look up.")),
		),
		func(_ context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			return gomcp.NewToolResultText("profile:" + request.GetString("name", "AgentScope")), nil
		},
	)
	return server
}

func jsonInput(t *testing.T, value map[string]any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return string(data)
}

func assertEventOrder(t *testing.T, events []message.Event, expected ...message.EventType) {
	t.Helper()
	next := 0
	for _, event := range events {
		if next < len(expected) && event.GetType() == expected[next] {
			next++
		}
	}
	if next != len(expected) {
		types := make([]message.EventType, 0, len(events))
		for _, event := range events {
			types = append(types, event.GetType())
		}
		t.Fatalf("event order mismatch: expected subsequence %v in %v", expected, types)
	}
}

func lastAssistantText(t *testing.T, agent *agentpkg.Agent) string {
	t.Helper()
	state := agent.AgentState()
	if state == nil || len(state.Context) == 0 {
		t.Fatal("agent state has no context")
	}
	last := state.Context[len(state.Context)-1]
	if last.Role != message.RoleAssistant {
		t.Fatalf("last message should be assistant, got %s", last.Role)
	}
	text := last.GetTextContent("")
	if text == nil {
		return ""
	}
	return *text
}

func onlyToolResultFromLastModelRequest(t *testing.T, model *scriptedChatModel) *message.ToolResultBlock {
	t.Helper()
	blocks := toolResultsFromLastModelRequest(t, model)
	if len(blocks) != 1 {
		t.Fatalf("expected one tool result in last model request, got %#v", lastModelRequestMessage(t, model).Content)
	}
	return blocks[0]
}

func lastToolResultFromLastModelRequest(t *testing.T, model *scriptedChatModel) *message.ToolResultBlock {
	t.Helper()
	blocks := toolResultsFromLastModelRequest(t, model)
	if len(blocks) == 0 {
		t.Fatalf("expected at least one tool result in last model request, got %#v", lastModelRequestMessage(t, model).Content)
	}
	return blocks[len(blocks)-1]
}

func toolResultsFromLastModelRequest(t *testing.T, model *scriptedChatModel) []*message.ToolResultBlock {
	t.Helper()
	msg := lastModelRequestMessage(t, model)
	blocks := msg.GetContentBlocks("tool_result")
	results := make([]*message.ToolResultBlock, 0, len(blocks))
	for _, block := range blocks {
		result, ok := block.(*message.ToolResultBlock)
		if !ok {
			t.Fatalf("tool_result block has unexpected type %T", block)
		}
		results = append(results, result)
	}
	return results
}

func lastModelRequestMessage(t *testing.T, model *scriptedChatModel) *message.Message {
	t.Helper()
	if len(model.requests) == 0 {
		t.Fatal("model has no recorded requests")
	}
	request := model.requests[len(model.requests)-1]
	if len(request.Messages) == 0 {
		t.Fatalf("last request has no messages: %#v", request)
	}
	return request.Messages[len(request.Messages)-1]
}

func blocksText(blocks message.ContentBlockList) string {
	var builder strings.Builder
	for _, block := range blocks {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func findToolByName(t *testing.T, tools []tool.Tool, name string) tool.Tool {
	t.Helper()
	for _, current := range tools {
		if current.Name() == name {
			return current
		}
	}
	t.Fatalf("missing tool %q", name)
	return nil
}

func requireNoErr(t *testing.T, err error, message string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", message, err)
	}
}
