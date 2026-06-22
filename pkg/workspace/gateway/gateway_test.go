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

package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/workspace"
	"github.com/yuluo-yx/agentscope-go/pkg/workspace/gateway"
)

func TestGatewayServerServesToolsMCPRegistrationToolCallsAndClose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	echo, err := tool.NewFunctionTool(
		"Echo",
		"Echo one value.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required":   []any{"value"},
		},
		func(_ context.Context, input map[string]any, _ *state.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("echo:" + input["value"].(string))}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}

	var registeredMCP *recordingRawMCP
	server := httptest.NewServer(gateway.NewServer(
		gateway.WithServerTools(echo),
		gateway.WithServerBearerToken("secret"),
		gateway.WithServerMCPClientFactory(func(config workspace.MCPClientConfig) (workspace.MCPClient, error) {
			registeredMCP = &recordingRawMCP{config: config}
			return registeredMCP, nil
		}),
	))
	defer server.Close()

	client, err := gateway.NewHTTPClient(server.URL, gateway.WithBearerToken("secret"))
	if err != nil {
		t.Fatalf("NewHTTPClient returned error: %v", err)
	}
	if healthErr := client.Health(ctx); healthErr != nil {
		t.Fatalf("Health returned error: %v", healthErr)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "Echo" || tools[0].IsMCP() {
		t.Fatalf("server tool should be exposed as a non-MCP gateway tool: %#v", tools)
	}
	kit, err := tool.NewToolkit(tools...)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	response, err := kit.RunTool(ctx, message.NewToolCallBlock("call-echo", "Echo", `{"value":"Ada"}`), state.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool Echo returned error: %v", err)
	}
	if text := response.GetTextContent(""); response.State != message.ToolResultSuccess || text == nil || *text != "echo:Ada" {
		t.Fatalf("unexpected Echo gateway response: %#v text=%v", response, text)
	}

	config := workspace.MCPClientConfig{
		Name:     "weather",
		Type:     workspace.MCPClientTypeHTTP,
		Stateful: true,
		HTTP:     &workspace.MCPHTTPConfig{URL: "https://example.invalid/weather"},
	}
	if addErr := client.AddMCP(ctx, config); addErr != nil {
		t.Fatalf("AddMCP returned error: %v", addErr)
	}
	if registeredMCP == nil || !registeredMCP.connected {
		t.Fatalf("registered MCP should be constructed and connected: %#v", registeredMCP)
	}
	mcps, err := client.ListMCPs(ctx)
	if err != nil {
		t.Fatalf("ListMCPs returned error: %v", err)
	}
	if len(mcps) != 1 || mcps[0].Name != "weather" {
		t.Fatalf("unexpected server MCP list: %#v", mcps)
	}

	mcpClient := client.NewMCPClient(config, true)
	mcpTools, err := mcpClient.ListTools(ctx)
	if err != nil {
		t.Fatalf("gateway MCP ListTools returned error: %v", err)
	}
	if len(mcpTools) != 1 || mcpTools[0].Name() != "mcp__weather__forecast" || !mcpTools[0].IsMCP() || !mcpTools[0].IsReadOnly() {
		t.Fatalf("unexpected gateway MCP tools: %#v", mcpTools)
	}
	mcpKit, err := tool.NewToolkit(mcpTools...)
	if err != nil {
		t.Fatalf("NewToolkit MCP returned error: %v", err)
	}
	mcpResponse, err := mcpKit.RunTool(ctx, message.NewToolCallBlock("call-forecast", "mcp__weather__forecast", `{"city":"Hangzhou"}`), state.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool forecast returned error: %v", err)
	}
	if text := mcpResponse.GetTextContent(""); mcpResponse.State != message.ToolResultSuccess || text == nil || *text != "forecast:Hangzhou" {
		t.Fatalf("unexpected forecast gateway response: %#v text=%v", mcpResponse, text)
	}

	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !registeredMCP.closed {
		t.Fatalf("server Close should close registered MCPs")
	}
}

func TestHTTPGatewayBootstrapsRegistersMCPExposesToolsAndCloses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var registered []workspace.MCPClientConfig
	closed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/mcps":
			var config workspace.MCPClientConfig
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			registered = append(registered, config)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/mcps":
			_ = json.NewEncoder(w).Encode(registered)
		case r.Method == http.MethodDelete && r.URL.Path == "/mcps/weather":
			registered = nil
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/tools":
			_ = json.NewEncoder(w).Encode([]gateway.ToolDescriptor{{
				Name:        "mcp__weather__forecast",
				Description: "Forecast weather.",
				InputSchema: map[string]any{"type": "object"},
				MCPName:     "weather",
				ReadOnly:    true,
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/tools/mcp__weather__forecast/call":
			_ = json.NewEncoder(w).Encode(gateway.ToolCallResponse{
				State:  message.ToolResultSuccess,
				Blocks: message.ContentBlockList{message.NewTextBlock("sunny")},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/close":
			closed = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := gateway.NewHTTPClient(server.URL)
	if err != nil {
		t.Fatalf("NewHTTPClient returned error: %v", err)
	}
	if bootstrapErr := client.Bootstrap(ctx); bootstrapErr != nil {
		t.Fatalf("Bootstrap returned error: %v", bootstrapErr)
	}
	addErr := client.AddMCP(ctx, workspace.MCPClientConfig{
		Name:     "weather",
		Type:     workspace.MCPClientTypeHTTP,
		Stateful: false,
		HTTP:     &workspace.MCPHTTPConfig{URL: "https://example.invalid/mcp"},
	})
	if addErr != nil {
		t.Fatalf("AddMCP returned error: %v", addErr)
	}
	mcps, err := client.ListMCPs(ctx)
	if err != nil {
		t.Fatalf("ListMCPs returned error: %v", err)
	}
	if len(mcps) != 1 || mcps[0].Name != "weather" {
		t.Fatalf("unexpected gateway MCP list: %#v", mcps)
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__weather__forecast" || !tools[0].IsMCP() {
		t.Fatalf("unexpected gateway tools: %#v", tools)
	}
	kit, err := tool.NewToolkit(tools...)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	response, err := kit.RunTool(ctx, message.NewToolCallBlock("call-1", "mcp__weather__forecast", `{}`), state.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool returned error: %v", err)
	}
	if text := response.GetTextContent(""); response.State != message.ToolResultSuccess || text == nil || *text != "sunny" {
		t.Fatalf("unexpected gateway tool response: %#v text=%v", response, text)
	}
	if err := client.RemoveMCP(ctx, "weather"); err != nil {
		t.Fatalf("RemoveMCP returned error: %v", err)
	}
	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !closed {
		t.Fatalf("gateway close endpoint should be called")
	}
}

func TestHTTPGatewayReportsHealthFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := gateway.NewHTTPClient(strings.TrimRight(server.URL, "/"))
	if err != nil {
		t.Fatalf("NewHTTPClient returned error: %v", err)
	}
	if err := client.Health(context.Background()); err == nil {
		t.Fatalf("Health should fail on non-2xx status")
	}
}

func TestHTTPGatewayMCPClientUsesPythonCompatibleRoutesAndAuth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gateway-token" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/mcps/weather/tools":
			_ = json.NewEncoder(w).Encode([]gomcp.Tool{
				gomcp.NewTool(
					"forecast",
					gomcp.WithDescription("Forecast weather."),
					gomcp.WithString("city", gomcp.Required(), gomcp.Description("City name.")),
					gomcp.WithReadOnlyHintAnnotation(true),
				),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/mcps/weather/tools/forecast":
			var request struct {
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if request.Arguments["city"] != "Hangzhou" {
				http.Error(w, "unexpected arguments", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]tool.ToolChunk{
				"chunk": *tool.NewToolChunk(
					message.ContentBlockList{message.NewTextBlock("sunny")},
					tool.WithToolChunkState(message.ToolResultSuccess),
				),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := gateway.NewHTTPClient(server.URL, gateway.WithBearerToken("gateway-token"))
	if err != nil {
		t.Fatalf("NewHTTPClient returned error: %v", err)
	}
	if bootstrapErr := client.Bootstrap(ctx); bootstrapErr != nil {
		t.Fatalf("Bootstrap returned error: %v", bootstrapErr)
	}
	mcpClient := client.NewMCPClient(workspace.MCPClientConfig{Name: "weather", Stateful: true}, true)
	tools, err := mcpClient.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__weather__forecast" || !tools[0].IsReadOnly() {
		t.Fatalf("unexpected MCP gateway tools: %#v", tools)
	}
	kit, err := tool.NewToolkit(tools...)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	response, err := kit.RunTool(ctx, message.NewToolCallBlock("call-1", "mcp__weather__forecast", `{"city":"Hangzhou"}`), state.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool returned error: %v", err)
	}
	if text := response.GetTextContent(""); response.State != message.ToolResultSuccess || text == nil || *text != "sunny" {
		t.Fatalf("unexpected MCP gateway response: %#v text=%v", response, text)
	}
}

type recordingRawMCP struct {
	config    workspace.MCPClientConfig
	connected bool
	closed    bool
}

func (m *recordingRawMCP) Name() string {
	return m.config.Name
}

func (m *recordingRawMCP) IsStateful() bool {
	return m.config.Stateful
}

func (m *recordingRawMCP) IsConnected() bool {
	return m.connected
}

func (m *recordingRawMCP) Connect(context.Context) error {
	m.connected = true
	return nil
}

func (m *recordingRawMCP) Close() error {
	m.closed = true
	m.connected = false
	return nil
}

func (m *recordingRawMCP) ListTools(context.Context) ([]workspace.Tool, error) {
	rawTools, err := m.ListRawTools(context.Background())
	if err != nil {
		return nil, err
	}
	tools := make([]workspace.Tool, 0, len(rawTools))
	for _, raw := range rawTools {
		currentRaw := raw
		wrapped, wrapErr := tool.NewFunctionTool(
			"mcp__"+m.config.Name+"__"+currentRaw.Name,
			currentRaw.Description,
			map[string]any{"type": "object"},
			func(_ context.Context, input map[string]any, _ *state.AgentState) (message.ContentBlockList, error) {
				return message.ContentBlockList{message.NewTextBlock("forecast:" + input["city"].(string))}, nil
			},
			tool.WithFunctionMCP(m.config.Name),
			tool.WithFunctionReadOnly(true),
		)
		if wrapErr != nil {
			return nil, wrapErr
		}
		tools = append(tools, wrapped)
	}
	return tools, nil
}

func (m *recordingRawMCP) MCPClientConfig() (workspace.MCPClientConfig, error) {
	return m.config, nil
}

func (m *recordingRawMCP) ListRawTools(context.Context) ([]gomcp.Tool, error) {
	return []gomcp.Tool{
		gomcp.NewTool(
			"forecast",
			gomcp.WithDescription("Forecast weather."),
			gomcp.WithString("city", gomcp.Required(), gomcp.Description("City name.")),
			gomcp.WithReadOnlyHintAnnotation(true),
		),
	}, nil
}

func (m *recordingRawMCP) CallTool(_ context.Context, rawName string, input map[string]any) (*gomcp.CallToolResult, error) {
	if rawName != "forecast" {
		return nil, fmt.Errorf("tool %q not found", rawName)
	}
	return gomcp.NewToolResultText("forecast:" + input["city"].(string)), nil
}
