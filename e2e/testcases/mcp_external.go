package testcases

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	pkgtestcases "github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	"github.com/yuluo-yx/agentscope-go/message"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	toolmcp "github.com/yuluo-yx/agentscope-go/tool/mcp"
)

func init() {
	pkgtestcases.Register("mcp-external-transports", pkgtestcases.TestCase{
		Description: "External MCP clients work over stdio, SSE, and streamable HTTP transports",
		Tags:        []string{"local", "mcp", "transport"},
		Fn:          testExternalMCPTransports,
	})
}

func testExternalMCPTransports(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	if err := os.MkdirAll(opts.WorkDir, 0o755); err != nil {
		return err
	}
	binary, err := buildExternalStdioServer(ctx, opts.WorkDir)
	if err != nil {
		return err
	}
	stdioClient, err := toolmcp.NewStdioClient("external", toolmcp.StdioConfig{Command: binary})
	if err != nil {
		return err
	}
	if err := stdioClient.Connect(ctx); err != nil {
		return err
	}
	if err := assertExternalEcho(ctx, stdioClient, "stdio:Ada"); err != nil {
		_ = stdioClient.Close()
		return err
	}
	if err := stdioClient.Close(); err != nil {
		return err
	}

	sseServer := mcpserver.NewTestServer(newExternalMCPServer("sse"))
	defer sseServer.Close()
	sseClient, err := toolmcp.NewHTTPClient(
		"external",
		toolmcp.HTTPConfig{URL: sseServer.URL + "/sse", Transport: toolmcp.HTTPTransportSSE},
		toolmcp.WithStateful(false),
	)
	if err != nil {
		return err
	}
	if err := assertExternalEcho(ctx, sseClient, "sse:Ada"); err != nil {
		return err
	}

	streamableServer := mcpserver.NewTestStreamableHTTPServer(newExternalMCPServer("streamable"))
	defer streamableServer.Close()
	streamableClient, err := toolmcp.NewHTTPClient(
		"external",
		toolmcp.HTTPConfig{URL: streamableServer.URL, Transport: toolmcp.HTTPTransportStreamable},
		toolmcp.WithStateful(false),
	)
	if err != nil {
		return err
	}
	if err := assertExternalEcho(ctx, streamableClient, "streamable:Ada"); err != nil {
		return err
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"transports": []string{"stdio", "sse", "streamable-http"}})
	}
	return nil
}

func assertExternalEcho(ctx context.Context, client *toolmcp.Client, expected string) error {
	tools, err := client.ListTools(ctx)
	if err != nil {
		return err
	}
	echo, err := findToolByName(tools, "mcp__external__echo")
	if err != nil {
		return err
	}
	kit, err := astool.NewToolkit(echo)
	if err != nil {
		return err
	}
	response, err := kit.RunTool(ctx, message.NewToolCallBlock("call-1", echo.Name(), `{"value":"Ada"}`), asstate.NewAgentState())
	if err != nil {
		return err
	}
	if text := response.GetTextContent(""); response.State != message.ToolResultSuccess || text == nil || *text != expected {
		return fmt.Errorf("unexpected MCP echo response: state=%s text=%v", response.State, text)
	}
	return nil
}

func newExternalMCPServer(prefix string) *mcpserver.MCPServer {
	server := mcpserver.NewMCPServer("external-server", "1.0.0", mcpserver.WithToolCapabilities(true))
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

func buildExternalStdioServer(ctx context.Context, dir string) (string, error) {
	source := filepath.Join(dir, "stdio_server.go")
	binary := filepath.Join(dir, "stdio-mcp-server")
	if err := os.WriteFile(source, []byte(stdioServerSource), 0o600); err != nil {
		return "", err
	}
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, source)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go build stdio server failed: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return binary, nil
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
