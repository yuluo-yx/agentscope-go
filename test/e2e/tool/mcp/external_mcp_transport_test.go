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

package mcpe2e_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/yuluo-yx/agentscope-go/message"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	toolmcp "github.com/yuluo-yx/agentscope-go/tool/mcp"
)

func TestExternalMCPTransportsE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("stdio subprocess", func(t *testing.T) {
		binary := buildExternalStdioServer(t)
		client, err := toolmcp.NewStdioClient("external", toolmcp.StdioConfig{Command: binary})
		if err != nil {
			t.Fatalf("NewStdioClient returned error: %v", err)
		}
		if err := client.Connect(ctx); err != nil {
			t.Fatalf("Connect returned error: %v", err)
		}
		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Fatalf("Close returned error: %v", err)
			}
		})

		assertExternalEcho(t, ctx, client, "stdio:Ada")
	})

	t.Run("sse", func(t *testing.T) {
		server := mcpserver.NewTestServer(newExternalMCPServer("sse"))
		defer server.Close()

		client, err := toolmcp.NewHTTPClient(
			"external",
			toolmcp.HTTPConfig{URL: server.URL + "/sse", Transport: toolmcp.HTTPTransportSSE},
			toolmcp.WithStateful(false),
		)
		if err != nil {
			t.Fatalf("NewHTTPClient returned error: %v", err)
		}

		assertExternalEcho(t, ctx, client, "sse:Ada")
	})

	t.Run("streamable http", func(t *testing.T) {
		server := mcpserver.NewTestStreamableHTTPServer(newExternalMCPServer("streamable"))
		defer server.Close()

		client, err := toolmcp.NewHTTPClient(
			"external",
			toolmcp.HTTPConfig{URL: server.URL, Transport: toolmcp.HTTPTransportStreamable},
			toolmcp.WithStateful(false),
		)
		if err != nil {
			t.Fatalf("NewHTTPClient returned error: %v", err)
		}

		assertExternalEcho(t, ctx, client, "streamable:Ada")
	})
}

func assertExternalEcho(t *testing.T, ctx context.Context, client *toolmcp.Client, expected string) {
	t.Helper()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	echo := findTool(t, tools, "mcp__external__echo")
	kit, err := astool.NewToolkit(echo)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	response, err := kit.RunTool(ctx, message.NewToolCallBlock("call-1", echo.Name(), `{"value":"Ada"}`), asstate.NewAgentState())
	if err != nil {
		t.Fatalf("RunTool returned error: %v", err)
	}
	if text := response.GetTextContent(""); response.State != message.ToolResultSuccess || text == nil || *text != expected {
		t.Fatalf("unexpected MCP echo response: state=%s text=%v", response.State, text)
	}
}

func newExternalMCPServer(prefix string) *mcpserver.MCPServer {
	server := mcpserver.NewMCPServer(
		"external-server",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	server.AddTool(
		gomcp.NewTool(
			"echo",
			gomcp.WithDescription("Echo one value."),
			gomcp.WithString("value", gomcp.Required(), gomcp.Description("Value to echo.")),
			gomcp.WithReadOnlyHintAnnotation(true),
		),
		func(_ context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			return gomcp.NewToolResultText(prefix + ":" + request.GetString("value", "")), nil
		},
	)
	return server
}

func buildExternalStdioServer(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	source := filepath.Join(dir, "server.go")
	binary := filepath.Join(dir, "stdio-mcp-server")
	if err := os.WriteFile(source, []byte(stdioServerSource), 0o600); err != nil {
		t.Fatalf("write stdio server source: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binary, source)
	cmd.Dir = filepath.Clean("../../../..")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build stdio server failed: %v\n%s", err, string(output))
	}
	return binary
}

func findTool(t *testing.T, tools []astool.Tool, name string) astool.Tool {
	t.Helper()

	for _, current := range tools {
		if current.Name() == name {
			return current
		}
	}
	t.Fatalf("missing tool %s", name)
	return nil
}

const stdioServerSource = `package main

import (
	"context"
	"log"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	s := server.NewMCPServer("external-stdio", "1.0.0", server.WithToolCapabilities(true))
	s.AddTool(
		mcp.NewTool(
			"echo",
			mcp.WithDescription("Echo one value."),
			mcp.WithString("value", mcp.Required(), mcp.Description("Value to echo.")),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("stdio:" + request.GetString("value", "")), nil
		},
	)
	if err := server.ServeStdio(s); err != nil && !strings.Contains(err.Error(), "file already closed") {
		log.Fatal(err)
	}
}
`
