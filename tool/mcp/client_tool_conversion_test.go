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
	"encoding/json"
	"strings"
	"testing"
	"time"

	goclient "github.com/mark3labs/mcp-go/client"
	gomcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

const mcpSchemaObjectType = "object"

func TestClientConfigValidationSnapshotsAndHelpers(t *testing.T) {
	if validateStdioConfig(StdioConfig{}) == nil {
		t.Fatal("empty stdio command should be rejected")
	}
	if validateStdioConfig(StdioConfig{Command: "cmd", EncodingErrorHandler: "bad"}) == nil {
		t.Fatal("unsupported stdio encoding handler should be rejected")
	}
	if validateHTTPConfig(HTTPConfig{}) == nil {
		t.Fatal("empty HTTP URL should be rejected")
	}
	if validateHTTPConfig(HTTPConfig{URL: "https://example.invalid", Transport: "bad"}) == nil {
		t.Fatal("unsupported HTTP transport should be rejected")
	}
	if resolveHTTPTransport(HTTPConfig{URL: "https://example.invalid/sse"}) != HTTPTransportSSE ||
		resolveHTTPTransport(HTTPConfig{URL: "https://example.invalid/messages/"}) != HTTPTransportSSE ||
		resolveHTTPTransport(HTTPConfig{URL: "https://example.invalid/mcp"}) != HTTPTransportStreamable ||
		resolveHTTPTransport(HTTPConfig{URL: "https://example.invalid/mcp", Transport: HTTPTransportStreamable}) != HTTPTransportStreamable {
		t.Fatal("HTTP transport resolution mismatch")
	}
	if got := strings.Join(envList(map[string]string{"B": "2", "A": "1"}), ","); got != "A=1,B=2" {
		t.Fatalf("envList should sort keys, got %q", got)
	}
	if envList(nil) != nil {
		t.Fatal("nil envList should stay nil")
	}
	clone := cloneStringMap(map[string]string{"A": "1"})
	clone["A"] = "changed"
	if cloneStringMap(map[string]string{"A": "1"})["A"] != "1" {
		t.Fatal("cloneStringMap should return independent maps")
	}
	if cloneStringMap(nil) != nil {
		t.Fatal("cloneStringMap(nil) should be nil")
	}

	stdio, err := NewStdioClient(
		" stdio ",
		StdioConfig{Command: "echo", Args: []string{"hello"}, Env: map[string]string{"A": "1"}, CWD: t.TempDir(), EncodingErrorHandler: "replace"},
		WithEnabledTools("read"),
		WithDisabledTools("write"),
		WithExecutionTimeout(time.Second),
		WithClientInfo("", ""),
	)
	if err != nil {
		t.Fatalf("NewStdioClient returned error: %v", err)
	}
	config, err := stdio.MCPClientConfig()
	if err != nil {
		t.Fatalf("MCPClientConfig returned error: %v", err)
	}
	if config.Name != "stdio" || config.Type != asworkspace.MCPClientTypeStdio || config.Stdio.Command != "echo" || config.ExecutionTimeout != time.Second {
		t.Fatalf("stdio config snapshot mismatch: %#v", config)
	}
	config.Stdio.Args[0] = "mutated"
	config.Stdio.Env["A"] = "mutated"
	config.EnabledTools[0] = "mutated"
	config, err = stdio.MCPClientConfig()
	if err != nil {
		t.Fatalf("second MCPClientConfig returned error: %v", err)
	}
	if config.Stdio.Args[0] != "hello" || config.Stdio.Env["A"] != "1" || config.EnabledTools[0] != "read" {
		t.Fatalf("MCPClientConfig should clone nested fields: %#v", config)
	}

	oauth := OAuthConfig{Scopes: []string{"scope-a"}}
	httpClient, err := NewHTTPClient(
		"http",
		HTTPConfig{URL: "https://example.invalid/mcp", Headers: map[string]string{"X-Test": "1"}, Timeout: time.Second, Transport: HTTPTransportStreamable},
		WithStateful(false),
		WithStreamableHTTPContinuousListening(),
		WithOAuthConfig(oauth),
	)
	if err != nil {
		t.Fatalf("NewHTTPClient returned error: %v", err)
	}
	httpConfig, err := httpClient.MCPClientConfig()
	if err != nil {
		t.Fatalf("HTTP MCPClientConfig returned error: %v", err)
	}
	if httpConfig.Type != asworkspace.MCPClientTypeHTTP || httpConfig.HTTP.Headers["X-Test"] != "1" || !httpConfig.HTTP.ContinuousListening {
		t.Fatalf("HTTP config snapshot mismatch: %#v", httpConfig)
	}

	if _, err := (*Client)(nil).MCPClientConfig(); err == nil {
		t.Fatal("nil client config should fail")
	}
	if _, err := NewInProcessClient("", newTestMCPServer()); err == nil {
		t.Fatal("empty client name should fail")
	}
	if _, err := newClient("missing-factory", defaultClientOptions(), asworkspace.MCPClientConfig{}, nil); err == nil {
		t.Fatal("nil client factory should fail")
	}
	if _, err := newClient("empty-enabled", clientOptions{enabledTools: []string{""}}, asworkspace.MCPClientConfig{}, func(context.Context) (*goclient.Client, error) { return nil, nil }); err == nil {
		t.Fatal("empty enabled tool name should fail")
	}
	inProcess, err := NewInProcessClient("runtime", newTestMCPServer())
	if err != nil {
		t.Fatalf("NewInProcessClient returned error: %v", err)
	}
	if _, err := inProcess.MCPClientConfig(); err == nil {
		t.Fatal("in-process clients should not be serializable")
	}
	if (*Client)(nil).Name() != "" || (*Client)(nil).IsStateful() || (*Client)(nil).IsConnected() {
		t.Fatal("nil client metadata mismatch")
	}
	stateless := &Client{name: "stateless"}
	if err := stateless.Close(); err != nil {
		t.Fatalf("stateless Close should be a no-op: %v", err)
	}
	if err := (*Client)(nil).Close(); err != nil {
		t.Fatalf("nil Close should be a no-op: %v", err)
	}
}

func TestToolMetadataRulesErrorsAndContentConversion(t *testing.T) {
	client := newConnectedTestClient(t)
	readOnly := false
	raw := gomcp.Tool{
		Name:           " raw_tool ",
		Description:    "Raw description.",
		RawInputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		Annotations:    gomcp.ToolAnnotation{ReadOnlyHint: &readOnly},
	}
	wrapped, err := NewTool(client, raw)
	if err != nil {
		t.Fatalf("NewTool returned error: %v", err)
	}
	if wrapped.Name() != "mcp__people__raw_tool" || wrapped.Description() != "Raw description." || wrapped.IsConcurrencySafe() || wrapped.IsReadOnly() || wrapped.IsExternalTool() || wrapped.IsStateInjected() || !wrapped.IsMCP() || wrapped.MCPName() != "people" {
		t.Fatalf("tool metadata mismatch: %#v", wrapped)
	}
	schema := wrapped.InputSchema()
	schema["type"] = "changed"
	if wrapped.InputSchema()["type"] != mcpSchemaObjectType {
		t.Fatal("InputSchema should return a clone")
	}
	decision, err := wrapped.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAsk {
		t.Fatalf("non-read-only MCP decision mismatch: %#v err=%v", decision, err)
	}
	for _, rule := range []string{"", "mcp__people__raw_tool", "raw_tool", "people.raw_tool", "people:raw_tool"} {
		if !wrapped.MatchRule(rule, nil) {
			t.Fatalf("rule %q should match", rule)
		}
	}
	if wrapped.MatchRule("other.raw_tool", nil) {
		t.Fatal("unrelated MCP rule should not match")
	}
	suggestions := wrapped.GenerateSuggestions(nil)
	if len(suggestions) != 1 || suggestions[0].RuleContent != "people.raw_tool" {
		t.Fatalf("suggestions mismatch: %#v", suggestions)
	}

	if _, err := NewTool(nil, raw); err == nil {
		t.Fatal("NewTool should reject nil clients")
	}
	if _, err := NewTool(client, gomcp.Tool{Name: "   "}); err == nil {
		t.Fatal("NewTool should reject empty raw names")
	}
	if _, err := (*Client)(nil).ListRawTools(context.Background()); err == nil {
		t.Fatal("nil ListRawTools should fail")
	}
	if _, err := (*Client)(nil).GetTool(context.Background(), "x"); err == nil {
		t.Fatal("nil GetTool should fail")
	}
	if _, err := (*Client)(nil).CallTool(context.Background(), "x", nil); err == nil {
		t.Fatal("nil CallTool should fail")
	}

	unconnected, err := NewTool(&Client{name: "offline", stateful: true}, gomcp.Tool{Name: "lookup"})
	if err != nil {
		t.Fatalf("offline NewTool returned error: %v", err)
	}
	response := runMCPTool(t, unconnected, map[string]any{})
	if response.State != message.ToolResultError || !strings.Contains(*response.GetTextContent(""), "not connected") {
		t.Fatalf("offline Execute should return error chunk: %#v", response)
	}

	blocks := ConvertContent([]gomcp.Content{
		gomcp.TextContent{Text: "text-value"},
		&gomcp.TextContent{Text: "text-pointer"},
		gomcp.ImageContent{Data: "aW1n", MIMEType: "image/png"},
		&gomcp.ImageContent{Data: "aW1nMg==", MIMEType: "image/jpeg"},
		gomcp.AudioContent{Data: "YXVkaW8=", MIMEType: "audio/wav"},
		&gomcp.AudioContent{Data: "YXVkaW8y", MIMEType: "audio/mpeg"},
		gomcp.EmbeddedResource{Resource: gomcp.TextResourceContents{URI: "file://a.txt", MIMEType: "text/plain", Text: "embedded text"}},
		&gomcp.EmbeddedResource{Resource: &gomcp.BlobResourceContents{URI: "file://b.bin", MIMEType: "application/octet-stream", Blob: "YmxvYg=="}},
		gomcp.ResourceLink{URI: "https://example.invalid/a.png", MIMEType: "image/png"},
		&gomcp.ResourceLink{URI: "https://example.invalid/b.txt", MIMEType: "text/plain"},
		nil,
	})
	if len(blocks) != 10 {
		t.Fatalf("ConvertContent should convert all supported blocks, got %d: %#v", len(blocks), blocks)
	}
	if got := blocks.GetTextContent("\n"); got == nil || !strings.Contains(*got, "text-value") || !strings.Contains(*got, "embedded text") {
		t.Fatalf("text content conversion mismatch: %#v", got)
	}
	if dataBlocks := blocks.GetContentBlocks("data"); len(dataBlocks) != 7 {
		t.Fatalf("data content conversion mismatch: %#v", dataBlocks)
	}
	structured := ConvertToolResult(&gomcp.CallToolResult{StructuredContent: map[string]any{"ok": true}})
	if len(structured) != 1 || !strings.Contains(*structured.GetTextContent(""), `"ok": true`) {
		t.Fatalf("structured fallback mismatch: %#v", structured)
	}
	if len(ConvertToolResult(nil)) != 0 {
		t.Fatal("nil tool result should convert to empty content")
	}
	if got := embeddedResourceBlocks(nil); len(got) != 0 {
		t.Fatalf("unknown embedded resource should be ignored: %#v", got)
	}
	if got := inputSchemaMap(gomcp.Tool{RawInputSchema: json.RawMessage(`not-json`)}); got["type"] != mcpSchemaObjectType || got["properties"] == nil || got["required"] == nil {
		t.Fatalf("invalid raw input schema should fall back to object schema: %#v", got)
	}
	if got := jsonString(make(chan int)); !strings.Contains(got, "0x") {
		t.Fatalf("jsonString should fall back to fmt.Sprint: %q", got)
	}
}

func TestClientNotificationAndConnectionErrorBranches(t *testing.T) {
	client, err := NewInProcessClient("people", newTestMCPServer())
	if err != nil {
		t.Fatalf("NewInProcessClient returned error: %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if _, err := client.GetTool(context.Background(), "lookup_profile"); err != nil {
		t.Fatalf("GetTool returned error: %v", err)
	}
	client.mu.Lock()
	if client.cachedTools == nil {
		t.Fatal("GetTool should populate cache")
	}
	client.mu.Unlock()
	client.handleNotification(gomcp.JSONRPCNotification{
		Notification: gomcp.Notification{Method: "notifications/other"},
	})
	client.mu.Lock()
	cached := client.cachedTools
	client.mu.Unlock()
	if cached == nil {
		t.Fatal("unrelated notifications should not clear cache")
	}
	if err := client.Connect(context.Background()); err == nil {
		t.Fatal("connecting an already-connected stateful client should fail")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := client.Close(); err == nil {
		t.Fatal("closing a disconnected stateful client should fail")
	}
	if _, err := client.ListRawTools(context.Background()); err == nil {
		t.Fatal("disconnected stateful ListRawTools should fail")
	}
	if _, err := client.CallTool(context.Background(), "lookup_profile", nil); err == nil {
		t.Fatal("disconnected stateful CallTool should fail")
	}
}

func runMCPTool(t *testing.T, current astool.Tool, input map[string]any) *astool.ToolResponse {
	t.Helper()
	chunks, err := current.Execute(context.Background(), input, asstate.NewAgentState())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	response := astool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			t.Fatalf("AppendChunk returned error: %v", err)
		}
	}
	return response
}
