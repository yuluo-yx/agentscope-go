//go:build integration

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

package opensandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/alibaba/OpenSandbox/sdks/sandbox/go"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	workspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

func TestOpenSandboxWorkspaceEndToEnd(t *testing.T) {
	domain := strings.TrimSpace(os.Getenv("OPEN_SANDBOX_DOMAIN"))
	apiKey := strings.TrimSpace(os.Getenv("OPEN_SANDBOX_API_KEY"))
	if domain == "" {
		t.Skip("OPEN_SANDBOX_DOMAIN is required")
	}
	protocol := strings.ToLower(strings.TrimSpace(os.Getenv("OPEN_SANDBOX_PROTOCOL")))
	if protocol == "" {
		protocol = "http"
	}
	if separator := strings.Index(domain, "://"); separator >= 0 {
		if configured := strings.ToLower(domain[:separator]); configured == "http" || configured == "https" {
			protocol = configured
			domain = domain[separator+3:]
		}
	}
	if protocol != "http" && protocol != "https" {
		t.Fatalf("OPEN_SANDBOX_PROTOCOL must be http or https, got %q", protocol)
	}

	workspaceID := "agentscope-go-e2e-" + randomTestSuffix(t)
	connection := sdk.ConnectionConfig{
		Domain:         domain,
		Protocol:       protocol,
		APIKey:         apiKey,
		RequestTimeout: 10 * time.Minute,
	}
	workspaceInstance, err := NewWorkspace(
		WithWorkspaceID(workspaceID),
		WithDomain(domain),
		WithProtocol(protocol),
		WithAPIKey(apiKey),
		WithRequestTimeout(10*time.Minute),
		WithTimeout(15*time.Minute),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		sandboxID := workspaceInstance.SandboxID()
		if err := workspaceInstance.Close(cleanupCtx); err != nil {
			t.Logf("close workspace during cleanup: %v", err)
		}
		cleanupOpenSandboxes(t, cleanupCtx, connection, workspaceID, sandboxID)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	if err := workspaceInstance.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if !workspaceInstance.IsAlive() || workspaceInstance.SandboxID() == "" ||
		workspaceInstance.WorkspaceRoot() != defaultWorkdir {
		t.Fatalf("initialized workspace state mismatch: alive=%v sandbox=%q root=%q",
			workspaceInstance.IsAlive(), workspaceInstance.SandboxID(), workspaceInstance.WorkspaceRoot())
	}
	instructions, err := workspaceInstance.GetInstructions(ctx)
	if err != nil || !strings.Contains(instructions, defaultWorkdir) {
		t.Fatalf("GetInstructions = %q, %v", instructions, err)
	}

	tools, err := workspaceInstance.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools returned error: %v", err)
	}
	writeTool := findIntegrationTool(t, tools, "Write")
	readTool := findIntegrationTool(t, tools, "Read")
	remoteFile := defaultWorkdir + "/e2e/hello.txt"
	if output := executeIntegrationTool(t, ctx, writeTool, map[string]any{
		"file_path": remoteFile,
		"content":   "hello opensandbox\n",
	}); !strings.Contains(output, "written successfully") {
		t.Fatalf("Write output mismatch: %q", output)
	}
	if output := executeIntegrationTool(t, ctx, readTool, map[string]any{
		"file_path": remoteFile,
	}); !strings.Contains(output, "hello opensandbox") {
		t.Fatalf("Read output mismatch: %q", output)
	}

	mcpScriptPath := defaultWorkdir + "/e2e/echo_mcp.py"
	mcpScript := `from mcp.server.fastmcp import FastMCP

mcp = FastMCP("agentscope-e2e")

@mcp.tool()
def echo(text: str) -> str:
    return text

if __name__ == "__main__":
    mcp.run()
`
	executeIntegrationTool(t, ctx, writeTool, map[string]any{
		"file_path": mcpScriptPath,
		"content":   mcpScript,
	})
	mcpClient := &testMCPClient{config: workspace.MCPClientConfig{
		Name:     "e2e-echo",
		Type:     workspace.MCPClientTypeStdio,
		Stateful: true,
		Stdio: &workspace.MCPStdioConfig{
			Command: defaultGatewayHome + "/.venv/bin/python",
			Args:    []string{mcpScriptPath},
			CWD:     defaultWorkdir,
		},
		ExecutionTimeout: 30 * time.Second,
	}}
	if err := workspaceInstance.AddMCP(ctx, mcpClient); err != nil {
		t.Fatalf(
			"AddMCP returned error: %v\n%s",
			err,
			openSandboxFailureDiagnostics(ctx, workspaceInstance, mcpScriptPath),
		)
	}
	mcps, err := workspaceInstance.ListMCPs(ctx)
	if err != nil || len(mcps) != 1 || mcps[0].Name() != "e2e-echo" {
		t.Fatalf("ListMCPs after AddMCP = %#v, %v", mcps, err)
	}
	mcpTools, err := mcps[0].ListTools(ctx)
	if err != nil || len(mcpTools) != 1 || mcpTools[0].Name() != "mcp__e2e-echo__echo" {
		t.Fatalf("MCP ListTools = %#v, %v", mcpTools, err)
	}
	if output := executeIntegrationTool(t, ctx, mcpTools[0], map[string]any{"text": "hello-mcp"}); !strings.Contains(output, "hello-mcp") {
		t.Fatalf("MCP echo output mismatch: %q", output)
	}
	if err := workspaceInstance.RemoveMCP(ctx, "e2e-echo"); err != nil {
		t.Fatalf("RemoveMCP returned error: %v", err)
	}
	mcps, err = workspaceInstance.ListMCPs(ctx)
	if err != nil || len(mcps) != 0 {
		t.Fatalf("ListMCPs after RemoveMCP = %#v, %v", mcps, err)
	}
}

func randomTestSuffix(t *testing.T) string {
	t.Helper()
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand.Read returned error: %v", err)
	}
	return hex.EncodeToString(data)
}

func findIntegrationTool(t *testing.T, tools []workspace.Tool, name string) workspace.Tool {
	t.Helper()
	for _, current := range tools {
		if current.Name() == name {
			return current
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func executeIntegrationTool(
	t *testing.T,
	ctx context.Context,
	target workspace.Tool,
	input map[string]any,
) string {
	t.Helper()
	chunks, err := target.Execute(ctx, input, nil)
	if err != nil {
		t.Fatalf("tool %q returned error: %v", target.Name(), err)
	}
	var output strings.Builder
	var terminalState message.ToolResultState
	for chunk := range chunks {
		for _, block := range chunk.Content {
			if text, ok := block.(*message.TextBlock); ok {
				output.WriteString(text.Text)
			}
		}
		if chunk.State == message.ToolResultError || chunk.State == message.ToolResultInterrupted {
			terminalState = chunk.State
		}
	}
	if terminalState != "" {
		t.Fatalf(
			"tool %q returned terminal state %q: %s",
			target.Name(),
			terminalState,
			output.String(),
		)
	}
	return output.String()
}

func openSandboxFailureDiagnostics(
	ctx context.Context,
	workspaceInstance *Workspace,
	mcpScriptPath string,
) string {
	var output strings.Builder
	if workspaceInstance == nil || workspaceInstance.provider == nil {
		return "diagnostics unavailable: workspace provider is nil"
	}

	workspaceInstance.provider.mu.Lock()
	handle := workspaceInstance.provider.handle
	workspaceInstance.provider.mu.Unlock()
	if handle == nil {
		return "diagnostics unavailable: sandbox handle is nil"
	}

	diagnosticCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	gatewayLog, err := handle.ReadFile(
		diagnosticCtx,
		defaultGatewayHome+"/gateway.log",
	)
	if err != nil {
		fmt.Fprintf(&output, "gateway log error: %v\n", err)
	} else {
		fmt.Fprintf(&output, "gateway log:\n%s\n", gatewayLog)
	}

	versionScript := `import importlib.metadata as m
for package in ("agentscope", "mcp", "fastapi", "uvicorn"):
    print(f"{package}={m.version(package)}")
`
	appendDiagnosticCommand(
		&output,
		"python package versions",
		handle,
		diagnosticCtx,
		[]string{
			defaultGatewayHome + "/.venv/bin/python",
			"-c",
			versionScript,
		},
	)
	appendDiagnosticCommand(
		&output,
		"MCP server probe",
		handle,
		diagnosticCtx,
		[]string{
			"timeout",
			"5s",
			defaultGatewayHome + "/.venv/bin/python",
			mcpScriptPath,
		},
	)
	return output.String()
}

func appendDiagnosticCommand(
	output *strings.Builder,
	name string,
	handle sandboxHandle,
	ctx context.Context,
	argv []string,
) {
	result, err := handle.Run(ctx, argv, defaultWorkdir, nil, 10*time.Second)
	fmt.Fprintf(
		output,
		"%s: error=%v exit=%d\nstdout:\n%s\nstderr:\n%s\n",
		name,
		err,
		result.ExitCode,
		result.Stdout,
		result.Stderr,
	)
}

func cleanupOpenSandboxes(
	t *testing.T,
	ctx context.Context,
	connection sdk.ConnectionConfig,
	workspaceID string,
	knownSandboxID string,
) {
	t.Helper()
	manager := sdk.NewSandboxManager(connection)
	defer func() {
		if err := manager.Close(); err != nil {
			t.Logf("close sandbox manager: %v", err)
		}
	}()
	targets := map[string]bool{}
	if knownSandboxID != "" {
		targets[knownSandboxID] = true
	}
	for page := 1; ; page++ {
		response, err := manager.ListSandboxInfos(ctx, sdk.ListOptions{
			Metadata: map[string]string{metadataWorkspaceID: workspaceID},
			Page:     page,
			PageSize: 100,
		})
		if err != nil {
			t.Logf("list sandboxes during cleanup: %v", err)
			break
		}
		for _, item := range response.Items {
			if item.Status.State != sdk.StateTerminated {
				targets[item.ID] = true
			}
		}
		if !response.Pagination.HasNextPage {
			break
		}
	}
	for sandboxID := range targets {
		if err := manager.KillSandbox(ctx, sandboxID); err != nil {
			t.Logf("kill sandbox %q during cleanup: %v", sandboxID, err)
		}
	}
}
