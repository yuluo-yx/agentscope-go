package testcases

import (
	"context"
	"encoding/base64"
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
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
	wslocal "github.com/yuluo-yx/agentscope-go/pkg/workspace/local"
)

func init() {
	pkgtestcases.Register("agent-tool-result-metadata", pkgtestcases.TestCase{
		Description: "Agent preserves Write/Edit tool result metadata and diffs through events and model requests",
		Tags:        []string{"local", "agent", "tool", "metadata"},
		Fn:          testAgentToolResultMetadata,
	})
	pkgtestcases.Register("tool-response-data-chunk-contract", pkgtestcases.TestCase{
		Description: "ToolResponse merges same-ID base64 data chunks by decoded bytes and keeps metadata",
		Tags:        []string{"local", "tool", "message", "data"},
		Fn:          testToolResponseDataChunkContract,
	})
	pkgtestcases.Register("message-tool-result-contracts", pkgtestcases.TestCase{
		Description: "Tool result event metadata and ToolResponse data chunk contracts remain stable",
		Tags:        []string{"local", "message", "tool", "contract"},
		Fn: func(ctx context.Context, _ pkgtestcases.TestCaseOptions) error {
			return runRepoGoTest(ctx,
				"github.com/yuluo-yx/agentscope-go/pkg/message",
				"github.com/yuluo-yx/agentscope-go/pkg/tool",
				"-run",
				"Test(ApplyEventPreservesToolResultEndMetadata|ToolResponseAppendChunkMergesBase64DataByDecodedBytes)$",
				"-count=1",
			)
		},
	})
	pkgtestcases.Register("provider-formatting-contracts", pkgtestcases.TestCase{
		Description: "Gemini schema/function-call formatting and OpenAI embedding request contracts remain compatible",
		Tags:        []string{"local", "model", "embedding", "contract"},
		Fn: func(ctx context.Context, _ pkgtestcases.TestCaseOptions) error {
			return runRepoGoTest(ctx,
				"github.com/yuluo-yx/agentscope-go/pkg/model/gemini",
				"github.com/yuluo-yx/agentscope-go/pkg/embedding/openai",
				"-run",
				"Test(GeminiFormattingHelpersCoverContentDataAndTools|GeminiResponseStreamAndErrorHelpers|ChatModelCallUsesGenAIFormatsRequestAndParsesResponse|TextModelCanOmitDimensionsFromRequest|TextModelFormatsRequestParsesResponseAndUsesCache)$",
				"-count=1",
			)
		},
	})
}

func testAgentToolResultMetadata(ctx context.Context, opts pkgtestcases.TestCaseOptions) error {
	workdir, err := os.MkdirTemp(opts.WorkDir, "metadata-workspace-")
	if err != nil {
		return err
	}
	ws, err := wslocal.NewWorkspace(workdir, wslocal.WithWorkspaceID("tool-result-metadata-e2e"))
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
	kit, err := astool.NewToolkit(tools...)
	if err != nil {
		return err
	}

	notePath := filepath.Join(workdir, "notes.md")
	model := &scriptedChatModel{name: "scripted-tool-result-metadata-e2e", responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("write-call", "Write", mustJSONInput(map[string]any{
			"file_path": notePath,
			"content":   "alpha\nneedle line\n",
		}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("read-call", "Read", mustJSONInput(map[string]any{
			"file_path": notePath,
			"limit":     10,
		}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock("edit-call", "Edit", mustJSONInput(map[string]any{
			"file_path":  notePath,
			"old_string": "needle",
			"new_string": "verified",
		}))}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("metadata diff verified")}, true),
	}}
	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeAcceptEdits)
	state.PermissionContext.WorkingDirectories["workspace"] = permission.AdditionalWorkingDirectory{Path: workdir, Source: "e2e"}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Preserve tool result metadata.",
		model,
		agentpkg.WithToolkit(kit),
		agentpkg.WithAgentState(state),
		agentpkg.WithReActConfig(agentpkg.ReActConfig{MaxIters: 8}),
	)
	if err != nil {
		return err
	}

	ends := map[string]*message.ToolResultEndEvent{}
	userMsg, err := message.NewUserMessage("Tony", "Create and edit a note while preserving metadata")
	if err != nil {
		return err
	}
	if err := agent.ReplyStream(ctx, userMsg, func(evt message.Event) error {
		if end, ok := evt.(*message.ToolResultEndEvent); ok {
			ends[end.ToolCallID] = end
		}
		return nil
	}); err != nil {
		return err
	}
	stateContext := agent.AgentState().Context
	if len(stateContext) == 0 {
		return fmt.Errorf("agent context is empty after reply")
	}
	final := stateContext[len(stateContext)-1]
	if text := final.GetTextContent(""); text == nil || *text != "metadata diff verified" {
		return fmt.Errorf("final reply text mismatch: %#v", final)
	}
	data, err := os.ReadFile(notePath)
	if err != nil {
		return err
	}
	if string(data) != "alpha\nverified line\n" {
		return fmt.Errorf("edited file content mismatch: %q", string(data))
	}

	writeEvent := ends["write-call"]
	if writeEvent == nil {
		return fmt.Errorf("missing Write ToolResultEndEvent in %#v", ends)
	}
	if err := assertToolMetadata(writeEvent.Metadata, notePath, []string{"+alpha", "+needle line"}, nil); err != nil {
		return fmt.Errorf("write event metadata: %w", err)
	}
	if len(model.requests) < 4 {
		return fmt.Errorf("expected at least 4 model requests, got %d", len(model.requests))
	}
	writeResult, err := toolResultByIDFromRequest(model.requests[1], "write-call")
	if err != nil {
		return err
	}
	if err := assertToolMetadata(writeResult.Metadata, notePath, []string{"+alpha", "+needle line"}, nil); err != nil {
		return fmt.Errorf("write request metadata: %w", err)
	}

	editEvent := ends["edit-call"]
	if editEvent == nil {
		return fmt.Errorf("missing Edit ToolResultEndEvent in %#v", ends)
	}
	if err := assertToolMetadata(editEvent.Metadata, notePath, []string{"-needle line", "+verified line"}, map[string]any{"occurrences": 1}); err != nil {
		return fmt.Errorf("edit event metadata: %w", err)
	}
	editResult, err := toolResultByIDFromRequest(model.requests[3], "edit-call")
	if err != nil {
		return err
	}
	if err := assertToolMetadata(editResult.Metadata, notePath, []string{"-needle line", "+verified line"}, map[string]any{"occurrences": 1}); err != nil {
		return fmt.Errorf("edit request metadata: %w", err)
	}
	if opts.SetDetails != nil {
		opts.SetDetails(map[string]any{"model_calls": len(model.requests), "metadata_events": len(ends), "file_path": notePath})
	}
	return nil
}

func testToolResponseDataChunkContract(context.Context, pkgtestcases.TestCaseOptions) error {
	response := astool.NewToolResponse()
	for _, part := range []struct {
		text     string
		metadata map[string]any
	}{
		{text: "hello ", metadata: map[string]any{"chunk": "first"}},
		{text: "world", metadata: map[string]any{"chunk": "second", "final": true}},
	} {
		chunk := astool.NewToolChunk(
			message.ContentBlockList{message.NewDataBlock(
				message.NewBase64Source(base64.StdEncoding.EncodeToString([]byte(part.text)), "text/plain"),
				message.WithDataBlockID("payload"),
				message.WithDataBlockName("payload.txt"),
			)},
			astool.WithToolChunkState(message.ToolResultSuccess),
			astool.WithToolChunkMetadata(part.metadata),
		)
		if err := response.AppendChunk(chunk); err != nil {
			return err
		}
	}
	if response.State != message.ToolResultSuccess {
		return fmt.Errorf("response state mismatch: %s", response.State)
	}
	if response.Metadata["chunk"] != "second" || response.Metadata["final"] != true {
		return fmt.Errorf("response metadata mismatch: %#v", response.Metadata)
	}
	if len(response.Content) != 1 {
		return fmt.Errorf("expected one merged data block, got %#v", response.Content)
	}
	block, ok := response.Content[0].(*message.DataBlock)
	if !ok {
		return fmt.Errorf("merged block type mismatch: %T", response.Content[0])
	}
	source, ok := block.Source.(*message.Base64Source)
	if !ok {
		return fmt.Errorf("merged source type mismatch: %T", block.Source)
	}
	decoded, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		return err
	}
	if string(decoded) != "hello world" {
		return fmt.Errorf("merged base64 payload mismatch: %q", string(decoded))
	}
	return nil
}

func toolResultByIDFromRequest(request modelpkg.CallRequest, id string) (*message.ToolResultBlock, error) {
	for msgIndex := len(request.Messages) - 1; msgIndex >= 0; msgIndex-- {
		blocks := request.Messages[msgIndex].GetContentBlocks("tool_result")
		for blockIndex := len(blocks) - 1; blockIndex >= 0; blockIndex-- {
			result, ok := blocks[blockIndex].(*message.ToolResultBlock)
			if !ok {
				return nil, fmt.Errorf("tool_result block has unexpected type %T", blocks[blockIndex])
			}
			if result.ID == id {
				return result, nil
			}
		}
	}
	return nil, fmt.Errorf("request has no tool result %q", id)
}

func assertToolMetadata(metadata map[string]any, filePath string, diffParts []string, exact map[string]any) error {
	if metadata["file_path"] != filePath {
		return fmt.Errorf("file_path mismatch: %#v", metadata)
	}
	diff, ok := metadata["diff"].(string)
	if !ok || diff == "" {
		return fmt.Errorf("missing diff metadata: %#v", metadata)
	}
	for _, part := range diffParts {
		if !strings.Contains(diff, part) {
			return fmt.Errorf("diff missing %q: %s", part, diff)
		}
	}
	for key, want := range exact {
		if metadata[key] != want {
			return fmt.Errorf("%s mismatch: got %#v want %#v in %#v", key, metadata[key], want, metadata)
		}
	}
	return nil
}
