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

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/workspace"
)

func TestHTTPClientValidationHeadersAndDoBranches(t *testing.T) {
	t.Parallel()

	invalidBaseURLs := []string{
		"",
		"ftp://example.test",
		"http://example.test?debug=true",
		"http://user@example.test",
		"http://example.test#fragment",
	}
	for _, baseURL := range invalidBaseURLs {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := NewHTTPClient(baseURL); err == nil {
				t.Fatalf("NewHTTPClient(%q) should fail", baseURL)
			}
		})
	}

	var sawHeader bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") == "yes" && r.Header.Get("Authorization") == "Bearer token" {
			sawHeader = true
		}
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusNoContent)
		case "/decode":
			_, _ = w.Write([]byte("{"))
		case "/status":
			http.Error(w, "bad status", http.StatusTeapot)
		case "/echo":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	headers := map[string]string{"X-Test": "yes", "": "ignored"}
	client, err := NewHTTPClient(server.URL+"/", WithHTTPClient(nil), WithHeaders(headers), WithBearerToken(" token "))
	if err != nil {
		t.Fatalf("NewHTTPClient returned error: %v", err)
	}
	headers["X-Test"] = "changed"
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if !sawHeader {
		t.Fatalf("client should send cloned static headers and bearer token")
	}
	if err := client.do(context.Background(), http.MethodGet, "relative", nil, nil, http.StatusOK); err == nil || !strings.Contains(err.Error(), "invalid internal path") {
		t.Fatalf("do relative path error = %v", err)
	}
	if err := client.do(context.Background(), http.MethodGet, "//double", nil, nil, http.StatusOK); err == nil || !strings.Contains(err.Error(), "invalid internal path") {
		t.Fatalf("do double-slash path error = %v", err)
	}
	if err := client.do(context.Background(), http.MethodPost, "/echo", map[string]any{"bad": func() {}}, nil, http.StatusOK); err == nil {
		t.Fatalf("do should return JSON marshal errors")
	}
	var decoded map[string]any
	if err := client.do(context.Background(), http.MethodGet, "/decode", nil, &decoded, http.StatusOK); err == nil {
		t.Fatalf("do should return JSON decode errors")
	}
	if err := client.do(context.Background(), http.MethodGet, "/status", nil, nil, http.StatusOK); err == nil || !strings.Contains(err.Error(), "HTTP 418") {
		t.Fatalf("do status error = %v", err)
	}
	var nilClient *Client
	if err := nilClient.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "nil client") {
		t.Fatalf("nil client Health error = %v", err)
	}
	if !statusAllowed(http.StatusCreated, []int{http.StatusOK, http.StatusCreated}) || statusAllowed(http.StatusTeapot, []int{http.StatusOK}) {
		t.Fatalf("statusAllowed mismatch")
	}
}

func TestGatewayToolAndMCPToolMetadataExecutionBranches(t *testing.T) {
	t.Parallel()

	gatewayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tools/Remote/call":
			_ = json.NewEncoder(w).Encode(ToolCallResponse{Blocks: message.ContentBlockList{message.NewTextBlock("remote ok")}})
		case "/tools/Fail/call":
			http.Error(w, "fail", http.StatusInternalServerError)
		case "/mcps/weather/tools/forecast":
			_ = json.NewEncoder(w).Encode(map[string]*tool.ToolChunk{
				"chunk": tool.NewToolChunk(message.ContentBlockList{message.NewTextBlock("sunny")}, tool.WithToolChunkState(message.ToolResultRunning)),
			})
		case "/mcps/weather/tools/missing":
			_ = json.NewEncoder(w).Encode(map[string]*tool.ToolChunk{"chunk": nil})
		default:
			http.NotFound(w, r)
		}
	}))
	defer gatewayServer.Close()
	client, err := NewHTTPClient(gatewayServer.URL)
	if err != nil {
		t.Fatalf("NewHTTPClient returned error: %v", err)
	}

	readOnly := &gatewayTool{client: client, descriptor: ToolDescriptor{Name: "Remote", MCPName: "weather", ReadOnly: true, ConcurrencySafe: true}}
	if readOnly.Name() != "Remote" || readOnly.Description() != "" || !readOnly.IsMCP() || readOnly.MCPName() != "weather" {
		t.Fatalf("gatewayTool metadata mismatch")
	}
	if schema := readOnly.InputSchema(); schema["type"] != "object" {
		t.Fatalf("gatewayTool nil schema should default to object: %#v", schema)
	}
	if !readOnly.IsReadOnly() || !readOnly.IsConcurrencySafe() || readOnly.IsExternalTool() || readOnly.IsStateInjected() {
		t.Fatalf("gatewayTool flags mismatch")
	}
	decision, err := readOnly.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("read-only CheckPermissions = %#v, %v", decision, err)
	}
	if !readOnly.MatchRule("weather", nil) || !readOnly.MatchRule("", nil) || readOnly.MatchRule("other", nil) {
		t.Fatalf("gatewayTool MatchRule mismatch")
	}
	if len(readOnly.GenerateSuggestions(nil)) != 1 {
		t.Fatalf("gatewayTool should generate one suggestion")
	}
	response := collectGatewayToolResponse(t, readOnly, map[string]any{"value": "ok"})
	if response.State != message.ToolResultSuccess || response.GetTextContent("") == nil || *response.GetTextContent("") != "remote ok" {
		t.Fatalf("gatewayTool Execute response mismatch: %#v", response)
	}
	writeTool := &gatewayTool{client: client, descriptor: ToolDescriptor{Name: "Fail", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}}}
	schema := writeTool.InputSchema()
	schema["type"] = "changed"
	if writeTool.descriptor.InputSchema["type"] != "object" {
		t.Fatalf("gatewayTool InputSchema should clone descriptor schema")
	}
	decision, err = writeTool.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAsk || len(decision.SuggestedRules) == 0 {
		t.Fatalf("write CheckPermissions = %#v, %v", decision, err)
	}
	failed := collectGatewayToolResponse(t, writeTool, nil)
	if failed.State != message.ToolResultError || failed.GetTextContent("") == nil || !strings.Contains(*failed.GetTextContent(""), "HTTP 500") {
		t.Fatalf("gatewayTool error response mismatch: %#v", failed)
	}

	raw := gomcp.NewTool(
		"forecast",
		gomcp.WithDescription("Forecast weather."),
		gomcp.WithRawInputSchema(json.RawMessage(`{"properties":{"city":{"type":"string"}}}`)),
		gomcp.WithReadOnlyHintAnnotation(true),
	)
	mcpTool := &gatewayMCPTool{client: client, mcpName: "weather", raw: raw, name: qualifiedMCPToolName("weather", raw.Name), inputSchema: rawInputSchemaMap(raw), readOnly: rawReadOnly(raw)}
	if mcpTool.Description() != "Forecast weather." || !mcpTool.IsMCP() || mcpTool.MCPName() != "weather" || mcpTool.IsConcurrencySafe() || mcpTool.IsExternalTool() || mcpTool.IsStateInjected() {
		t.Fatalf("gatewayMCPTool metadata mismatch")
	}
	if mcpTool.InputSchema()["type"] != "object" {
		t.Fatalf("gatewayMCPTool schema should be normalized: %#v", mcpTool.InputSchema())
	}
	decision, err = mcpTool.CheckPermissions(context.Background(), nil, permission.NewContext(permission.ModeDefault))
	if err != nil || decision.Behavior != permission.BehaviorAllow {
		t.Fatalf("gatewayMCPTool read-only permissions = %#v, %v", decision, err)
	}
	if !mcpTool.MatchRule("", nil) || !mcpTool.MatchRule("forecast", nil) || !mcpTool.MatchRule("weather.forecast", nil) || !mcpTool.MatchRule("weather:forecast", nil) || !mcpTool.MatchRule("mcp__weather__forecast", nil) {
		t.Fatalf("gatewayMCPTool MatchRule mismatch")
	}
	if got := mcpTool.GenerateSuggestions(nil)[0].RuleContent; got != "weather.forecast" {
		t.Fatalf("gatewayMCPTool suggestion = %q", got)
	}
	mcpResponse := collectGatewayToolResponse(t, mcpTool, map[string]any{"city": "Hangzhou"})
	if mcpResponse.State != message.ToolResultSuccess || mcpResponse.GetTextContent("") == nil || *mcpResponse.GetTextContent("") != "sunny" {
		t.Fatalf("gatewayMCPTool Execute response mismatch: %#v", mcpResponse)
	}
	missingChunk := &gatewayMCPTool{client: client, mcpName: "weather", raw: gomcp.NewTool("missing"), name: "mcp__weather__missing"}
	missingResponse := collectGatewayToolResponse(t, missingChunk, nil)
	if missingResponse.State != message.ToolResultError || missingResponse.GetTextContent("") == nil || !strings.Contains(*missingResponse.GetTextContent(""), "returned no chunk") {
		t.Fatalf("gatewayMCPTool nil chunk response mismatch: %#v", missingResponse)
	}
	askTool := &gatewayMCPTool{mcpName: "weather", raw: gomcp.NewTool("write"), name: "mcp__weather__write"}
	askDecision, err := askTool.CheckPermissions(context.Background(), nil, nil)
	if err != nil || askDecision.Behavior != permission.BehaviorAsk {
		t.Fatalf("gatewayMCPTool write permissions = %#v, %v", askDecision, err)
	}
}

func TestMCPClientConfigConnectListToolsAndFilters(t *testing.T) {
	t.Parallel()

	var nilMCP *MCPClient
	if nilMCP.Name() != "" || nilMCP.IsStateful() || nilMCP.IsConnected() {
		t.Fatalf("nil MCP identity mismatch")
	}
	if err := nilMCP.Connect(context.Background()); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("nil MCP Connect error = %v", err)
	}
	if _, err := nilMCP.MCPClientConfig(); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("nil MCP MCPClientConfig error = %v", err)
	}
	if _, err := nilMCP.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "nil MCP client") {
		t.Fatalf("nil MCP ListTools error = %v", err)
	}

	added := false
	removed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/mcps":
			added = true
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodDelete && r.URL.Path == "/mcps/weather":
			removed = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/mcps/weather/tools":
			_ = json.NewEncoder(w).Encode([]gomcp.Tool{
				gomcp.NewTool("forecast", gomcp.WithDescription("Forecast.")),
				gomcp.NewTool("history", gomcp.WithDescription("History.")),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPClient returned error: %v", err)
	}
	config := workspace.MCPClientConfig{
		Name:          "weather",
		Stateful:      true,
		Type:          workspace.MCPClientTypeHTTP,
		HTTP:          &workspace.MCPHTTPConfig{URL: "http://localhost/mcp", Headers: map[string]string{"X": "Y"}},
		EnabledTools:  []string{"forecast"},
		DisabledTools: []string{},
		Stdio:         &workspace.MCPStdioConfig{Args: []string{"--one"}, Env: map[string]string{"A": "B"}},
	}
	mcpClient := client.NewMCPClient(config, false).(*MCPClient)
	if _, err := mcpClient.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("ListTools should require connected stateful MCP, got %v", err)
	}
	if err := mcpClient.Connect(context.Background()); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if !added || !mcpClient.connected {
		t.Fatalf("Connect should register MCP, added=%v connected=%v", added, mcpClient.connected)
	}
	if err := mcpClient.Connect(context.Background()); err == nil || !strings.Contains(err.Error(), "already connected") {
		t.Fatalf("Connect already-connected error = %v", err)
	}
	cloned, err := mcpClient.MCPClientConfig()
	if err != nil {
		t.Fatalf("MCPClientConfig returned error: %v", err)
	}
	cloned.EnabledTools[0] = "changed"
	cloned.Stdio.Args[0] = "changed"
	cloned.Stdio.Env["A"] = "changed"
	cloned.HTTP.Headers["X"] = "changed"
	if mcpClient.config.EnabledTools[0] != "forecast" || mcpClient.config.Stdio.Args[0] != "--one" || mcpClient.config.Stdio.Env["A"] != "B" || mcpClient.config.HTTP.Headers["X"] != "Y" {
		t.Fatalf("MCPClientConfig should deep clone config: %#v", mcpClient.config)
	}
	tools, err := mcpClient.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__weather__forecast" {
		t.Fatalf("ListTools filter mismatch: %#v", tools)
	}
	if _, err := client.ListMCPTools(context.Background(), "weather"); err != nil {
		t.Fatalf("ListMCPTools returned error: %v", err)
	}
	if err := mcpClient.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !removed || mcpClient.connected {
		t.Fatalf("Close should unregister MCP, removed=%v connected=%v", removed, mcpClient.connected)
	}

	stateless := client.NewMCPClient(workspace.MCPClientConfig{Name: "stateless", Stateful: false}, false).(*MCPClient)
	if err := stateless.Connect(context.Background()); err != nil {
		t.Fatalf("stateless Connect returned error: %v", err)
	}
	if err := stateless.Close(); err != nil {
		t.Fatalf("stateless Close returned error: %v", err)
	}
	if _, _, err := toolNameSet([]string{" "}); err == nil || !strings.Contains(err.Error(), "tool filter name is empty") {
		t.Fatalf("toolNameSet empty name error = %v", err)
	}
	if _, err := filterRawTools([]gomcp.Tool{gomcp.NewTool("one")}, []string{"one"}, []string{"one"}); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("filterRawTools overlap error = %v", err)
	}
}

func TestServerRoutesErrorsHelpersAndFallbackMCPTools(t *testing.T) {
	t.Parallel()

	var nilServer *Server
	response := httptest.NewRecorder()
	nilServer.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tools", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("nil server status = %d", response.Code)
	}

	readTool := &gatewayCoverageTool{
		name:     "Read",
		readOnly: true,
		schema:   map[string]any{"type": "object"},
		chunks: []tool.ToolChunk{*tool.NewToolChunk(
			message.ContentBlockList{message.NewTextBlock("read ok")},
			tool.WithToolChunkState(message.ToolResultSuccess),
		)},
	}
	plainMCP := &plainGatewayMCP{
		name:      "plain",
		stateful:  true,
		connected: false,
		tools:     []workspace.Tool{readTool},
		config: workspace.MCPClientConfig{
			Name:     "plain",
			Type:     workspace.MCPClientTypeHTTP,
			Stateful: true,
			HTTP:     &workspace.MCPHTTPConfig{URL: "http://localhost/plain"},
		},
	}
	server := NewServer(
		nil,
		WithServerTools(nil, &gatewayCoverageTool{name: ""}, readTool),
		WithServerMCPs(nil, &plainGatewayMCP{name: ""}, plainMCP),
		WithServerBearerToken("secret"),
	)
	if !NewServer().authorized(httptest.NewRequest(http.MethodGet, "/tools", nil)) {
		t.Fatalf("authorized should allow requests when server has no token")
	}
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d", rec.Code)
	}
	authed := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		return requestAuthed(server, method, path, body)
	}
	if rec := authed(http.MethodGet, "/unknown", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d", rec.Code)
	}
	if rec := authed(http.MethodPost, "/tools/Read/call", "{"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad tool JSON status = %d", rec.Code)
	}
	if rec := authed(http.MethodPost, "/tools/Missing/call", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing tool status = %d", rec.Code)
	}
	if rec := authed(http.MethodPost, "/tools/%zz/call", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad escaped tool call status = %d", rec.Code)
	}
	if rec := authed(http.MethodPost, "/tools/Read/call", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("tool call status = %d body=%s", rec.Code, rec.Body.String())
	}
	errorToolServer := NewServer(
		WithServerBearerToken("secret"),
		WithServerTools(&gatewayCoverageTool{name: "Bad", err: errors.New("tool failed")}),
	)
	if rec := postAuthed(errorToolServer, "/tools/Bad/call", `{}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("tool execution error status = %d", rec.Code)
	}
	if rec := authed(http.MethodGet, "/mcps/%20/tools", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty MCP tools path status = %d", rec.Code)
	}
	if rec := authed(http.MethodGet, "/mcps/%zz/tools", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad escaped MCP tools path status = %d", rec.Code)
	}
	if rec := authed(http.MethodGet, "/mcps/missing/tools", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing MCP tools status = %d", rec.Code)
	}
	if rec := authed(http.MethodPost, "/mcps/missing/tools/Read", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing MCP call status = %d", rec.Code)
	}
	if rec := authed(http.MethodPost, "/mcps/%zz/tools/Read", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad escaped MCP call path status = %d", rec.Code)
	}
	if rec := authed(http.MethodPost, "/mcps/plain/tools/Read", "{"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad MCP call JSON status = %d", rec.Code)
	}
	if rec := authed(http.MethodPost, "/mcps/plain/tools/Read", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("fallback MCP tool call status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := authed(http.MethodPost, "/mcps/plain/tools/Missing", `{}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("missing fallback MCP tool status = %d", rec.Code)
	}
	if !plainMCP.connectCalled || !plainMCP.connected {
		t.Fatalf("fallback MCP should be connected before use: %#v", plainMCP)
	}
	if rec := authed(http.MethodDelete, "/mcps/missing", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("remove missing status = %d", rec.Code)
	}
	if rec := authed(http.MethodDelete, "/mcps/plain", ""); rec.Code != http.StatusOK {
		t.Fatalf("remove plain status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !plainMCP.closeCalled {
		t.Fatalf("remove should close connected stateful MCP")
	}

	factoryErr := errors.New("factory failed")
	errorServer := NewServer(WithServerBearerToken("secret"), WithServerMCPClientFactory(func(workspace.MCPClientConfig) (workspace.MCPClient, error) {
		return nil, factoryErr
	}))
	if rec := postAuthed(errorServer, "/mcps", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty MCP name status = %d", rec.Code)
	}
	if rec := postAuthed(errorServer, "/mcps", `{"name":"bad","type":"http_mcp"}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("factory error status = %d", rec.Code)
	}
	connectErrServer := NewServer(WithServerBearerToken("secret"), WithServerMCPClientFactory(func(workspace.MCPClientConfig) (workspace.MCPClient, error) {
		return &plainGatewayMCP{name: "bad-connect", stateful: true, connectErr: errors.New("connect failed")}, nil
	}))
	if rec := postAuthed(connectErrServer, "/mcps", `{"name":"bad-connect","type":"http_mcp","http":{"url":"http://localhost"}}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("connect error add status = %d", rec.Code)
	}
	nilFactoryServer := NewServer(WithServerBearerToken("secret"), WithServerMCPClientFactory(func(config workspace.MCPClientConfig) (workspace.MCPClient, error) {
		return nil, nil
	}))
	if rec := postAuthed(nilFactoryServer, "/mcps", `{"name":"nil","type":"http_mcp","http":{"url":"http://localhost"}}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("nil factory status = %d", rec.Code)
	}
	duplicateMCP := &plainGatewayMCP{name: "dup", config: workspace.MCPClientConfig{Name: "dup", Type: workspace.MCPClientTypeHTTP, HTTP: &workspace.MCPHTTPConfig{URL: "http://localhost/dup"}}}
	duplicateServer := NewServer(WithServerBearerToken("secret"), WithServerMCPs(duplicateMCP), WithServerMCPClientFactory(func(workspace.MCPClientConfig) (workspace.MCPClient, error) {
		return &plainGatewayMCP{name: "dup"}, nil
	}))
	if rec := postAuthed(duplicateServer, "/mcps", `{"name":"dup","type":"http_mcp","http":{"url":"http://localhost"}}`); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d", rec.Code)
	}
	lateDuplicate := &plainGatewayMCP{name: "existing", stateful: true, connected: true}
	lateDuplicateServer := NewServer(
		WithServerBearerToken("secret"),
		WithServerMCPs(lateDuplicate),
		WithServerMCPClientFactory(func(workspace.MCPClientConfig) (workspace.MCPClient, error) {
			return &plainGatewayMCP{name: "existing", stateful: true, connected: true}, nil
		}),
	)
	if rec := postAuthed(lateDuplicateServer, "/mcps", `{"name":"new","type":"http_mcp","http":{"url":"http://localhost"}}`); rec.Code != http.StatusConflict {
		t.Fatalf("late duplicate status = %d", rec.Code)
	}

	nonSerializableServer := NewServer(WithServerBearerToken("secret"), WithServerMCPs(nonConfigGatewayMCP{name: "runtime"}))
	if rec := requestAuthed(nonSerializableServer, http.MethodGet, "/mcps", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("non-serializable MCP list status = %d", rec.Code)
	}
	listErrServer := NewServer(WithServerBearerToken("secret"), WithServerMCPs(&plainGatewayMCP{
		name:         "list-broken",
		stateful:     true,
		listToolsErr: errors.New("list failed"),
	}))
	if rec := requestAuthed(listErrServer, http.MethodGet, "/tools", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("list tools MCP error status = %d", rec.Code)
	}
	closeErrServer := NewServer(WithServerBearerToken("secret"), WithServerMCPs(&plainGatewayMCP{
		name:      "close-broken",
		stateful:  true,
		connected: true,
		closeErr:  errors.New("close failed"),
	}))
	if rec := requestAuthed(closeErrServer, http.MethodDelete, "/mcps/close-broken", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("remove close error status = %d", rec.Code)
	}
	closeAllServer := NewServer(WithServerBearerToken("secret"), WithServerMCPs(&plainGatewayMCP{
		name:      "close-all-broken",
		stateful:  true,
		connected: true,
		closeErr:  errors.New("close all failed"),
	}))
	if rec := requestAuthed(closeAllServer, http.MethodPost, "/close", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("close all error status = %d", rec.Code)
	}

	if normalizedPath("/tools/") != "/tools" || normalizedPath("/") != "/" {
		t.Fatalf("normalizedPath mismatch")
	}
	if _, err := mcpNameFromToolsPath("/mcps/%20/tools"); err == nil || !strings.Contains(err.Error(), "MCP name is empty") {
		t.Fatalf("mcpNameFromToolsPath empty error = %v", err)
	}
	if _, _, err := mcpAndToolNameFromCallPath("/mcps/weather/tools/%20"); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("mcpAndToolNameFromCallPath empty tool error = %v", err)
	}
	raw := rawToolFromWorkspaceTool("plain", readTool)
	if raw.Name != "Read" || raw.Annotations.ReadOnlyHint == nil || !*raw.Annotations.ReadOnlyHint {
		t.Fatalf("rawToolFromWorkspaceTool mismatch: %#v", raw)
	}
	if rawToolName("plain", "mcp__plain__Read") != "Read" || rawToolName("plain", "Other") != "Other" {
		t.Fatalf("rawToolName mismatch")
	}
	if _, err := configFromMCP(nonConfigGatewayMCP{name: "runtime"}); err == nil || !strings.Contains(err.Error(), "cannot be serialized") {
		t.Fatalf("configFromMCP non-provider error = %v", err)
	}
}

func TestDefaultServerMCPFactoryBranches(t *testing.T) {
	t.Parallel()

	if _, err := defaultServerMCPFactory(workspace.MCPClientConfig{Name: "bad", Type: workspace.MCPClientTypeStdio}); err == nil || !strings.Contains(err.Error(), "missing config") {
		t.Fatalf("defaultServerMCPFactory missing stdio error = %v", err)
	}
	if _, err := defaultServerMCPFactory(workspace.MCPClientConfig{Name: "bad", Type: workspace.MCPClientTypeHTTP}); err == nil || !strings.Contains(err.Error(), "missing config") {
		t.Fatalf("defaultServerMCPFactory missing HTTP error = %v", err)
	}
	if _, err := defaultServerMCPFactory(workspace.MCPClientConfig{Name: "bad", Type: "unsupported"}); err == nil || !strings.Contains(err.Error(), "unsupported MCP type") {
		t.Fatalf("defaultServerMCPFactory unsupported error = %v", err)
	}
	stdio, err := defaultServerMCPFactory(workspace.MCPClientConfig{
		Name:             "stdio",
		Type:             workspace.MCPClientTypeStdio,
		Stateful:         true,
		ExecutionTimeout: time.Second,
		EnabledTools:     []string{"one"},
		DisabledTools:    []string{"two"},
		Stdio: &workspace.MCPStdioConfig{
			Command: "server",
			Args:    []string{"--arg"},
			Env:     map[string]string{"TOKEN": "secret"},
		},
	})
	if err != nil {
		t.Fatalf("defaultServerMCPFactory stdio returned error: %v", err)
	}
	if stdio.Name() != "stdio" || !stdio.IsStateful() {
		t.Fatalf("stdio MCP mismatch: %T %q", stdio, stdio.Name())
	}
	httpClient, err := defaultServerMCPFactory(workspace.MCPClientConfig{
		Name:             "http",
		Type:             workspace.MCPClientTypeHTTP,
		ExecutionTimeout: time.Second,
		HTTP:             &workspace.MCPHTTPConfig{URL: "http://localhost/mcp", Headers: map[string]string{"X": "Y"}, ContinuousListening: true},
	})
	if err != nil {
		t.Fatalf("defaultServerMCPFactory HTTP returned error: %v", err)
	}
	if httpClient.Name() != "http" {
		t.Fatalf("HTTP MCP name = %q", httpClient.Name())
	}
}

func collectGatewayToolResponse(t *testing.T, current workspace.Tool, input map[string]any) *tool.ToolResponse {
	t.Helper()

	chunks, err := current.Execute(context.Background(), input, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	response := tool.NewToolResponse()
	for chunk := range chunks {
		if err := response.AppendChunk(&chunk); err != nil {
			t.Fatalf("AppendChunk returned error: %v", err)
		}
	}
	return response
}

func postAuthed(server *Server, path, body string) *httptest.ResponseRecorder {
	return requestAuthed(server, http.MethodPost, path, body)
}

func requestAuthed(server *Server, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/", strings.NewReader(body))
	request.URL.Path = path
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

type gatewayCoverageTool struct {
	name     string
	readOnly bool
	mcpName  string
	schema   map[string]any
	chunks   []tool.ToolChunk
	err      error
}

func (t *gatewayCoverageTool) Name() string { return t.name }

func (t *gatewayCoverageTool) Description() string { return "coverage tool" }

func (t *gatewayCoverageTool) InputSchema() map[string]any {
	if t.schema == nil {
		return map[string]any{"type": "object"}
	}
	return t.schema
}

func (t *gatewayCoverageTool) IsConcurrencySafe() bool { return true }

func (t *gatewayCoverageTool) IsReadOnly() bool { return t.readOnly }

func (t *gatewayCoverageTool) IsExternalTool() bool { return false }

func (t *gatewayCoverageTool) IsStateInjected() bool { return false }

func (t *gatewayCoverageTool) IsMCP() bool { return t.mcpName != "" }

func (t *gatewayCoverageTool) MCPName() string { return t.mcpName }

func (t *gatewayCoverageTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	return &permission.Decision{Behavior: permission.BehaviorAllow}, nil
}

func (t *gatewayCoverageTool) MatchRule(string, map[string]any) bool { return true }

func (t *gatewayCoverageTool) GenerateSuggestions(map[string]any) []permission.Rule { return nil }

func (t *gatewayCoverageTool) Execute(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	if t.err != nil {
		return nil, t.err
	}
	chunks := make(chan tool.ToolChunk, len(t.chunks))
	for _, chunk := range t.chunks {
		chunks <- chunk
	}
	close(chunks)
	return chunks, nil
}

type plainGatewayMCP struct {
	name          string
	stateful      bool
	connected     bool
	connectCalled bool
	closeCalled   bool
	connectErr    error
	closeErr      error
	tools         []workspace.Tool
	listToolsErr  error
	config        workspace.MCPClientConfig
}

func (m *plainGatewayMCP) Name() string { return m.name }

func (m *plainGatewayMCP) IsStateful() bool { return m.stateful }

func (m *plainGatewayMCP) IsConnected() bool { return m.connected }

func (m *plainGatewayMCP) Connect(context.Context) error {
	m.connectCalled = true
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *plainGatewayMCP) Close() error {
	m.closeCalled = true
	if m.closeErr != nil {
		return m.closeErr
	}
	m.connected = false
	return nil
}

func (m *plainGatewayMCP) ListTools(context.Context) ([]workspace.Tool, error) {
	return m.tools, m.listToolsErr
}

func (m *plainGatewayMCP) MCPClientConfig() (workspace.MCPClientConfig, error) {
	if m.config.Name == "" {
		return workspace.MCPClientConfig{Name: m.name, Type: workspace.MCPClientTypeHTTP, HTTP: &workspace.MCPHTTPConfig{URL: "http://localhost/" + m.name}}, nil
	}
	return m.config, nil
}

type nonConfigGatewayMCP struct {
	name string
}

func (m nonConfigGatewayMCP) Name() string { return m.name }

func (nonConfigGatewayMCP) IsStateful() bool { return false }

func (nonConfigGatewayMCP) IsConnected() bool { return false }

func (nonConfigGatewayMCP) Connect(context.Context) error { return nil }

func (nonConfigGatewayMCP) Close() error { return nil }

func (nonConfigGatewayMCP) ListTools(context.Context) ([]workspace.Tool, error) { return nil, nil }
