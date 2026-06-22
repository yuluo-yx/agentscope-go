package testcases

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pkgtestcases "github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	builtintool "github.com/yuluo-yx/agentscope-go/pkg/tool/builtin"
	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
	agentsandboxworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace/agentsandbox"
	dockerworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace/docker"
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
	pkgtestcases.Register("workspace-agent-sandbox-builtin-tools-loop", pkgtestcases.TestCase{
		Description: "Agent Sandbox workspace drives Bash, Write, Read, Edit, Glob, and Grep through WithWorkspace and streaming Agent events",
		Tags:        []string{"agent-sandbox", "workspace", "agent", "tool"},
		Fn:          testAgentSandboxWorkspaceBuiltinToolsLoop,
	})
	pkgtestcases.Register("workspace-agent-sandbox-resource-lifecycle", pkgtestcases.TestCase{
		Description: "Agent Sandbox workspace instructions, tools, skills, MCP metadata, removal, and reset work together",
		Tags:        []string{"agent-sandbox", "workspace", "skill", "mcp"},
		Fn:          testAgentSandboxWorkspaceResourceLifecycle,
	})
	pkgtestcases.Register("workspace-agent-sandbox-tool-error-boundaries", pkgtestcases.TestCase{
		Description: "Agent Sandbox workspace tools return clear results for runtime and validation error paths",
		Tags:        []string{"agent-sandbox", "workspace", "tool", "error"},
		Fn:          testAgentSandboxWorkspaceToolErrorBoundaries,
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

func testAgentSandboxWorkspaceBuiltinToolsLoop(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	hostWorkdir := filepath.Join(opts.WorkDir, "host")
	if err := os.MkdirAll(hostWorkdir, 0o755); err != nil {
		return err
	}
	ws, err := agentsandboxworkspace.NewWorkspace(agentSandboxOptions(hostWorkdir)...)
	if err != nil {
		return err
	}
	defer ws.Close(context.Background())

	notePath := "/home/user/data/e2e-tools.md"
	noteText := "alpha\nneedle line\n"
	model := &scriptedChatModel{name: "scripted-agent-sandbox-tools-e2e", responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("bash-call", "Bash", mustJSONInput(map[string]any{"command": "pwd", "description": "Print sandbox working directory"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("write-call", "Write", mustJSONInput(map[string]any{"file_path": notePath, "content": noteText}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("read-call", "Read", mustJSONInput(map[string]any{"file_path": notePath, "limit": 10}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("edit-call", "Edit", mustJSONInput(map[string]any{"file_path": notePath, "old_string": "needle", "new_string": "verified"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("glob-call", "Glob", mustJSONInput(map[string]any{"path": "/home/user/data", "pattern": "*.md"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("grep-call", "Grep", mustJSONInput(map[string]any{"path": "/home/user/data", "pattern": "verified", "glob": "*.md", "output_mode": "content"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("agent sandbox builtin tools verified")}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	state.PermissionContext.WorkingDirectories["workspace"] = permission.AdditionalWorkingDirectory{Path: "/home/user", Source: "e2e"}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Use Agent Sandbox workspace tools.",
		model,
		agentpkg.WithAgentState(state),
		agentpkg.WithWorkspace(ctx, ws),
		agentpkg.WithReActConfig(agentpkg.ReActConfig{MaxIters: 12}),
	)
	if err != nil {
		return err
	}
	if !ws.IsAlive() {
		return fmt.Errorf("agent.WithWorkspace should initialize Agent Sandbox workspace")
	}
	userMsg, err := message.NewUserMessage("Tony", "Exercise Agent Sandbox workspace tools")
	if err != nil {
		return err
	}
	var events []message.Event
	if err := agent.ReplyStream(ctx, userMsg, func(evt message.Event) error {
		events = append(events, evt)
		return nil
	}); err != nil {
		return err
	}
	if err := assertEventOrder(events, message.ToolCallStartType, message.ToolResultEndType, message.TextBlockDeltaType, message.ReplyEndType); err != nil {
		return err
	}
	if len(model.requests) != 7 {
		return fmt.Errorf("expected 7 model requests for six tool calls and final answer, got %d", len(model.requests))
	}
	for _, toolName := range []string{"Bash", "Edit", "Glob", "Grep", "Read", "Write"} {
		if !requestIncludesTool(model.requests[0], toolName) {
			return fmt.Errorf("WithWorkspace should expose %s tool to the model", toolName)
		}
	}
	checks := []struct {
		requestIndex int
		name         string
		want         string
	}{
		{requestIndex: 1, name: "Bash", want: "/home/user"},
		{requestIndex: 2, name: "Write", want: "written successfully inside the Agent Sandbox workspace"},
		{requestIndex: 3, name: "Read", want: "needle line"},
		{requestIndex: 4, name: "Edit", want: "Successfully replaced 1 occurrence"},
		{requestIndex: 5, name: "Glob", want: notePath},
		{requestIndex: 6, name: "Grep", want: "verified line"},
	}
	for _, check := range checks {
		result, err := lastToolResultFromRequest(model.requests[check.requestIndex])
		if err != nil {
			return fmt.Errorf("request %d %s result: %w", check.requestIndex, check.name, err)
		}
		text := result.Output.Blocks.GetTextContent("")
		if result.Name != check.name || result.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, check.want) {
			return fmt.Errorf("%s result mismatch: %#v text=%#v", check.name, result, text)
		}
	}
	final := agent.AgentState().Context[len(agent.AgentState().Context)-1]
	if text := final.GetTextContent(""); text == nil || *text != "agent sandbox builtin tools verified" {
		return fmt.Errorf("final assistant text mismatch: %#v", final)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"model_calls": len(model.requests), "tools": []string{"Bash", "Write", "Read", "Edit", "Glob", "Grep"}})
	}
	return nil
}

func testAgentSandboxWorkspaceResourceLifecycle(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	hostWorkdir := filepath.Join(opts.WorkDir, "host")
	sourceSkillDir := filepath.Join(opts.WorkDir, "source-skill")
	if err := os.MkdirAll(sourceSkillDir, 0o755); err != nil {
		return err
	}
	skillMarkdown := "---\nname: planning\ndescription: Plan Agent Sandbox e2e work.\n---\n# Planning\nUse a sandbox checklist.\n"
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), []byte(skillMarkdown), 0o600); err != nil {
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

	instructions, err := ws.GetInstructions(ctx)
	if err != nil {
		return err
	}
	if !strings.Contains(instructions, "Agent Sandbox") || !strings.Contains(instructions, "/home/user") {
		return fmt.Errorf("Agent Sandbox instructions should describe runtime and workdir, got %q", instructions)
	}
	tools, err := ws.ListTools(ctx)
	if err != nil {
		return err
	}
	if err := assertWorkspaceToolSet(tools, "Bash", "Edit", "Glob", "Grep", "Read", "Write"); err != nil {
		return err
	}
	if err := ws.AddSkill(ctx, sourceSkillDir); err != nil {
		return err
	}
	skills, err := ws.ListSkills(ctx)
	if err != nil {
		return err
	}
	if len(skills) != 1 || skills[0].Name != "planning" || !strings.Contains(skills[0].Markdown, "sandbox checklist") {
		return fmt.Errorf("unexpected Agent Sandbox workspace skills: %#v", skills)
	}
	viewer := builtintool.NewSkillViewer(skills)
	kit, err := tool.NewToolkit(viewer)
	if err != nil {
		return err
	}
	viewed, err := kit.RunTool(ctx, message.NewToolCallBlock("skill-call", "Skill", `{"skill":"planning"}`), asstate.NewAgentState())
	if err != nil {
		return err
	}
	if text := viewed.GetTextContent(""); viewed.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, "sandbox checklist") {
		return fmt.Errorf("Skill viewer should return Agent Sandbox skill body, got %#v text=%#v", viewed, text)
	}
	mcp := &workspaceLifecycleMCP{config: asworkspace.MCPClientConfig{
		Name:     "sandbox-notes",
		Type:     asworkspace.MCPClientTypeHTTP,
		Stateful: false,
		HTTP:     &asworkspace.MCPHTTPConfig{URL: "https://example.invalid/agent-sandbox-mcp"},
	}}
	if err := ws.AddMCP(ctx, mcp); err != nil {
		return err
	}
	mcps, err := ws.ListMCPs(ctx)
	if err != nil {
		return err
	}
	if len(mcps) != 1 || mcps[0].Name() != "sandbox-notes" {
		return fmt.Errorf("unexpected Agent Sandbox workspace MCP list after add: %#v", mcps)
	}
	if err := ws.RemoveMCP(ctx, "sandbox-notes"); err != nil {
		return err
	}
	mcps, err = ws.ListMCPs(ctx)
	if err != nil {
		return err
	}
	if len(mcps) != 0 {
		return fmt.Errorf("Agent Sandbox workspace MCP should be removed, got %#v", mcps)
	}
	if err := ws.RemoveSkill(ctx, "planning"); err != nil {
		return err
	}
	skills, err = ws.ListSkills(ctx)
	if err != nil {
		return err
	}
	if len(skills) != 0 {
		return fmt.Errorf("Agent Sandbox workspace skill should be removed, got %#v", skills)
	}
	if err := ws.Reset(ctx); err != nil {
		return err
	}
	if ws.IsAlive() {
		return fmt.Errorf("Reset should mark Agent Sandbox workspace inactive")
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"workspace_id": ws.WorkspaceID(), "skill": "planning", "mcp": "sandbox-notes"})
	}
	return nil
}

func testAgentSandboxWorkspaceToolErrorBoundaries(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
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

	tools, err := ws.ListTools(ctx)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(tools...)
	if err != nil {
		return err
	}
	state := asstate.NewAgentState()
	notePath := "/home/user/data/error-boundaries.md"
	setup, err := runWorkspaceTool(ctx, kit, "Write", map[string]any{"file_path": notePath, "content": "alpha\n"}, state)
	if err != nil {
		return err
	}
	if text := setup.GetTextContent(""); setup.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, "written successfully") {
		return fmt.Errorf("setup Write should succeed, got %#v text=%#v", setup, text)
	}
	checks := []struct {
		name      string
		input     map[string]any
		wantState message.ToolResultState
		wantText  string
	}{
		{name: "Bash", input: map[string]any{"command": "exit 7", "description": "Return a non-zero status"}, wantState: message.ToolResultError, wantText: "Command failed with exit code 7"},
		{name: "Read", input: map[string]any{"file_path": "/home/user/data/missing.md"}, wantState: message.ToolResultError, wantText: "Error reading file"},
		{name: "Edit", input: map[string]any{"file_path": notePath, "old_string": "missing", "new_string": "verified"}, wantState: message.ToolResultError, wantText: "old_string not found"},
		{name: "Glob", input: map[string]any{"path": "/home/user/data", "pattern": "*.missing"}, wantState: message.ToolResultSuccess, wantText: "No files found matching pattern"},
		{name: "Grep", input: map[string]any{"path": "/home/user/data", "pattern": "["}, wantState: message.ToolResultError, wantText: "invalid regex pattern"},
		{name: "Grep", input: map[string]any{"path": "/home/user/data", "pattern": "not-present"}, wantState: message.ToolResultSuccess, wantText: "No matches found"},
	}
	for _, check := range checks {
		response, err := runWorkspaceTool(ctx, kit, check.name, check.input, state)
		if err != nil {
			return err
		}
		text := response.GetTextContent("")
		if response.State != check.wantState || text == nil || !strings.Contains(*text, check.wantText) {
			return fmt.Errorf("%s boundary result mismatch: state=%s text=%#v want state=%s text containing %q", check.name, response.State, text, check.wantState, check.wantText)
		}
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"checked_results": len(checks), "workspace_id": ws.WorkspaceID()})
	}
	return nil
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

func lastToolResultFromRequest(request modelpkg.CallRequest) (*message.ToolResultBlock, error) {
	if len(request.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	for msgIndex := len(request.Messages) - 1; msgIndex >= 0; msgIndex-- {
		blocks := request.Messages[msgIndex].GetContentBlocks("tool_result")
		for blockIndex := len(blocks) - 1; blockIndex >= 0; blockIndex-- {
			result, ok := blocks[blockIndex].(*message.ToolResultBlock)
			if !ok {
				return nil, fmt.Errorf("tool_result block has unexpected type %T", blocks[blockIndex])
			}
			return result, nil
		}
	}
	return nil, fmt.Errorf("request has no tool result")
}

func runWorkspaceTool(ctx context.Context, kit *tool.Toolkit, name string, input map[string]any, state *asstate.AgentState) (*tool.ToolResponse, error) {
	return kit.RunTool(ctx, message.NewToolCallBlock("call-"+strings.ToLower(name), name, mustJSONInput(input)), state)
}

func assertWorkspaceToolSet(tools []asworkspace.Tool, want ...string) error {
	found := map[string]bool{}
	for _, current := range tools {
		found[current.Name()] = true
	}
	for _, name := range want {
		if !found[name] {
			return fmt.Errorf("missing workspace tool %q in %#v", name, found)
		}
	}
	if len(found) != len(want) {
		return fmt.Errorf("unexpected workspace tools: got %#v want %#v", found, want)
	}
	return nil
}
