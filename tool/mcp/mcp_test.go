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

package mcp

import (
	"context"
	"strings"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

func TestClientListsFilteredMCPToolsAndRunsThroughToolkit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newConnectedTestClient(t, WithEnabledTools("lookup_profile", "render_badge"))
	rawTools, err := client.ListRawTools(ctx)
	if err != nil {
		t.Fatalf("ListRawTools returned error: %v", err)
	}
	assertFilteredRawTools(t, rawTools)

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	lookup := findTool(t, tools, "mcp__people__lookup_profile")
	assertLookupToolMetadata(t, lookup)
	kit, err := astool.NewToolkit(lookup)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	response, err := kit.RunTool(ctx, message.NewToolCallBlock("call-1", lookup.Name(), `{"name":"Ada"}`), asstate.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool returned error: %v", err)
	}
	if response.State != message.ToolResultSuccess {
		t.Fatalf("expected success response, got %s", response.State)
	}
	if !strings.Contains(textOutput(response.Content), "profile:Ada") {
		t.Fatalf("unexpected MCP response content: %#v", response.Content)
	}
}

func TestMCPToolConvertsDataBlocksAndToolErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := newConnectedTestClient(t)
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	badge := findTool(t, tools, "mcp__people__render_badge")
	badgeResponse := runTool(t, badge, map[string]any{"name": "Ada"})
	if badgeResponse.State != message.ToolResultSuccess {
		t.Fatalf("expected badge success, got %s", badgeResponse.State)
	}
	if !strings.Contains(textOutput(badgeResponse.Content), "badge:Ada") {
		t.Fatalf("expected text content in badge response: %#v", badgeResponse.Content)
	}
	data := firstDataBlock(badgeResponse.Content)
	if data == nil {
		t.Fatalf("expected image content to become a DataBlock")
	}
	source, ok := data.Source.(*message.Base64Source)
	if !ok {
		t.Fatalf("expected Base64Source, got %T", data.Source)
	}
	if source.MediaType != "image/png" || source.Data != "aGVsbG8=" {
		t.Fatalf("unexpected data source: %#v", source)
	}

	failTool := findTool(t, tools, "mcp__people__fail_lookup")
	failResponse := runTool(t, failTool, map[string]any{})
	if failResponse.State != message.ToolResultError {
		t.Fatalf("expected error response, got %s", failResponse.State)
	}
	if !strings.Contains(textOutput(failResponse.Content), "upstream failed") {
		t.Fatalf("unexpected error content: %#v", failResponse.Content)
	}
}

func TestClientValidationMatchesPythonMCPConstraints(t *testing.T) {
	t.Parallel()

	if _, err := NewStdioClient("bad", StdioConfig{Command: "echo"}, WithStateful(false)); err == nil {
		t.Fatalf("expected stateless stdio MCP to be rejected")
	}
	if _, err := NewInProcessClient(
		"bad",
		newTestMCPServer(),
		WithEnabledTools("lookup_profile"),
		WithDisabledTools("lookup_profile"),
	); err == nil {
		t.Fatalf("expected overlapping enabled and disabled tool filters to be rejected")
	}
}

func newTestMCPServer() *mcpserver.MCPServer {
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
	server.AddTool(
		gomcp.NewTool(
			"render_badge",
			gomcp.WithDescription("Render a small profile badge."),
			gomcp.WithString("name", gomcp.Required(), gomcp.Description("Name to render.")),
		),
		func(_ context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			return gomcp.NewToolResultImage("badge:"+request.GetString("name", "AgentScope"), "aGVsbG8=", "image/png"), nil
		},
	)
	server.AddTool(
		gomcp.NewTool("fail_lookup", gomcp.WithDescription("Return an MCP tool error.")),
		func(context.Context, gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			return gomcp.NewToolResultError("upstream failed"), nil
		},
	)
	server.AddTool(
		gomcp.NewTool("hidden_tool", gomcp.WithDescription("A filtered test tool.")),
		func(context.Context, gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			return gomcp.NewToolResultText("hidden"), nil
		},
	)
	return server
}

func newConnectedTestClient(t *testing.T, opts ...ClientOption) *Client {
	t.Helper()
	client, err := NewInProcessClient("people", newTestMCPServer(), opts...)
	if err != nil {
		t.Fatalf("NewInProcessClient returned error: %v", err)
	}
	if connectErr := client.Connect(context.Background()); connectErr != nil {
		t.Fatalf("Connect returned error: %v", connectErr)
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Fatalf("Close returned error: %v", closeErr)
		}
	})
	return client
}

func assertFilteredRawTools(t *testing.T, rawTools []gomcp.Tool) {
	t.Helper()
	if len(rawTools) != 2 {
		t.Fatalf("expected 2 filtered raw tools, got %d", len(rawTools))
	}
	for _, rawTool := range rawTools {
		if rawTool.Name == "hidden_tool" {
			t.Fatalf("disabled hidden tool leaked into filtered tools")
		}
	}
}

func assertLookupToolMetadata(t *testing.T, lookup astool.Tool) {
	t.Helper()
	if !lookup.IsMCP() || lookup.MCPName() != "people" {
		t.Fatalf("expected MCP metadata, got is_mcp=%t mcp_name=%q", lookup.IsMCP(), lookup.MCPName())
	}
	if !lookup.IsReadOnly() {
		t.Fatalf("expected readOnlyHint to mark the tool read-only")
	}
	if lookup.IsStateInjected() {
		t.Fatalf("MCP tools must not request AgentState injection")
	}
	schema := lookup.InputSchema()
	if schema["type"] != "object" {
		t.Fatalf("expected object schema, got %#v", schema["type"])
	}
	if !containsString(schema["required"], "name") {
		t.Fatalf("expected required field name in schema: %#v", schema["required"])
	}
	decision, err := lookup.CheckPermissions(context.Background(), map[string]any{"name": "Ada"}, permission.NewContext(permission.ModeDefault))
	if err != nil {
		t.Fatalf("CheckPermissions returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("expected read-only MCP tool to be allowed, got %s", decision.Behavior)
	}
}

func findTool(t *testing.T, tools []astool.Tool, name string) astool.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("missing tool %q", name)
	return nil
}

func runTool(t *testing.T, current astool.Tool, input map[string]any) *astool.ToolResponse {
	t.Helper()
	chunks, err := current.Execute(context.Background(), input, asstate.NewAgentState())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	response := astool.NewToolResponse("test-call")
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			t.Fatalf("AppendChunk returned error: %v", err)
		}
	}
	return response
}

func textOutput(blocks message.ContentBlockList) string {
	var builder strings.Builder
	for _, block := range blocks {
		if text, ok := block.(*message.TextBlock); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func firstDataBlock(blocks message.ContentBlockList) *message.DataBlock {
	for _, block := range blocks {
		if data, ok := block.(*message.DataBlock); ok {
			return data
		}
	}
	return nil
}

func containsString(value any, target string) bool {
	switch typed := value.(type) {
	case []string:
		for _, item := range typed {
			if item == target {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if item == target {
				return true
			}
		}
	}
	return false
}
