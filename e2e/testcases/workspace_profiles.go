package testcases

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	pkgtestcases "github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
	agentsandboxworkspace "github.com/yuluo-yx/agentscope-go/workspace/agentsandbox"
	dockerworkspace "github.com/yuluo-yx/agentscope-go/workspace/docker"
)

func init() {
	pkgtestcases.Register("workspace-docker-agent-loop", pkgtestcases.TestCase{
		Description: "Docker workspace tools complete an Agent write/read loop",
		Tags:        []string{"docker", "workspace", "agent"},
		Fn:          testDockerWorkspaceAgentLoop,
	})
	pkgtestcases.Register("workspace-agent-sandbox-agent-loop", pkgtestcases.TestCase{
		Description: "Agent Sandbox workspace tools complete an Agent write/read loop",
		Tags:        []string{"agent-sandbox", "workspace", "agent"},
		Fn:          testAgentSandboxWorkspaceAgentLoop,
	})
}

func testDockerWorkspaceAgentLoop(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	hostWorkdir := filepath.Join(opts.WorkDir, "host")
	if err := os.MkdirAll(hostWorkdir, 0o755); err != nil {
		return err
	}
	image := strings.TrimSpace(os.Getenv("AGENTSCOPE_DOCKER_IMAGE"))
	if image == "" {
		image = "ubuntu:latest"
	}
	ws, err := dockerworkspace.NewWorkspace(
		dockerworkspace.WithWorkspaceID("docker-workspace-e2e"),
		dockerworkspace.WithImage(image),
		dockerworkspace.WithHostWorkdir(hostWorkdir),
		dockerworkspace.WithPullImage(false),
		dockerworkspace.WithNetworkDisabled(true),
	)
	if err != nil {
		return err
	}
	if err := ws.Initialize(ctx); err != nil {
		return err
	}
	defer ws.Close(context.Background())
	notePath := "/workspace/data/e2e-note.txt"
	noteText := "docker workspace note\ncreated by agent e2e"
	if err := exerciseWorkspaceAgentLoop(ctx, ws, "/workspace", notePath, noteText, "docker workspace verified"); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(hostWorkdir, "data", "e2e-note.txt"))
	if err != nil {
		return err
	}
	if string(data) != noteText {
		return fmt.Errorf("Docker workspace bind mount file content mismatch: %q", string(data))
	}
	return nil
}

func testAgentSandboxWorkspaceAgentLoop(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	hostWorkdir := filepath.Join(opts.WorkDir, "host")
	if err := os.MkdirAll(hostWorkdir, 0o755); err != nil {
		return err
	}
	ws, err := agentsandboxworkspace.NewWorkspace(agentSandboxOptions(hostWorkdir)...)
	if err != nil {
		return err
	}
	if err := ws.Initialize(ctx); err != nil {
		return err
	}
	defer ws.Close(context.Background())
	notePath := envOrDefault("AGENTSCOPE_AGENT_SANDBOX_TEST_FILE", "/home/user/data/e2e-note.txt")
	noteText := "agent sandbox workspace note\ncreated by agent e2e"
	return exerciseWorkspaceAgentLoop(ctx, ws, "/home/user", notePath, noteText, "agent sandbox workspace verified")
}

func exerciseWorkspaceAgentLoop(ctx context.Context, ws asworkspace.Workspace, permissionRoot, notePath, noteText, finalText string) error {
	tools, err := ws.ListTools(ctx)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(tools...)
	if err != nil {
		return err
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("write-call", "Write", mustJSONInput(map[string]any{"file_path": notePath, "content": noteText}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("read-call", "Read", mustJSONInput(map[string]any{"file_path": notePath, "limit": 5}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock(finalText)}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	state.PermissionContext.WorkingDirectories["workspace"] = permission.AdditionalWorkingDirectory{Path: permissionRoot, Source: "e2e"}
	agent, err := agentpkg.NewAgent("Friday", "Use workspace tools.", model, agentpkg.WithToolkit(kit), agentpkg.WithAgentState(state))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Create and read a workspace note")
	if err != nil {
		return err
	}
	reply, err := agent.Reply(ctx, userMsg)
	if err != nil {
		return err
	}
	if text := reply.GetTextContent(""); text == nil || *text != finalText {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}
	result, err := lastToolResultFromLastRequest(model)
	if err != nil {
		return err
	}
	if text := result.Output.Blocks.GetTextContent(""); result.Name != "Read" || text == nil || !strings.Contains(*text, strings.Split(noteText, "\n")[0]) {
		return fmt.Errorf("read tool result should be passed back to final model call, got %#v text=%#v", result, text)
	}
	if err := exerciseWorkspaceOffloadContract(ctx, ws, result); err != nil {
		return err
	}
	return nil
}

func exerciseWorkspaceOffloadContract(ctx context.Context, ws asworkspace.Workspace, readResult *message.ToolResultBlock) error {
	userMsg, err := message.NewUserMessage("Tony", message.ContentBlockList{
		message.NewTextBlock("Keep this workspace context."),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"), message.WithDataBlockName("hello.txt")),
	})
	if err != nil {
		return err
	}
	contextPath, err := ws.OffloadContext(ctx, "session-e2e", []*message.Message{userMsg})
	if err != nil {
		return err
	}
	if !fileExists(contextPath) {
		return fmt.Errorf("offloaded context file does not exist: %s", contextPath)
	}
	resultPath, err := ws.OffloadToolResult(ctx, "session-e2e", readResult)
	if err != nil {
		return err
	}
	if !fileExists(resultPath) {
		return fmt.Errorf("offloaded tool result file does not exist: %s", resultPath)
	}
	offloaded, err := ws.OffloadDataBlock(ctx, message.NewDataBlock(
		message.NewBase64Source("aGVsbG8=", "text/plain"),
		message.WithDataBlockName("hello.txt"),
	))
	if err != nil {
		return err
	}
	if offloaded == nil || offloaded.Source == nil || offloaded.Source.SourceType() != "url" {
		return fmt.Errorf("OffloadDataBlock should return a URL-backed block, got %#v", offloaded)
	}
	return nil
}

func agentSandboxOptions(hostWorkdir string) []agentsandboxworkspace.Option {
	opts := []agentsandboxworkspace.Option{
		agentsandboxworkspace.WithWorkspaceID("agent-sandbox-e2e"),
		agentsandboxworkspace.WithTemplateName(envOrDefault("AGENTSCOPE_AGENT_SANDBOX_TEMPLATE", "python-sandbox-template")),
		agentsandboxworkspace.WithNamespace(envOrDefault("AGENTSCOPE_AGENT_SANDBOX_NAMESPACE", "default")),
		agentsandboxworkspace.WithHostWorkdir(hostWorkdir),
	}
	if apiURL := strings.TrimSpace(os.Getenv("AGENTSCOPE_AGENT_SANDBOX_API_URL")); apiURL != "" {
		return append(opts, agentsandboxworkspace.WithAPIURL(apiURL))
	}
	if gateway := strings.TrimSpace(os.Getenv("AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAME")); gateway != "" {
		opts = append(opts, agentsandboxworkspace.WithGateway(
			gateway,
			envOrDefault("AGENTSCOPE_AGENT_SANDBOX_GATEWAY_NAMESPACE", "default"),
		))
	}
	return opts
}
