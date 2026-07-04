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
	"sync"
	"testing"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
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
	if text := response.GetTextContent(""); text == nil || !strings.Contains(*text, "profile:Ada") {
		t.Fatalf("unexpected MCP response content: %#v", text)
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
	if text := badgeResponse.GetTextContent(""); text == nil || !strings.Contains(*text, "badge:Ada") {
		t.Fatalf("expected text content in badge response: %#v", text)
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
	if text := failResponse.GetTextContent(""); text == nil || !strings.Contains(*text, "upstream failed") {
		t.Fatalf("unexpected error content: %#v", text)
	}
}

func TestMCPToolMatchesServerScopedPermissionRules(t *testing.T) {
	t.Parallel()

	client := newConnectedTestClient(t)
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	badge := findTool(t, tools, "mcp__people__render_badge")
	if !badge.MatchRule("mcp:people", map[string]any{"name": "Ada"}) {
		t.Fatalf("server-scoped MCP rule should match tool from people server")
	}
	if badge.MatchRule("mcp:other", map[string]any{"name": "Ada"}) {
		t.Fatalf("server-scoped MCP rule should not match another server")
	}

	suggestions := badge.GenerateSuggestions(map[string]any{"name": "Ada"})
	if !hasRuleContent(suggestions, "mcp:people") {
		t.Fatalf("MCP suggestions should include server-scoped rule: %#v", suggestions)
	}

	engine := permission.NewEngine(permission.NewContext(permission.ModeDefault))
	engine.AddRule(permission.Rule{
		ToolName:    badge.Name(),
		RuleContent: "mcp:people",
		Behavior:    permission.BehaviorDeny,
	})
	decision, err := engine.CheckPermission(context.Background(), badge, map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatalf("CheckPermission returned error: %v", err)
	}
	if decision.Behavior != permission.BehaviorDeny {
		t.Fatalf("server-scoped MCP deny rule should deny badge tool, got %#v", decision)
	}
}

func TestStatelessInProcessClientUsesEphemeralConnections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client, err := NewInProcessClient(
		"ephemeral",
		newTestMCPServer(),
		WithStateful(false),
		WithDisabledTools("hidden_tool"),
		WithExecutionTimeout(time.Second),
	)
	if err != nil {
		t.Fatalf("NewInProcessClient returned error: %v", err)
	}
	if client.IsStateful() || client.IsConnected() {
		t.Fatalf("stateless client should not report persistent state")
	}
	rawTools, err := client.ListRawTools(ctx)
	if err != nil {
		t.Fatalf("ListRawTools returned error: %v", err)
	}
	if len(rawTools) != 3 {
		t.Fatalf("hidden tool should be filtered from stateless raw tools: %#v", rawTools)
	}
	wrapped, err := client.GetTool(ctx, "lookup_profile")
	if err != nil {
		t.Fatalf("GetTool returned error: %v", err)
	}
	response := runTool(t, wrapped, map[string]any{"name": "Grace"})
	if response.State != message.ToolResultSuccess || response.GetTextContent("") == nil || !strings.Contains(*response.GetTextContent(""), "profile:Grace") {
		t.Fatalf("stateless wrapped tool response mismatch: %#v", response)
	}
	result, err := client.CallTool(ctx, "lookup_profile", map[string]any{"name": "Lin"})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatalf("CallTool should return MCP content: %#v", result)
	}
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("stateless Connect should be a no-op: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("stateless Close should be a no-op: %v", err)
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

func TestToolListChangedNotificationClearsCachedTools(t *testing.T) {
	t.Parallel()

	events := make(chan ToolListChangedEvent, 1)
	client := newConnectedTestClient(t, WithToolListChangedHandler(func(event ToolListChangedEvent) {
		events <- event
	}))
	if _, err := client.GetTool(context.Background(), "lookup_profile"); err != nil {
		t.Fatalf("GetTool returned error: %v", err)
	}
	client.mu.Lock()
	if client.cachedTools == nil {
		t.Fatalf("GetTool should populate cached raw tools")
	}
	client.mu.Unlock()

	client.handleNotification(gomcp.JSONRPCNotification{
		Notification: gomcp.Notification{Method: string(gomcp.MethodNotificationToolsListChanged)},
	})

	select {
	case event := <-events:
		if event.ClientName != "people" {
			t.Fatalf("unexpected event client name: %q", event.ClientName)
		}
		if event.Notification.Method != string(gomcp.MethodNotificationToolsListChanged) {
			t.Fatalf("unexpected event method: %q", event.Notification.Method)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for tool list changed callback")
	}
	client.mu.Lock()
	cached := client.cachedTools
	client.mu.Unlock()
	if cached != nil {
		t.Fatalf("tool list changed notification should clear cached tools")
	}
}

func TestCapabilityBoundariesMatchPythonMCPImplementation(t *testing.T) {
	t.Parallel()

	boundaries := CapabilityBoundaries()
	assertFeatureBoundary(t, boundaries, FeatureOAuthAuth, FeatureStatusPartial, "runtime OAuthConfig")
	assertFeatureBoundary(t, boundaries, FeatureToolListChangedNotification, FeatureStatusPartial, "clears cached raw tools")
	assertFeatureBoundary(t, boundaries, FeatureDeferredLoading, FeatureStatusSupported, "DeferredToolkit")
	assertFeatureBoundary(t, boundaries, FeatureTaskAugmentedTools, FeatureStatusSupported, "WithTaskTTL")
}

func TestDeferredToolkitLoadsToolsOnDemandAndCanInvalidate(t *testing.T) {
	t.Parallel()

	echo, err := astool.NewFunctionTool(
		"Echo",
		"Echo one value.",
		map[string]any{"type": "object"},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			value, _ := input["value"].(string)
			return message.ContentBlockList{message.NewTextBlock(value)}, nil
		},
		astool.WithFunctionReadOnly(true),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	loader := &recordingToolLoader{name: "lazy", tools: []astool.Tool{echo}}
	kit, err := NewDeferredToolkit(loader)
	if err != nil {
		t.Fatalf("NewDeferredToolkit returned error: %v", err)
	}
	if loader.ListCount() != 0 {
		t.Fatalf("deferred toolkit should not list tools during construction, got %d", loader.ListCount())
	}

	schemas, err := kit.ToolSchemas()
	if err != nil {
		t.Fatalf("ToolSchemas returned error: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Function.Name != "Echo" {
		t.Fatalf("unexpected deferred schemas: %#v", schemas)
	}
	if loader.ListCount() != 1 {
		t.Fatalf("first ToolSchemas should list once, got %d", loader.ListCount())
	}
	if found, ok := kit.FindTool("Echo"); !ok || found.Name() != "Echo" {
		t.Fatalf("FindTool should use cached tools, got %#v %v", found, ok)
	}
	if loader.ListCount() != 1 {
		t.Fatalf("FindTool should not reload cached tools, got %d", loader.ListCount())
	}

	response, err := kit.RunTool(context.Background(), message.NewToolCallBlock("call-echo", "Echo", `{"value":"hello"}`), asstate.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool returned error: %v", err)
	}
	if text := response.GetTextContent(""); text == nil || *text != "hello" {
		t.Fatalf("deferred toolkit response mismatch: %#v", response)
	}
	chunks, err := kit.CallTool(context.Background(), message.NewToolCallBlock("call-echo-2", "Echo", `{"value":"again"}`), asstate.NewAgentState())
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if chunks == nil {
		t.Fatal("CallTool should return chunks")
	}
	for range chunks {
	}

	kit.Invalidate()
	if _, err := kit.ToolSchemas(); err != nil {
		t.Fatalf("ToolSchemas after invalidate returned error: %v", err)
	}
	if loader.ListCount() != 2 {
		t.Fatalf("ToolSchemas after invalidate should reload, got %d", loader.ListCount())
	}
}

func TestDeferredToolkitErrorBranches(t *testing.T) {
	t.Parallel()

	if _, err := NewDeferredToolkit(nil); err == nil {
		t.Fatal("nil deferred loader should fail")
	}
	var nilKit *DeferredToolkit
	nilKit.Invalidate()
	if _, err := nilKit.CallTool(context.Background(), message.NewToolCallBlock("call", "Missing", "{}"), asstate.NewAgentState()); err == nil {
		t.Fatal("nil deferred toolkit CallTool should fail")
	}

	loader := &recordingToolLoader{name: "broken", err: context.Canceled}
	kit, err := NewDeferredToolkit(loader)
	if err != nil {
		t.Fatalf("NewDeferredToolkit returned error: %v", err)
	}
	if _, err := kit.ToolSchemas(); err == nil {
		t.Fatal("deferred ToolSchemas should return loader errors")
	}
	if found, ok := kit.FindTool("Missing"); ok || found != nil {
		t.Fatalf("FindTool should miss when loading fails, got %#v %v", found, ok)
	}
	if _, err := kit.RunTool(context.Background(), message.NewToolCallBlock("call", "Missing", "{}"), asstate.NewAgentState()); err == nil {
		t.Fatal("RunTool should return loader errors")
	}
}

func TestClientCallToolIncludesTaskTTL(t *testing.T) {
	t.Parallel()

	captured := make(chan *gomcp.TaskParams, 1)
	server := mcpserver.NewMCPServer("task-server", "1.0.0", mcpserver.WithToolCapabilities(false))
	server.AddTool(
		gomcp.NewTool(
			"capture_task",
			gomcp.WithDescription("Capture task parameters."),
			gomcp.WithString("name", gomcp.Required(), gomcp.Description("Name to echo.")),
		),
		func(_ context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			captured <- request.Params.Task
			return gomcp.NewToolResultText("task:" + request.GetString("name", "AgentScope")), nil
		},
	)
	client, err := NewInProcessClient("tasks", server, WithTaskTTL(5*time.Second))
	if err != nil {
		t.Fatalf("NewInProcessClient returned error: %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	result, err := client.CallTool(context.Background(), "capture_task", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	if text := ConvertToolResult(result).GetTextContent(""); text == nil || *text != "task:Ada" {
		t.Fatalf("unexpected tool result: %#v", result)
	}
	select {
	case task := <-captured:
		if task == nil || task.TTL == nil || *task.TTL != 5000 {
			t.Fatalf("task TTL was not sent in MCP call: %#v", task)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for captured task params")
	}
}

func TestClientTaskTTLValidationAndZeroTTL(t *testing.T) {
	t.Parallel()

	if _, err := NewInProcessClient("badttl", newTestMCPServer(), WithTaskTTL(-time.Second)); err == nil {
		t.Fatal("negative task TTL should be rejected")
	}

	captured := make(chan *gomcp.TaskParams, 1)
	server := mcpserver.NewMCPServer("task-server", "1.0.0", mcpserver.WithToolCapabilities(false))
	server.AddTool(
		gomcp.NewTool("capture_task", gomcp.WithString("name")),
		func(_ context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			captured <- request.Params.Task
			return gomcp.NewToolResultText("ok"), nil
		},
	)
	client, err := NewInProcessClient("taskszero", server, WithTaskTTL(0))
	if err != nil {
		t.Fatalf("NewInProcessClient returned error: %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	if _, err := client.CallTool(context.Background(), "capture_task", map[string]any{"name": "Ada"}); err != nil {
		t.Fatalf("CallTool returned error: %v", err)
	}
	select {
	case task := <-captured:
		if task == nil || task.TTL != nil {
			t.Fatalf("zero TTL should send task params without TTL, got %#v", task)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for captured task params")
	}
}

type recordingToolLoader struct {
	mu    sync.Mutex
	name  string
	tools []astool.Tool
	err   error
	calls int
}

func (l *recordingToolLoader) Name() string {
	return l.name
}

func (l *recordingToolLoader) ListTools(context.Context) ([]astool.Tool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	return append([]astool.Tool(nil), l.tools...), nil
}

func (l *recordingToolLoader) ListCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
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

func assertFeatureBoundary(t *testing.T, boundaries map[Feature]FeatureBoundary, feature Feature, status FeatureStatus, detail string) {
	t.Helper()

	boundary, ok := boundaries[feature]
	if !ok {
		t.Fatalf("missing feature boundary %q", feature)
	}
	if boundary.Status != status {
		t.Fatalf("feature %q status mismatch: got %q want %q", feature, boundary.Status, status)
	}
	if !strings.Contains(boundary.Detail, detail) {
		t.Fatalf("feature %q detail should mention %q: %s", feature, detail, boundary.Detail)
	}
}

func runTool(t *testing.T, current astool.Tool, input map[string]any) *astool.ToolResponse {
	t.Helper()
	chunks, err := current.Execute(context.Background(), input, asstate.NewAgentState())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	response := astool.NewToolResponse(astool.WithToolResponseID("test-call"))
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			t.Fatalf("AppendChunk returned error: %v", err)
		}
	}
	return response
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

func hasRuleContent(rules []permission.Rule, content string) bool {
	for _, rule := range rules {
		if rule.RuleContent == content {
			return true
		}
	}
	return false
}
