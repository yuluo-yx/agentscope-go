package testcases

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	pkgtestcases "github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	agentscope "github.com/yuluo-yx/agentscope-go/pkg/agentscope"
	asembedding "github.com/yuluo-yx/agentscope-go/pkg/embedding"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	builtintool "github.com/yuluo-yx/agentscope-go/pkg/tool/builtin"
	tasktool "github.com/yuluo-yx/agentscope-go/pkg/tool/task"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
	wslocal "github.com/yuluo-yx/agentscope-go/pkg/workspace/local"
)

func init() {
	pkgtestcases.Register("embedding-cache-contract", pkgtestcases.TestCase{
		Description: "Embedding request validation, cache identity, file cache, and response clone contracts hold together",
		Tags:        []string{"local", "embedding", "cache"},
		Fn:          testEmbeddingCacheContract,
	})
	pkgtestcases.Register("builtin-tools-agent-loop", pkgtestcases.TestCase{
		Description: "Agent can drive local Bash, Write, Read, Edit, Glob, and Grep tools in one workflow",
		Tags:        []string{"local", "tool", "workspace", "agent"},
		Fn:          testBuiltinToolsAgentLoop,
	})
	pkgtestcases.Register("task-tools-lifecycle", pkgtestcases.TestCase{
		Description: "TaskCreate, TaskGet, TaskList, and TaskUpdate support create/update/delete lifecycle through Agent state",
		Tags:        []string{"local", "task", "state", "agent"},
		Fn:          testTaskToolsLifecycle,
	})
	pkgtestcases.Register("workspace-resource-lifecycle", pkgtestcases.TestCase{
		Description: "Local workspace instructions, skills, MCP persistence metadata, removal, and reset work together",
		Tags:        []string{"local", "workspace", "skill", "mcp"},
		Fn:          testWorkspaceResourceLifecycle,
	})
	pkgtestcases.Register("facade-package-contract", pkgtestcases.TestCase{
		Description: "Facade aliases compose with domain packages for Agent, state, model, and tool APIs",
		Tags:        []string{"local", "facade", "architecture"},
		Fn:          testFacadePackageContract,
	})
	pkgtestcases.Register("gateway-http-edge-contracts", pkgtestcases.TestCase{
		Description: "Workspace gateway edge contracts cover auth, Python-compatible MCP routes, health failures, and lifecycle",
		Tags:        []string{"local", "gateway", "http"},
		Fn: func(ctx context.Context, _ pkgtestcases.TestCaseOptions) error {
			return runRepoGoTest(ctx, "github.com/yuluo-yx/agentscope-go/pkg/workspace/gateway", "-run", "TestHTTPGateway(BootstrapsRegistersMCPExposesToolsAndCloses|ReportsHealthFailure|MCPClientUsesPythonCompatibleRoutesAndAuth)$", "-count=1")
		},
	})
	pkgtestcases.Register("message-state-types-contracts", pkgtestcases.TestCase{
		Description: "Message JSON/events, state cache/tasks, and shared type contracts stay compatible",
		Tags:        []string{"local", "message", "state", "types"},
		Fn: func(ctx context.Context, _ pkgtestcases.TestCaseOptions) error {
			return runRepoGoTest(ctx,
				"github.com/yuluo-yx/agentscope-go/pkg/message",
				"github.com/yuluo-yx/agentscope-go/pkg/state",
				"github.com/yuluo-yx/agentscope-go/pkg/types",
				"-run", "Test(ContentBlockJSONRoundTrip|PythonEventGoldenFixtureRoundTrip|ApplyEventAccumulatesStreamingMessage|AgentStateDefaultsAndClone|ToolContextCachesFilesWithLRUEviction|TaskLifecycleHelpers|ToolChoiceValidation|JSONSerializableValidation)$",
				"-count=1",
			)
		},
	})
	pkgtestcases.Register("model-provider-metadata-contracts", pkgtestcases.TestCase{
		Description: "Provider metadata and compatibility registry expose complete model cards",
		Tags:        []string{"local", "model", "metadata"},
		Fn: func(ctx context.Context, _ pkgtestcases.TestCaseOptions) error {
			return runRepoGoTest(ctx, "github.com/yuluo-yx/agentscope-go/pkg/model", "-run", "Test(EveryProviderExposesCompleteModelMetadata|OpenAIResponsesUsesDedicatedProviderPackage|CompatibleProviderConfigValidationAndClone)$", "-count=1")
		},
	})
}

func testEmbeddingCacheContract(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	request := asembedding.EmbeddingRequest{
		Inputs: []asembedding.EmbeddingInput{
			asembedding.NewTextInput("hello agentscope"),
			asembedding.NewImageURLInput("https://example.com/image.png", "image/png"),
			asembedding.NewImageBase64Input("aGVsbG8=", "image/png"),
			asembedding.NewVideoURLInput("https://example.com/video.mp4", "video/mp4"),
		},
		Parameters: map[string]any{"purpose": "e2e"},
		Metadata:   map[string]any{"trace": "embedding-cache-contract"},
	}
	if err := request.Validate(asembedding.ModalityText, asembedding.ModalityImage, asembedding.ModalityVideo); err != nil {
		return err
	}
	if err := (asembedding.EmbeddingRequest{Inputs: []asembedding.EmbeddingInput{asembedding.NewImageURLInput("https://example.com/image.png", "image/png")}}).Validate(asembedding.ModalityText); !errors.Is(err, asembedding.ErrUnsupportedModality) {
		return fmt.Errorf("text-only validation should reject image input, got %v", err)
	}
	identifier := asembedding.CacheIdentifier("local-e2e", "deterministic", 3, request)
	cache, err := asembedding.NewFileCache(filepath.Join(opts.WorkDir, "embedding-cache"), asembedding.WithMaxFileNumber(4), asembedding.WithMaxCacheSizeBytes(32*1024))
	if err != nil {
		return err
	}
	embeddings := []types.Embedding{{0.1, 0.2, 0.3}}
	if err := cache.Store(ctx, identifier, embeddings, asembedding.StoreOptions{Overwrite: true}); err != nil {
		return err
	}
	cached, ok, err := cache.Retrieve(ctx, identifier)
	if err != nil {
		return err
	}
	if !ok || len(cached) != 1 || len(cached[0]) != 3 || cached[0][2] != 0.3 {
		return fmt.Errorf("unexpected cached embedding: ok=%t value=%#v", ok, cached)
	}
	tokens := 7
	response := asembedding.NewEmbeddingResponse(cached, asembedding.WithEmbeddingUsage(&asembedding.EmbeddingUsage{Tokens: &tokens}), asembedding.WithEmbeddingSource(asembedding.SourceCache))
	clone := response.Clone()
	clone.Embeddings[0][0] = 9
	*clone.Usage.Tokens = 99
	if response.Embeddings[0][0] == 9 || *response.Usage.Tokens == 99 {
		return fmt.Errorf("embedding response Clone should deep-copy embeddings and usage")
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"cache_dir": cache.Dir(), "identifier_len": len(identifier)})
	}
	return nil
}

func testBuiltinToolsAgentLoop(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	workdir := filepath.Join(opts.WorkDir, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return err
	}
	ws, err := wslocal.NewWorkspace(workdir, wslocal.WithWorkspaceID("builtin-tools-e2e"))
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
	notePath := filepath.Join(workdir, "notes.md")
	model := &scriptedChatModel{name: "scripted-builtin-tools-e2e", responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("bash-call", "Bash", mustJSONInput(map[string]any{"command": "printf shell-ready", "description": "Check shell execution"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("write-call", "Write", mustJSONInput(map[string]any{"file_path": notePath, "content": "alpha\nneedle line\n"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("read-call", "Read", mustJSONInput(map[string]any{"file_path": notePath, "limit": 10}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("edit-call", "Edit", mustJSONInput(map[string]any{"file_path": notePath, "old_string": "needle", "new_string": "verified"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("glob-call", "Glob", mustJSONInput(map[string]any{"path": workdir, "pattern": "*.md"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("grep-call", "Grep", mustJSONInput(map[string]any{"path": workdir, "pattern": "verified", "glob": "*.md", "output_mode": "content"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("builtin tools verified")}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	state.PermissionContext.WorkingDirectories["workspace"] = permission.AdditionalWorkingDirectory{Path: workdir, Source: "e2e"}
	agent, err := agentpkg.NewAgent("Friday", "Use local workspace tools.", model, agentpkg.WithToolkit(kit), agentpkg.WithAgentState(state), agentpkg.WithReActConfig(agentpkg.ReActConfig{MaxIters: 10}))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Exercise local tools")
	if err != nil {
		return err
	}
	reply, err := agent.Reply(ctx, userMsg)
	if err != nil {
		return err
	}
	if text := reply.GetTextContent(""); text == nil || *text != "builtin tools verified" {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}
	data, err := os.ReadFile(notePath)
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), "verified line") {
		return fmt.Errorf("Edit should update workspace note, got %q", string(data))
	}
	result, err := lastToolResultFromLastRequest(model)
	if err != nil {
		return err
	}
	if text := result.Output.Blocks.GetTextContent(""); result.Name != "Grep" || text == nil || !strings.Contains(*text, "verified line") {
		return fmt.Errorf("Grep result should be passed to final model call, got %#v text=%#v", result, text)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"model_calls": len(model.requests), "tools": []string{"Bash", "Write", "Read", "Edit", "Glob", "Grep"}})
	}
	return nil
}

func testTaskToolsLifecycle(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	kit, err := tool.NewToolkit(tasktool.NewTools()...)
	if err != nil {
		return err
	}
	model := &taskLifecycleChatModel{}
	agent, err := agentpkg.NewAgent("Friday", "Manage tasks.", model, agentpkg.WithToolkit(kit), agentpkg.WithReActConfig(agentpkg.ReActConfig{MaxIters: 12}))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Create, inspect, update, list, and delete tasks")
	if err != nil {
		return err
	}
	reply, err := agent.Reply(ctx, userMsg)
	if err != nil {
		return err
	}
	if text := reply.GetTextContent(""); text == nil || *text != "task lifecycle verified" {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}
	tasks := agent.AgentState().TaskContext.Tasks
	if len(tasks) != 1 {
		return fmt.Errorf("expected one remaining task after deleting cleanup task, got %#v", tasks)
	}
	if tasks[0].Subject != "Primary e2e task" || tasks[0].State != asstate.TaskCompleted || tasks[0].Owner == nil || *tasks[0].Owner != "e2e" || tasks[0].Metadata["phase"] != "verified" {
		return fmt.Errorf("remaining task state mismatch: %#v", tasks[0])
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"model_calls": len(model.requests), "remaining_task": tasks[0].ID})
	}
	return nil
}

func testFacadePackageContract(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	state := agentscope.NewAgentState()
	if state.SessionID == "" || state.PermissionContext == nil || state.TaskContext == nil {
		return fmt.Errorf("root facade NewAgentState should initialize runtime state: %#v", state)
	}
	model := &scriptedChatModel{name: "facade-contract", responses: []*modelpkg.ChatResponse{
		agentscope.NewChatResponse(message.ContentBlockList{message.NewTextBlock("facade ok")}, true,
			agentscope.WithChatResponseUsage(&agentscope.ChatUsage{InputTokens: 1, OutputTokens: 2})),
	}}
	agent, err := agentscope.NewAgent(
		"Friday",
		"Use root facade aliases.",
		model,
		agentscope.WithAgentState(state),
		agentscope.WithReActConfig(agentpkg.ReActConfig{MaxIters: 3}),
	)
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Check facade")
	if err != nil {
		return err
	}
	reply, err := agent.Reply(ctx, userMsg)
	if err != nil {
		return err
	}
	if agent.AgentName() != "Friday" {
		return fmt.Errorf("root facade Agent alias mismatch: %q", agent.AgentName())
	}
	if text := reply.GetTextContent(""); text == nil || *text != "facade ok" {
		return fmt.Errorf("root facade agent reply mismatch: %#v", reply)
	}
	chunk := agentscope.NewToolChunk(
		message.ContentBlockList{message.NewTextBlock("chunk ok")},
		agentscope.WithToolChunkMetadata(map[string]any{"facade": "ok"}),
	)
	if chunk.Metadata["facade"] != "ok" {
		return fmt.Errorf("root facade ToolChunk alias mismatch: %#v", chunk)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"model_calls": len(model.requests), "session_id_set": state.SessionID != ""})
	}
	return nil
}

func testWorkspaceResourceLifecycle(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	workdir := filepath.Join(opts.WorkDir, "workspace")
	sourceSkillDir := filepath.Join(opts.WorkDir, "source-skill")
	if err := os.MkdirAll(sourceSkillDir, 0o755); err != nil {
		return err
	}
	skillMarkdown := "---\nname: planning\ndescription: Plan e2e work.\n---\n# Planning\nUse a short checklist.\n"
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), []byte(skillMarkdown), 0o600); err != nil {
		return err
	}
	ws, err := wslocal.NewWorkspace(workdir, wslocal.WithWorkspaceID("resource-lifecycle-e2e"))
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
	if !strings.Contains(instructions, workdir) {
		return fmt.Errorf("workspace instructions should include workdir, got %q", instructions)
	}
	if err := ws.AddSkill(ctx, sourceSkillDir); err != nil {
		return err
	}
	skills, err := ws.ListSkills(ctx)
	if err != nil {
		return err
	}
	if len(skills) != 1 || skills[0].Name != "planning" || !strings.Contains(skills[0].Markdown, "short checklist") {
		return fmt.Errorf("unexpected workspace skills: %#v", skills)
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
	if text := viewed.GetTextContent(""); viewed.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, "short checklist") {
		return fmt.Errorf("Skill viewer should return skill body, got %#v text=%#v", viewed, text)
	}
	mcp := &workspaceLifecycleMCP{config: asworkspace.MCPClientConfig{
		Name:     "notes",
		Type:     asworkspace.MCPClientTypeHTTP,
		Stateful: false,
		HTTP:     &asworkspace.MCPHTTPConfig{URL: "https://example.invalid/mcp"},
	}}
	if err := ws.AddMCP(ctx, mcp); err != nil {
		return err
	}
	mcps, err := ws.ListMCPs(ctx)
	if err != nil {
		return err
	}
	if len(mcps) != 1 || mcps[0].Name() != "notes" {
		return fmt.Errorf("unexpected workspace MCP list after add: %#v", mcps)
	}
	if err := ws.RemoveMCP(ctx, "notes"); err != nil {
		return err
	}
	mcps, err = ws.ListMCPs(ctx)
	if err != nil {
		return err
	}
	if len(mcps) != 0 {
		return fmt.Errorf("workspace MCP should be removed, got %#v", mcps)
	}
	if err := ws.RemoveSkill(ctx, "planning"); err != nil {
		return err
	}
	skills, err = ws.ListSkills(ctx)
	if err != nil {
		return err
	}
	if len(skills) != 0 {
		return fmt.Errorf("workspace skill should be removed, got %#v", skills)
	}
	if err := ws.Reset(ctx); err != nil {
		return err
	}
	if ws.IsAlive() {
		return fmt.Errorf("Reset should mark local workspace inactive")
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"workspace_id": ws.WorkspaceID(), "skill": "planning", "mcp": "notes"})
	}
	return nil
}

type taskLifecycleChatModel struct {
	requests  []modelpkg.CallRequest
	primaryID string
	cleanupID string
}

func (m *taskLifecycleChatModel) Name() string { return "task-lifecycle-e2e" }

func (m *taskLifecycleChatModel) Call(_ context.Context, request modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	m.captureTaskID(request)
	switch len(m.requests) {
	case 1:
		return modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("create-primary", "TaskCreate", `{"subject":"Primary e2e task","description":"Track the primary task.","metadata":{"phase":"created"}}`)}, true), nil
	case 2:
		return modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("create-cleanup", "TaskCreate", `{"subject":"Cleanup e2e task","description":"Temporary task to delete.","metadata":{"phase":"cleanup"}}`)}, true), nil
	case 3:
		if m.primaryID == "" {
			return nil, fmt.Errorf("primary task ID was not captured")
		}
		return modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("get-primary", "TaskGet", mustJSONInput(map[string]any{"task_id": m.primaryID}))}, true), nil
	case 4:
		if m.cleanupID == "" {
			return nil, fmt.Errorf("cleanup task ID was not captured")
		}
		return modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("delete-cleanup", "TaskUpdate", mustJSONInput(map[string]any{"task_id": m.cleanupID, "status": "deleted"}))}, true), nil
	case 5:
		return modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("complete-primary", "TaskUpdate", mustJSONInput(map[string]any{"task_id": m.primaryID, "status": "completed", "owner": "e2e", "metadata": map[string]any{"phase": "verified"}}))}, true), nil
	case 6:
		return modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("list-tasks", "TaskList", `{}`)}, true), nil
	default:
		return modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("task lifecycle verified")}, true), nil
	}
}

func (m *taskLifecycleChatModel) Stream(ctx context.Context, request modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	response, err := m.Call(ctx, request)
	if err != nil {
		return nil, err
	}
	out := make(chan modelpkg.ChatResponse, 1)
	out <- *response
	close(out)
	return out, nil
}

func (m *taskLifecycleChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *taskLifecycleChatModel) captureTaskID(request modelpkg.CallRequest) {
	text := latestToolResultText(request)
	if text == "" {
		return
	}
	id := parseCreatedTaskID(text)
	if id == "" {
		return
	}
	if m.primaryID == "" {
		m.primaryID = id
		return
	}
	if m.cleanupID == "" && id != m.primaryID {
		m.cleanupID = id
	}
}

func latestToolResultText(request modelpkg.CallRequest) string {
	if len(request.Messages) == 0 {
		return ""
	}
	for msgIndex := len(request.Messages) - 1; msgIndex >= 0; msgIndex-- {
		blocks := request.Messages[msgIndex].GetContentBlocks("tool_result")
		for blockIndex := len(blocks) - 1; blockIndex >= 0; blockIndex-- {
			if result, ok := blocks[blockIndex].(*message.ToolResultBlock); ok {
				return valueOrEmpty(result.Output.Blocks.GetTextContent(""))
			}
		}
	}
	return ""
}

var createdTaskIDPattern = regexp.MustCompile(`Task ([a-f0-9-]+) created successfully`)

func parseCreatedTaskID(text string) string {
	matches := createdTaskIDPattern.FindStringSubmatch(text)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

type workspaceLifecycleMCP struct {
	config asworkspace.MCPClientConfig
}

func (m *workspaceLifecycleMCP) Name() string { return m.config.Name }

func (m *workspaceLifecycleMCP) IsStateful() bool { return m.config.Stateful }

func (*workspaceLifecycleMCP) IsConnected() bool { return false }

func (*workspaceLifecycleMCP) Connect(context.Context) error { return nil }

func (*workspaceLifecycleMCP) Close() error { return nil }

func (*workspaceLifecycleMCP) ListTools(context.Context) ([]asworkspace.Tool, error) {
	return []asworkspace.Tool{}, nil
}

func (m *workspaceLifecycleMCP) MCPClientConfig() (asworkspace.MCPClientConfig, error) {
	return m.config, nil
}

var _ asworkspace.MCPClient = (*workspaceLifecycleMCP)(nil)
var _ asworkspace.MCPConfigProvider = (*workspaceLifecycleMCP)(nil)
