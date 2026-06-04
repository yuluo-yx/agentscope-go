package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/workspace"
	"github.com/yuluo-yx/agentscope-go/workspace/gateway"
)

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
	if err := client.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap returned error: %v", err)
	}
	if err := client.AddMCP(ctx, workspace.MCPClientConfig{
		Name:     "weather",
		Type:     workspace.MCPClientTypeHTTP,
		Stateful: false,
		HTTP:     &workspace.MCPHTTPConfig{URL: "https://example.invalid/mcp"},
	}); err != nil {
		t.Fatalf("AddMCP returned error: %v", err)
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
