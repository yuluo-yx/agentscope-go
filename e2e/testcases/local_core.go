package testcases

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/middleware"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
	asmcp "github.com/yuluo-yx/agentscope-go/tool/mcp"
	tasktool "github.com/yuluo-yx/agentscope-go/tool/task"
	wslocal "github.com/yuluo-yx/agentscope-go/workspace/local"
)

func init() {
	testcases.Register("chatmodel-contract", testcases.TestCase{
		Description: "ChatModel Call/Stream contract preserves tools, metadata, usage, and clone boundaries",
		Tags:        []string{"local", "model", "contract"},
		Fn:          testChatModelContract,
	})
	testcases.Register("chatmodel-stream-error", testcases.TestCase{
		Description: "ChatModel stream terminal errors remain observable",
		Tags:        []string{"local", "model", "error"},
		Fn:          testChatModelStreamError,
	})
	testcases.Register("agent-tool-loop", testcases.TestCase{
		Description: "Agent ReAct loop calls a local function tool and feeds the result back to the model",
		Tags:        []string{"local", "agent", "tool"},
		Fn:          testAgentToolLoop,
	})
	testcases.Register("permission-confirm-resume", testcases.TestCase{
		Description: "Agent pauses for tool permission confirmation and resumes with the approved result",
		Tags:        []string{"local", "agent", "permission"},
		Fn:          testPermissionConfirmResume,
	})
	testcases.Register("permission-deny-tool-result", testcases.TestCase{
		Description: "Denied tool permission emits a denied tool result without executing the local handler",
		Tags:        []string{"local", "agent", "permission", "tool"},
		Fn:          testPermissionDenyToolResult,
	})
	testcases.Register("permission-updated-input", testcases.TestCase{
		Description: "Permission decisions can rewrite tool input before Agent executes the local tool",
		Tags:        []string{"local", "agent", "permission", "tool"},
		Fn:          testPermissionUpdatedInput,
	})
	testcases.Register("external-tool-resume", testcases.TestCase{
		Description: "External tools emit execution requirements and resume after external results are observed",
		Tags:        []string{"local", "agent", "tool", "external"},
		Fn:          testExternalToolResume,
	})
	testcases.Register("workspace-local-files", testcases.TestCase{
		Description: "Local workspace file tools run through an Agent loop",
		Tags:        []string{"local", "workspace", "tool"},
		Fn:          testWorkspaceLocalFiles,
	})
	testcases.Register("mcp-inprocess-agent", testcases.TestCase{
		Description: "In-process MCP tools are exposed to Agent and return tool results",
		Tags:        []string{"local", "mcp", "agent"},
		Fn:          testMCPInProcessAgent,
	})
	testcases.Register("workspace-offload", testcases.TestCase{
		Description: "Workspace offloads context, tool result, and base64 data blocks to files",
		Tags:        []string{"local", "workspace", "offload"},
		Fn:          testWorkspaceOffload,
	})
	testcases.Register("middleware-react-tracing", testcases.TestCase{
		Description: "Middleware-provided tools, model metadata, tracing, and workspace tools compose in one ReAct loop",
		Tags:        []string{"local", "middleware", "agent"},
		Fn:          testMiddlewareReactTracing,
	})
	testcases.Register("task-tools-state", testcases.TestCase{
		Description: "Task tools persist structured state through an Agent tool loop",
		Tags:        []string{"local", "task", "state"},
		Fn:          testTaskToolsState,
	})
	testcases.Register("gateway-http-contract", testcases.TestCase{
		Description: "Workspace gateway HTTP contract covers health, tools, MCP registration, calls, and close",
		Tags:        []string{"local", "gateway", "http"},
		Fn: func(ctx context.Context, _ testcases.TestCaseOptions) error {
			return runRepoGoTest(ctx, "github.com/yuluo-yx/agentscope-go/workspace/gateway", "-run", "TestGatewayServerServesToolsMCPRegistrationToolCallsAndClose", "-count=1")
		},
	})
	testcases.Register("message-event-apply", testcases.TestCase{
		Description: "Message event application reconstructs streaming text, data, thinking, hints, tools, and usage",
		Tags:        []string{"local", "message", "events"},
		Fn: func(ctx context.Context, _ testcases.TestCaseOptions) error {
			return runRepoGoTest(ctx, "github.com/yuluo-yx/agentscope-go/message", "-run", "TestApplyEventAccumulatesStreamingMessage", "-count=1")
		},
	})
	testcases.Register("context-compression", testcases.TestCase{
		Description: "Context compression preserves pending tool calls, tasks, summaries, and reserved read cache",
		Tags:        []string{"local", "agent", "context"},
		Fn: func(ctx context.Context, _ testcases.TestCaseOptions) error {
			return runRepoGoTest(ctx, "github.com/yuluo-yx/agentscope-go/agent", "-run", "TestCompressContextPreservesPendingToolCallsTasksAndCleansReadCache", "-count=1")
		},
	})
}

func testChatModelContract(ctx context.Context, opts testcases.TestCaseOptions) error {
	echoTool, err := tool.NewFunctionTool(
		"Echo",
		"Echo one value.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required":   []string{"value"},
		},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("echo")}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(echoTool)
	if err != nil {
		return err
	}
	schemas, err := kit.ToolSchemas()
	if err != nil {
		return err
	}
	systemMsg, err := message.NewSystemMessage("system", "Reply through the direct ChatModel API.")
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", message.ContentBlockList{
		message.NewTextBlock("call and stream"),
		message.NewDataBlock(message.NewURLSource("file:///tmp/input.txt", "text/plain")),
	})
	if err != nil {
		return err
	}
	request := modelpkg.CallRequest{
		Messages: []*message.Message{systemMsg, userMsg},
		Tools:    schemas,
		Metadata: map[string]any{"trace": "chatmodel-e2e"},
		Parameters: map[string]any{
			"temperature": 0,
		},
	}
	model := &scriptedChatModel{name: "scripted-chatmodel-contract", responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("call ok")}, true,
			modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{InputTokens: 11, OutputTokens: 2, Time: time.Millisecond}),
			modelpkg.WithChatResponseMetadata(map[string]any{"mode": "call"})),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("stream ok", message.WithBlockID("stream-text"))}, true,
			modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{InputTokens: 13, OutputTokens: 3, Time: 2 * time.Millisecond}),
			modelpkg.WithChatResponseMetadata(map[string]any{"mode": "stream"})),
	}}

	callResponse, err := model.Call(ctx, request)
	if err != nil {
		return err
	}
	if err := assertText(callResponse.Content, "call ok"); err != nil {
		return err
	}
	if callResponse.Usage == nil || callResponse.Usage.InputTokens != 11 || callResponse.Usage.OutputTokens != 2 {
		return fmt.Errorf("call usage mismatch: %#v", callResponse.Usage)
	}
	count, err := model.CountTokens(request)
	if err != nil {
		return err
	}
	if count <= 0 {
		return fmt.Errorf("expected positive token count, got %d", count)
	}

	stream, err := model.Stream(ctx, request)
	if err != nil {
		return err
	}
	chunks := collectChatStream(stream)
	if len(chunks) != 2 || chunks[0].IsLast || !chunks[1].IsLast {
		return fmt.Errorf("stream should emit one delta and one final response, got %#v", chunks)
	}
	if err := assertText(chunks[1].Content, "stream ok"); err != nil {
		return err
	}
	if len(model.requests) != 2 || !requestIncludesTool(model.requests[0], "Echo") || !requestIncludesTool(model.requests[1], "Echo") {
		return fmt.Errorf("direct requests should preserve tool schemas, got %#v", model.requests)
	}
	model.requests[0].Messages[0].Content[0].(*message.TextBlock).Text = "mutated"
	if text := systemMsg.GetTextContent(""); text == nil || *text != "Reply through the direct ChatModel API." {
		return fmt.Errorf("recorded request should be cloned away from caller messages")
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"token_count": count, "stream_chunks": len(chunks)})
	}
	return nil
}

func testChatModelStreamError(ctx context.Context, _ testcases.TestCaseOptions) error {
	expectedErr := errors.New("provider stream failed")
	model := asyncErrorChatModel{err: expectedErr}
	userMsg, err := message.NewUserMessage("Tony", "stream error")
	if err != nil {
		return err
	}
	stream, err := model.Stream(ctx, modelpkg.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil {
		return err
	}
	chunks := collectChatStream(stream)
	if len(chunks) != 1 || chunks[0].Error == nil || !strings.Contains(chunks[0].Error.Error(), expectedErr.Error()) || !chunks[0].IsLast {
		return fmt.Errorf("terminal stream error not preserved: %#v", chunks)
	}
	count, err := model.CountTokens(modelpkg.CallRequest{Messages: []*message.Message{userMsg}})
	if err != nil || count == 0 {
		return fmt.Errorf("CountTokens should still work for errored stream model, got count=%d err=%v", count, err)
	}
	return nil
}

func testAgentToolLoop(ctx context.Context, opts testcases.TestCaseOptions) error {
	greet, err := tool.NewFunctionTool(
		"Greet",
		"Return a greeting for one name.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
			"required":   []string{"name"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			name, _ := input["name"].(string)
			return message.ContentBlockList{message.NewTextBlock("hello " + name)}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(greet)
	if err != nil {
		return err
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("greet-call", "Greet", mustJSONInput(map[string]any{"name": "Ada"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("hello Ada acknowledged", message.WithBlockID("final-text"))}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeExplore)
	agent, err := agentpkg.NewAgent("Friday", "Use tools when useful.", model, agentpkg.WithToolkit(kit), agentpkg.WithAgentState(state))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Greet Ada")
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
	final := agent.AgentState().Context[len(agent.AgentState().Context)-1]
	if text := final.GetTextContent(""); text == nil || *text != "hello Ada acknowledged" {
		return fmt.Errorf("final assistant text mismatch: %#v", final)
	}
	result, err := onlyToolResultFromLastRequest(model)
	if err != nil {
		return err
	}
	if text := result.Output.Blocks.GetTextContent(""); result.State != message.ToolResultSuccess || text == nil || *text != "hello Ada" {
		return fmt.Errorf("unexpected tool result passed back to model: %#v text=%#v", result, text)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"events": len(events), "model_calls": len(model.requests)})
	}
	return nil
}

func testPermissionConfirmResume(ctx context.Context, _ testcases.TestCaseOptions) error {
	executed := false
	publish, err := tool.NewFunctionTool(
		"Publish",
		"Publish one topic after user confirmation.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"topic": map[string]any{"type": "string"}},
			"required":   []string{"topic"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			executed = true
			topic, _ := input["topic"].(string)
			return message.ContentBlockList{message.NewTextBlock("published " + topic)}, nil
		},
	)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(publish)
	if err != nil {
		return err
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("publish-call", "Publish", mustJSONInput(map[string]any{"topic": "release"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("release published")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Ask before publishing.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Publish the release")
	if err != nil {
		return err
	}
	var confirm *message.RequireUserConfirmEvent
	if err := agent.ReplyStream(ctx, userMsg, func(evt message.Event) error {
		if e, ok := evt.(*message.RequireUserConfirmEvent); ok {
			confirm = e
		}
		return nil
	}); err != nil {
		return err
	}
	if executed {
		return fmt.Errorf("tool executed before confirmation")
	}
	if confirm == nil || len(confirm.ToolCalls) != 1 {
		return fmt.Errorf("expected one confirmation event, got %#v", confirm)
	}
	reply, err := agent.Reply(ctx, message.NewUserConfirmResultEvent(confirm.ReplyID(), []message.ConfirmResult{{
		Confirmed: true,
		ToolCall:  confirm.ToolCalls[0],
		Rules:     confirm.ToolCalls[0].SuggestedRules,
	}}))
	if err != nil {
		return err
	}
	if !executed {
		return fmt.Errorf("confirmed tool call did not execute")
	}
	if text := reply.GetTextContent(""); text == nil || *text != "release published" {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}
	return nil
}

func testPermissionDenyToolResult(ctx context.Context, opts testcases.TestCaseOptions) error {
	executed := false
	deleteRecord, err := tool.NewFunctionTool(
		"DeleteRecord",
		"Delete one record when policy allows it.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
			"required":   []string{"id"},
		},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			executed = true
			return message.ContentBlockList{message.NewTextBlock("deleted")}, nil
		},
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{Behavior: permission.BehaviorDeny, Message: "blocked by e2e policy"}, nil
		}),
	)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(deleteRecord)
	if err != nil {
		return err
	}
	model := &scriptedChatModel{name: "scripted-permission-deny-e2e", responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("delete-call", "DeleteRecord", mustJSONInput(map[string]any{"id": "release"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("delete was blocked")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Respect permission denials.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Delete release")
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
	if executed {
		return fmt.Errorf("DeleteRecord handler executed despite denied permission")
	}
	final := agent.AgentState().Context[len(agent.AgentState().Context)-1]
	if text := final.GetTextContent(""); text == nil || *text != "delete was blocked" {
		return fmt.Errorf("final reply text mismatch: %#v", final)
	}
	if err := assertEventOrder(events, message.ToolCallStartType, message.ToolResultEndType, message.TextBlockDeltaType, message.ReplyEndType); err != nil {
		return err
	}
	result, err := onlyToolResultFromLastRequest(model)
	if err != nil {
		return err
	}
	if text := result.Output.Blocks.GetTextContent(""); result.State != message.ToolResultDenied || text == nil || !strings.Contains(*text, "blocked by e2e policy") {
		return fmt.Errorf("denied tool result mismatch: %#v text=%#v", result, text)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"model_calls": len(model.requests), "denied_tool": result.Name})
	}
	return nil
}

func testPermissionUpdatedInput(ctx context.Context, opts testcases.TestCaseOptions) error {
	var handlerInput map[string]any
	normalizeDeploy, err := tool.NewFunctionTool(
		"NormalizeDeploy",
		"Normalize a deploy request before it is executed.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"service": map[string]any{"type": "string"},
				"env":     map[string]any{"type": "string"},
			},
			"required": []string{"service", "env"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			handlerInput = input
			return message.ContentBlockList{message.NewTextBlock(fmt.Sprintf("deploy %s to %s", input["service"], input["env"]))}, nil
		},
		tool.WithFunctionPermissionFunc(func(_ context.Context, input map[string]any, _ *permission.Context) (*permission.Decision, error) {
			updated := map[string]any{
				"service": strings.TrimSpace(fmt.Sprint(input["service"])),
				"env":     "staging",
			}
			return &permission.Decision{Behavior: permission.BehaviorAllow, Message: "normalized by e2e policy", UpdatedInput: updated}, nil
		}),
	)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(normalizeDeploy)
	if err != nil {
		return err
	}
	model := &scriptedChatModel{name: "scripted-permission-updated-input-e2e", responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("normalize-call", "NormalizeDeploy", mustJSONInput(map[string]any{"service": " checkout ", "env": "prod"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("normalized deploy recorded")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Normalize deploy input before executing tools.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Deploy checkout")
	if err != nil {
		return err
	}
	reply, err := agent.Reply(ctx, userMsg)
	if err != nil {
		return err
	}
	if text := reply.GetTextContent(""); text == nil || *text != "normalized deploy recorded" {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}
	if handlerInput["service"] != "checkout" || handlerInput["env"] != "staging" {
		return fmt.Errorf("permission-updated input was not passed to handler: %#v", handlerInput)
	}
	result, err := onlyToolResultFromLastRequest(model)
	if err != nil {
		return err
	}
	if text := result.Output.Blocks.GetTextContent(""); result.State != message.ToolResultSuccess || text == nil || *text != "deploy checkout to staging" {
		return fmt.Errorf("normalized tool result mismatch: %#v text=%#v", result, text)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"model_calls": len(model.requests), "normalized_env": handlerInput["env"]})
	}
	return nil
}

func testExternalToolResume(ctx context.Context, opts testcases.TestCaseOptions) error {
	executedLocally := false
	deployJob, err := tool.NewFunctionTool(
		"DeployJob",
		"Submit a deployment job to an external executor.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"service": map[string]any{"type": "string"}},
			"required":   []string{"service"},
		},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {
			executedLocally = true
			return message.ContentBlockList{message.NewTextBlock("should run externally")}, nil
		},
		tool.WithFunctionExternalTool(true),
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{Behavior: permission.BehaviorAllow, Message: "external execution allowed"}, nil
		}),
	)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(deployJob)
	if err != nil {
		return err
	}
	model := &scriptedChatModel{name: "scripted-external-tool-e2e", responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("deploy-call", "DeployJob", mustJSONInput(map[string]any{"service": "checkout"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("deployment recorded")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Submit external jobs when needed.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Deploy checkout")
	if err != nil {
		return err
	}
	var required *message.RequireExternalExecutionEvent
	var events []message.Event
	if err := agent.ReplyStream(ctx, userMsg, func(evt message.Event) error {
		events = append(events, evt)
		if typed, ok := evt.(*message.RequireExternalExecutionEvent); ok {
			required = typed
		}
		return nil
	}); err != nil {
		return err
	}
	if executedLocally {
		return fmt.Errorf("external DeployJob handler executed locally")
	}
	if required == nil || len(required.ToolCalls) != 1 || required.ToolCalls[0].Name != "DeployJob" {
		return fmt.Errorf("expected one external execution request, got %#v", required)
	}
	if err := assertEventOrder(events, message.ToolCallStartType, message.ToolResultStartType, message.RequireExternalExecutionType, message.ReplyEndType); err != nil {
		return err
	}
	result := message.NewToolResultBlock(
		required.ToolCalls[0].ID,
		required.ToolCalls[0].Name,
		message.ToolResultOutput{Blocks: message.ContentBlockList{message.NewTextBlock("external executor finished checkout")}},
		message.ToolResultSuccess,
	)
	reply, err := agent.Reply(ctx, message.NewExternalExecutionResultEvent(required.ReplyID(), []*message.ToolResultBlock{result}))
	if err != nil {
		return err
	}
	if text := reply.GetTextContent(""); text == nil || *text != "deployment recorded" {
		return fmt.Errorf("final reply text mismatch after external result: %#v", reply)
	}
	resumedResult, err := onlyToolResultFromLastRequest(model)
	if err != nil {
		return err
	}
	if text := resumedResult.Output.Blocks.GetTextContent(""); resumedResult.State != message.ToolResultSuccess || text == nil || *text != "external executor finished checkout" {
		return fmt.Errorf("external tool result should be sent to resumed model call, got %#v text=%#v", resumedResult, text)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"model_calls": len(model.requests), "external_tool": required.ToolCalls[0].Name})
	}
	return nil
}

func testWorkspaceLocalFiles(ctx context.Context, opts testcases.TestCaseOptions) error {
	workdir := filepath.Join(opts.WorkDir, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return err
	}
	ws, err := wslocal.NewWorkspace(workdir, wslocal.WithWorkspaceID("framework-workspace-e2e"))
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
	notePath := filepath.Join(workdir, "notes.txt")
	noteText := "workspace note\ncreated by e2e"
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("write-call", "Write", mustJSONInput(map[string]any{"file_path": notePath, "content": noteText}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("read-call", "Read", mustJSONInput(map[string]any{"file_path": notePath, "limit": 5}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("workspace note verified")}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	state.PermissionContext.WorkingDirectories["workspace"] = permission.AdditionalWorkingDirectory{Path: workdir, Source: "e2e"}
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
	if text := reply.GetTextContent(""); text == nil || *text != "workspace note verified" {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}
	data, err := os.ReadFile(notePath)
	if err != nil {
		return err
	}
	if string(data) != noteText {
		return fmt.Errorf("workspace file content mismatch: %q", string(data))
	}
	result, err := lastToolResultFromLastRequest(model)
	if err != nil {
		return err
	}
	if text := result.Output.Blocks.GetTextContent(""); result.Name != "Read" || text == nil || !strings.Contains(*text, "workspace note") {
		return fmt.Errorf("read tool result should be passed back to the final model call, got %#v text=%#v", result, text)
	}
	return nil
}

func testMCPInProcessAgent(ctx context.Context, _ testcases.TestCaseOptions) error {
	client, err := asmcp.NewInProcessClient("people", newPeopleMCPServer())
	if err != nil {
		return err
	}
	if err := client.Connect(ctx); err != nil {
		return err
	}
	defer client.Close()
	tools, err := client.ListTools(ctx)
	if err != nil {
		return err
	}
	kit, err := tool.NewToolkit(tools...)
	if err != nil {
		return err
	}
	lookup, err := findToolByName(tools, "mcp__people__lookup_profile")
	if err != nil {
		return err
	}
	if !lookup.IsMCP() || lookup.MCPName() != "people" || !lookup.IsReadOnly() {
		return fmt.Errorf("unexpected MCP metadata: is_mcp=%t mcp=%q read_only=%t", lookup.IsMCP(), lookup.MCPName(), lookup.IsReadOnly())
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("mcp-call", lookup.Name(), mustJSONInput(map[string]any{"name": "Ada"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("profile loaded")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Use MCP tools.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Load Ada profile")
	if err != nil {
		return err
	}
	reply, err := agent.Reply(ctx, userMsg)
	if err != nil {
		return err
	}
	if text := reply.GetTextContent(""); text == nil || *text != "profile loaded" {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}
	result, err := onlyToolResultFromLastRequest(model)
	if err != nil {
		return err
	}
	if text := result.Output.Blocks.GetTextContent(""); result.State != message.ToolResultSuccess || text == nil || *text != "profile:Ada" {
		return fmt.Errorf("unexpected MCP result: %#v text=%#v", result, text)
	}
	return nil
}

func testWorkspaceOffload(ctx context.Context, opts testcases.TestCaseOptions) error {
	workdir := filepath.Join(opts.WorkDir, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return err
	}
	ws, err := wslocal.NewWorkspace(workdir)
	if err != nil {
		return err
	}
	if err := ws.Initialize(ctx); err != nil {
		return err
	}
	defer ws.Close(context.Background())
	userMsg, err := message.NewUserMessage("Tony", message.ContentBlockList{
		message.NewTextBlock("Inspect this badge."),
		message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "image/png"), message.WithDataBlockName("badge.png")),
	})
	if err != nil {
		return err
	}
	contextPath, err := ws.OffloadContext(ctx, "session-1", []*message.Message{userMsg})
	if err != nil {
		return err
	}
	contextData, err := os.ReadFile(contextPath)
	if err != nil {
		return err
	}
	if strings.Contains(string(contextData), `"type":"base64"`) || !strings.Contains(string(contextData), `"type":"url"`) {
		return fmt.Errorf("offloaded context should replace base64 data with URL source: %s", contextData)
	}
	resultPath, err := ws.OffloadToolResult(ctx, "session-1", message.NewToolResultBlock(
		"badge-call",
		"RenderBadge",
		message.ToolResultOutput{Blocks: message.ContentBlockList{
			message.NewTextBlock("badge ready"),
			message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "image/png"), message.WithDataBlockName("badge.png")),
		}},
		message.ToolResultSuccess,
	))
	if err != nil {
		return err
	}
	resultData, err := os.ReadFile(resultPath)
	if err != nil {
		return err
	}
	if !strings.Contains(string(resultData), "badge ready") || !strings.Contains(string(resultData), "<data url='file://") {
		return fmt.Errorf("offloaded tool result should include text and data URL reference: %s", resultData)
	}
	dataFiles, err := os.ReadDir(filepath.Join(workdir, "data"))
	if err != nil {
		return err
	}
	if len(dataFiles) != 1 {
		return fmt.Errorf("expected one de-duplicated data file, got %d", len(dataFiles))
	}
	offloadedBytes, err := os.ReadFile(filepath.Join(workdir, "data", dataFiles[0].Name()))
	if err != nil {
		return err
	}
	if string(offloadedBytes) != "hello" {
		return fmt.Errorf("offloaded base64 payload mismatch: %q", string(offloadedBytes))
	}
	return nil
}

func testMiddlewareReactTracing(ctx context.Context, opts testcases.TestCaseOptions) error {
	workdir := filepath.Join(opts.WorkDir, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return err
	}
	ws, err := wslocal.NewWorkspace(workdir, wslocal.WithWorkspaceID("middleware-react-e2e"))
	if err != nil {
		return err
	}
	recorder := &recordingTracer{}
	middlewareEcho, err := tool.NewFunctionTool(
		"MiddlewareEcho",
		"Echo a middleware-provided value.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required":   []string{"value"},
		},
		func(_ context.Context, input map[string]any, _ *asstate.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("middleware:" + input["value"].(string))}, nil
		},
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{Behavior: permission.BehaviorAllow, Message: "allowed in e2e"}, nil
		}),
	)
	if err != nil {
		return err
	}
	model := &scriptedChatModel{name: "scripted-framework-e2e", responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("middleware-call", "MiddlewareEcho", mustJSONInput(map[string]any{"value": "ready"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("write-call", "Write", mustJSONInput(map[string]any{"file_path": filepath.Join(workdir, "notes.txt"), "content": "middleware workspace note"}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("read-call", "Read", mustJSONInput(map[string]any{"file_path": filepath.Join(workdir, "notes.txt"), "limit": 5}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("middleware workspace verified")}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	state.PermissionContext.WorkingDirectories["workspace"] = permission.AdditionalWorkingDirectory{Path: workdir, Source: "e2e"}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Use workspace tools.",
		model,
		agentpkg.WithWorkspace(ctx, ws),
		agentpkg.WithAgentState(state),
		agentpkg.WithMiddlewares(
			requestResponseMetadataMiddleware{},
			middleware.NewTracingMiddleware(recorder),
			middlewareToolList{tools: []agentpkg.Tool{middlewareEcho}},
		),
	)
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Create and verify a workspace note")
	if err != nil {
		return err
	}
	reply, err := agent.Reply(ctx, userMsg)
	if err != nil {
		return err
	}
	if text := reply.GetTextContent(""); text == nil || *text != "middleware workspace verified" {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}
	data, err := os.ReadFile(filepath.Join(workdir, "notes.txt"))
	if err != nil {
		return err
	}
	if string(data) != "middleware workspace note" {
		return fmt.Errorf("workspace file content mismatch: %q", string(data))
	}
	for index, request := range model.requests {
		if value, ok := request.Metadata["middleware_request"].(string); !ok || value != "preserved" {
			return fmt.Errorf("model request %d lost middleware metadata: %#v", index, request.Metadata)
		}
	}
	for _, want := range []string{"invoke_agent Friday", "chat scripted-framework-e2e", "execute_tool MiddlewareEcho", "execute_tool Write", "execute_tool Read"} {
		if !recorder.HasSpan(want) {
			return fmt.Errorf("missing tracing span %q in %v", want, recorder.SpanNames())
		}
	}
	return nil
}

func testTaskToolsState(ctx context.Context, _ testcases.TestCaseOptions) error {
	kit, err := tool.NewToolkit(tasktool.NewTaskCreate())
	if err != nil {
		return err
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("task-call", "TaskCreate", `{"subject":"Track phase five","description":"Create a task from the global E2E smoke test.","metadata":{"phase":"five"}}`)}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("task is tracked")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Track structured work.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		return err
	}
	userMsg, err := message.NewUserMessage("Tony", "Track phase five work")
	if err != nil {
		return err
	}
	reply, err := agent.Reply(ctx, userMsg)
	if err != nil {
		return err
	}
	if text := reply.GetTextContent(""); text == nil || *text != "task is tracked" {
		return fmt.Errorf("final reply text mismatch: %#v", reply)
	}
	tasks := agent.AgentState().TaskContext.Tasks
	if len(tasks) != 1 || tasks[0].Subject != "Track phase five" || tasks[0].Metadata["phase"] != "five" {
		return fmt.Errorf("TaskCreate should persist task state, got %#v", tasks)
	}
	result, err := onlyToolResultFromLastRequest(model)
	if err != nil {
		return err
	}
	if text := result.Output.Blocks.GetTextContent(""); result.State != message.ToolResultSuccess || text == nil || !strings.Contains(*text, "created successfully") {
		return fmt.Errorf("tool result should be successful, got %#v", result)
	}
	return nil
}

func collectChatStream(stream <-chan modelpkg.ChatResponse) []modelpkg.ChatResponse {
	var chunks []modelpkg.ChatResponse
	for chunk := range stream {
		chunks = append(chunks, *chunk.Clone())
	}
	return chunks
}

func newPeopleMCPServer() *mcpserver.MCPServer {
	server := mcpserver.NewMCPServer("people-server", "1.0.0", mcpserver.WithToolCapabilities(true))
	server.AddTool(
		gomcp.NewTool(
			"lookup_profile",
			gomcp.WithDescription("Look up one profile by name."),
			gomcp.WithReadOnlyHintAnnotation(true),
			gomcp.WithString("name", gomcp.Required(), gomcp.Description("Name to look up.")),
		),
		func(_ context.Context, request gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			return gomcp.NewToolResultText("profile:" + request.GetString("name", "AgentScope")), nil
		},
	)
	return server
}

type middlewareToolList struct {
	tools []agentpkg.Tool
}

func (m middlewareToolList) MiddlewareName() string { return "middleware-tool-list" }

func (m middlewareToolList) ListTools(context.Context, agentpkg.AgentAccessor) ([]agentpkg.Tool, error) {
	return append([]agentpkg.Tool(nil), m.tools...), nil
}

type requestResponseMetadataMiddleware struct{}

func (requestResponseMetadataMiddleware) MiddlewareName() string { return "request-response-metadata" }

func (requestResponseMetadataMiddleware) OnModelCall(
	ctx context.Context,
	_ agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ModelCallHandler,
) (<-chan modelpkg.ChatResponse, error) {
	request := input["request"].(modelpkg.CallRequest)
	request.Metadata = cloneAnyMap(request.Metadata)
	request.Metadata["middleware_request"] = "preserved"
	input["request"] = request
	responses, err := next(ctx)
	if err != nil {
		return nil, err
	}
	wrapped := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(wrapped)
		for response := range responses {
			clone := response.Clone()
			clone.Metadata = cloneAnyMap(clone.Metadata)
			clone.Metadata["middleware_response"] = "observed"
			wrapped <- *clone
		}
	}()
	return wrapped, nil
}

type recordingTracer struct {
	mu    sync.Mutex
	spans []*recordingSpan
}

func (t *recordingTracer) StartSpan(ctx context.Context, name string, attributes map[string]any) (context.Context, middleware.TraceSpan) {
	span := &recordingSpan{name: name, attributes: cloneAnyMap(attributes)}
	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()
	return ctx, span
}

func (t *recordingTracer) SpanNames() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	names := make([]string, 0, len(t.spans))
	for _, span := range t.spans {
		names = append(names, span.name)
	}
	return names
}

func (t *recordingTracer) HasSpan(name string) bool {
	for _, current := range t.SpanNames() {
		if current == name {
			return true
		}
	}
	return false
}

type recordingSpan struct {
	name       string
	attributes map[string]any
	err        error
	ended      bool
}

func (s *recordingSpan) SetAttributes(attributes map[string]any) {
	for key, value := range attributes {
		s.attributes[key] = value
	}
}

func (s *recordingSpan) RecordError(err error) { s.err = err }

func (s *recordingSpan) End() { s.ended = true }

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}
