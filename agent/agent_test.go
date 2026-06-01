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

package agent_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

type scriptedChatModel struct {
	responses []*modelpkg.ChatResponse
	requests  []modelpkg.CallRequest
}

func (m *scriptedChatModel) Name() string {
	return "scripted"
}

func (m *scriptedChatModel) Call(_ context.Context, request modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func (m *scriptedChatModel) Stream(ctx context.Context, request modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	ch := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(ch)
		delta := response.Clone()
		delta.IsLast = false
		delta.Usage = nil
		select {
		case ch <- *delta:
		case <-ctx.Done():
			return
		}
		select {
		case ch <- *response.Clone():
		case <-ctx.Done():
			return
		}
	}()
	return ch, nil
}

func (m *scriptedChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

type streamOnlyChatModel struct {
	chunks      []*modelpkg.ChatResponse
	callCount   int
	streamCount int
	requests    []modelpkg.CallRequest
}

func (m *streamOnlyChatModel) Name() string {
	return "stream-only"
}

func (m *streamOnlyChatModel) Call(context.Context, modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	m.callCount++
	return nil, fmt.Errorf("Call should not be used for streaming agent replies")
}

func (m *streamOnlyChatModel) Stream(ctx context.Context, request modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	m.streamCount++
	m.requests = append(m.requests, request.Clone())
	ch := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(ch)
		for _, chunk := range m.chunks {
			select {
			case ch <- *chunk.Clone():
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (m *streamOnlyChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func TestAgentReplyStreamsModelEventsAndPersistsContext(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("hello Tony")},
			true,
			modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{InputTokens: 3, OutputTokens: 2}),
		),
	}}
	agent, err := agentpkg.NewAgent("Friday", "You are helpful.", model)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Hi")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	var eventTypes []message.EventType
	if err := agent.ReplyStream(context.Background(), userMsg, func(evt message.Event) error {
		eventTypes = append(eventTypes, evt.GetType())
		return nil
	}); err != nil {
		t.Fatalf("ReplyStream returned error: %v", err)
	}

	assertInitialModelRequest(t, model.requests, "You are helpful.", "Hi")
	assertCoreReplyEvents(t, eventTypes)
	assertPersistedAssistantReply(t, agent.AgentState(), "hello Tony", 3, 2)
}

func TestAgentReplyStreamConsumesModelStreamDeltas(t *testing.T) {
	t.Parallel()

	textID := "text-stream"
	model := &streamOnlyChatModel{chunks: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("hel", message.WithBlockID(textID))},
			false,
			modelpkg.WithChatResponseID("stream-response"),
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("lo", message.WithBlockID(textID))},
			false,
			modelpkg.WithChatResponseID("stream-response"),
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("hello", message.WithBlockID(textID))},
			true,
			modelpkg.WithChatResponseID("stream-response"),
			modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{InputTokens: 4, OutputTokens: 2}),
		),
	}}
	agent, err := agentpkg.NewAgent("Friday", "You are helpful.", model)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Hi")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	var textDeltas []string
	var eventTypes []message.EventType
	if err := agent.ReplyStream(context.Background(), userMsg, func(evt message.Event) error {
		eventTypes = append(eventTypes, evt.GetType())
		if delta, ok := evt.(*message.TextBlockDeltaEvent); ok {
			textDeltas = append(textDeltas, delta.Delta)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplyStream returned error: %v", err)
	}

	if model.callCount != 0 {
		t.Fatalf("agent should not call non-streaming Call, got %d calls", model.callCount)
	}
	if model.streamCount != 1 {
		t.Fatalf("agent should call Stream once, got %d", model.streamCount)
	}
	assertStringSlice(t, textDeltas, []string{"hel", "lo"})
	assertInitialModelRequest(t, model.requests, "You are helpful.", "Hi")
	assertCoreReplyEvents(t, eventTypes)
	assertPersistedAssistantReply(t, agent.AgentState(), "hello", 4, 2)
}

func TestAgentReplyStreamReturnsModelStreamError(t *testing.T) {
	t.Parallel()

	model := &streamOnlyChatModel{chunks: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			nil,
			true,
			modelpkg.WithChatResponseError(errors.New("provider stream failed")),
		),
	}}
	agent, err := agentpkg.NewAgent("Friday", "You are helpful.", model)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Hi")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	err = agent.ReplyStream(context.Background(), userMsg, func(message.Event) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "provider stream failed") {
		t.Fatalf("ReplyStream should return terminal model stream error, got %v", err)
	}
}

func assertInitialModelRequest(t *testing.T, requests []modelpkg.CallRequest, systemText, userText string) {
	t.Helper()
	if len(requests) != 1 {
		t.Fatalf("model should be called once, got %d", len(requests))
	}
	if got := requests[0].Messages[0]; got.Role != message.RoleSystem || *got.GetTextContent("") != systemText {
		t.Fatalf("first model message should be system prompt, got %#v", got)
	}
	if got := requests[0].Messages[1]; got.Role != message.RoleUser || *got.GetTextContent("") != userText {
		t.Fatalf("second model message should be user input, got %#v", got)
	}
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("string slice length mismatch: got %#v want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("string slice mismatch: got %#v want %#v", got, want)
		}
	}
}

func assertCoreReplyEvents(t *testing.T, eventTypes []message.EventType) {
	t.Helper()
	for _, want := range []message.EventType{
		message.ReplyStartType,
		message.ModelCallStartType,
		message.TextBlockDeltaType,
		message.ReplyEndType,
	} {
		if !hasEventType(eventTypes, want) {
			t.Fatalf("reply stream missed event %s: %#v", want, eventTypes)
		}
	}
}

func assertPersistedAssistantReply(t *testing.T, state *statepkg.AgentState, text string, inputTokens, outputTokens int) {
	t.Helper()
	if len(state.Context) != 2 {
		t.Fatalf("context should contain user and assistant messages, got %d", len(state.Context))
	}
	finalText := state.Context[1].GetTextContent("")
	if finalText == nil || *finalText != text {
		t.Fatalf("assistant message not persisted: %#v", state.Context[1])
	}
	if state.Context[1].Usage == nil || state.Context[1].Usage.InputTokens != inputTokens || state.Context[1].Usage.OutputTokens != outputTokens {
		t.Fatalf("usage should be accumulated on assistant message, got %#v", state.Context[1].Usage)
	}
}

func TestAgentReplyReturnsModelErrors(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{}
	agent, err := agentpkg.NewAgent("Friday", "You are helpful.", model)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Hi")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	if _, err := agent.Reply(context.Background(), userMsg); err == nil || !strings.Contains(err.Error(), "no response") {
		t.Fatalf("Reply should return model error, got %v", err)
	}
}

func TestAgentMiddlewaresEditSystemPromptAndModelRequest(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("handled")},
			true,
		),
	}}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Base prompt.",
		model,
		agentpkg.WithMiddlewares(requestEditingMiddleware{}),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Hi")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	if _, err := agent.Reply(context.Background(), userMsg); err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	if len(model.requests) != 1 {
		t.Fatalf("model should be called once, got %d", len(model.requests))
	}
	systemText := model.requests[0].Messages[0].GetTextContent("")
	if systemText == nil || !strings.Contains(*systemText, "Middleware note.") {
		t.Fatalf("system prompt middleware was not applied: %#v", systemText)
	}
	if got, ok := model.requests[0].Metadata["from_middleware"].(bool); !ok || !got {
		t.Fatalf("model call middleware did not edit request metadata: %#v", model.requests[0].Metadata)
	}
}

func TestAgentExecutesToolCallsAndContinuesReasoning(t *testing.T) {
	t.Parallel()

	echo, err := tool.NewFunctionTool(
		"Echo",
		"Echo a value.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required":   []string{"value"},
		},
		func(_ context.Context, input map[string]any, _ *statepkg.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("echo:" + input["value"].(string))}, nil
		},
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{Behavior: permission.BehaviorAllow, Message: "allowed in test"}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	kit, err := tool.NewToolkit(echo)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("call-1", "Echo", `{"value":"hi"}`)},
			true,
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("done")},
			true,
		),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Use tools when needed.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Echo hi")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	reply, err := agent.Reply(context.Background(), userMsg)
	if err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	if len(model.requests) != 2 {
		t.Fatalf("model should be called twice, got %d", len(model.requests))
	}
	if text := reply.GetTextContent(""); text == nil || *text != "done" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
	last := model.requests[1].Messages[len(model.requests[1].Messages)-1]
	results := last.GetContentBlocks("tool_result")
	if len(results) != 1 {
		t.Fatalf("second model call should include tool result context, got %#v", last.Content)
	}
	result := results[0].(*message.ToolResultBlock)
	if result.State != message.ToolResultSuccess || len(result.Output.Blocks) != 1 {
		t.Fatalf("tool result should be successful, got %#v", result)
	}
	if got := result.Output.Blocks.GetTextContent(""); got == nil || *got != "echo:hi" {
		t.Fatalf("tool output mismatch: %#v", got)
	}
}

func TestAgentUsesPermissionUpdatedInputForToolExecution(t *testing.T) {
	t.Parallel()

	var executedValue string
	rewriter, err := tool.NewFunctionTool(
		"Rewrite",
		"Rewrite a value before execution.",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
			"required":   []string{"value"},
		},
		func(_ context.Context, input map[string]any, _ *statepkg.AgentState) (message.ContentBlockList, error) {
			executedValue, _ = input["value"].(string)
			return message.ContentBlockList{message.NewTextBlock("used:" + executedValue)}, nil
		},
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{
				Behavior:     permission.BehaviorAllow,
				Message:      "rewritten in test",
				UpdatedInput: map[string]any{"value": "rewritten"},
			}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	kit, err := tool.NewToolkit(rewriter)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("call-rewrite", "Rewrite", `{"value":"original"}`)},
			true,
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("done")},
			true,
		),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Use rewritten input.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Rewrite original")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	if _, err := agent.Reply(context.Background(), userMsg); err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	if executedValue != "rewritten" {
		t.Fatalf("tool should execute with permission updated input, got %q", executedValue)
	}
}

func TestAgentExecutesConcurrencySafeToolCallsInParallel(t *testing.T) {
	t.Parallel()

	started := make(chan string, 2)
	release := make(chan struct{})
	newGatedTool := func(name string) tool.Tool {
		fn, err := tool.NewFunctionTool(
			name,
			"Wait until both tools have started.",
			map[string]any{"type": "object"},
			func(ctx context.Context, _ map[string]any, _ *statepkg.AgentState) (message.ContentBlockList, error) {
				started <- name
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return message.ContentBlockList{message.NewTextBlock(name + ":done")}, nil
			},
			tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
				return &permission.Decision{Behavior: permission.BehaviorAllow, Message: "allowed in test"}, nil
			}),
		)
		if err != nil {
			t.Fatalf("NewFunctionTool returned error: %v", err)
		}
		return fn
	}
	kit, err := tool.NewToolkit(newGatedTool("GateA"), newGatedTool("GateB"))
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{
				message.NewToolCallBlock("call-a", "GateA", `{}`),
				message.NewToolCallBlock("call-b", "GateB", `{}`),
			},
			true,
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("done")},
			true,
		),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Run safe tools together.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Run both")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := agent.Reply(ctx, userMsg)
		result <- err
	}()

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(300 * time.Millisecond):
			close(release)
			t.Fatalf("expected both concurrency-safe tools to start before either one is released, saw %#v", seen)
		}
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}
}

func TestAgentCompressContextTruncatesToolResults(t *testing.T) {
	t.Parallel()

	state := statepkg.NewAgentState()
	assistantMsg, err := message.NewAssistantMessage("Friday", message.ContentBlockList{
		message.NewToolResultBlock(
			"call-long",
			"Read",
			message.ToolResultOutput{
				Raw:    "raw-long-output",
				Blocks: message.ContentBlockList{message.NewTextBlock("text-long-output")},
			},
			message.ToolResultSuccess,
		),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = []*message.Message{assistantMsg}
	model := &scriptedChatModel{}
	config := agentpkg.DefaultContextConfig()
	config.ToolResultLimit = 4
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Compress context.",
		model,
		agentpkg.WithAgentState(state),
		agentpkg.WithContextConfig(config),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}

	if err := agent.CompressContext(context.Background()); err != nil {
		t.Fatalf("CompressContext returned error: %v", err)
	}

	result := state.Context[0].GetContentBlocks("tool_result")[0].(*message.ToolResultBlock)
	if !strings.HasPrefix(result.Output.Raw, "raw-") || !strings.Contains(result.Output.Raw, "truncated") {
		t.Fatalf("raw tool output was not truncated: %q", result.Output.Raw)
	}
	text := result.Output.Blocks.GetTextContent("")
	if text == nil || !strings.HasPrefix(*text, "text") || !strings.Contains(*text, "truncated") {
		t.Fatalf("text tool output was not truncated: %v", text)
	}
}

func TestAgentStopsForToolPermissionConfirmation(t *testing.T) {
	t.Parallel()

	write, err := tool.NewFunctionTool(
		"WriteThing",
		"Write a value.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *statepkg.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("written")}, nil
		},
		tool.WithFunctionSuggestedRule("WriteThing"),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	kit, err := tool.NewToolkit(write)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("call-ask", "WriteThing", `{}`)},
			true,
		),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Ask before writes.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Write")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	var confirm *message.RequireUserConfirmEvent
	var eventTypes []message.EventType
	if err := agent.ReplyStream(context.Background(), userMsg, func(evt message.Event) error {
		eventTypes = append(eventTypes, evt.GetType())
		if e, ok := evt.(*message.RequireUserConfirmEvent); ok {
			confirm = e
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplyStream returned error: %v", err)
	}

	if confirm == nil || len(confirm.ToolCalls) != 1 {
		t.Fatalf("expected confirmation event, got %#v", confirm)
	}
	if confirm.ToolCalls[0].State != message.ToolCallAsking || len(confirm.ToolCalls[0].SuggestedRules) != 1 {
		t.Fatalf("tool call should be marked asking with suggested rule, got %#v", confirm.ToolCalls[0])
	}
	last := agent.AgentState().Context[len(agent.AgentState().Context)-1]
	block := last.FindBlock("tool_call", "call-ask").(*message.ToolCallBlock)
	if block.State != message.ToolCallAsking {
		t.Fatalf("context tool call should remain awaiting confirmation, got %#v", block)
	}
	if !hasEventType(eventTypes, message.ReplyEndType) {
		t.Fatalf("waiting reply should still emit ReplyEnd event, got %#v", eventTypes)
	}
}

func TestAgentResumesConfirmedToolCall(t *testing.T) {
	t.Parallel()

	executed := false
	write, err := tool.NewFunctionTool(
		"WriteThing",
		"Write a value.",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any, *statepkg.AgentState) (message.ContentBlockList, error) {
			executed = true
			return message.ContentBlockList{message.NewTextBlock("written")}, nil
		},
		tool.WithFunctionSuggestedRule("WriteThing"),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	kit, err := tool.NewToolkit(write)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("call-ask", "WriteThing", `{}`)},
			true,
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("done")},
			true,
		),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Ask before writes.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Write")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	var confirm *message.RequireUserConfirmEvent
	if replyErr := agent.ReplyStream(context.Background(), userMsg, func(evt message.Event) error {
		if e, ok := evt.(*message.RequireUserConfirmEvent); ok {
			confirm = e
		}
		return nil
	}); replyErr != nil {
		t.Fatalf("initial ReplyStream returned error: %v", replyErr)
	}
	if confirm == nil || len(confirm.ToolCalls) != 1 {
		t.Fatalf("expected confirmation event, got %#v", confirm)
	}

	confirmEvent := message.NewUserConfirmResultEvent(confirm.ReplyID(), []message.ConfirmResult{{
		Confirmed: true,
		ToolCall:  confirm.ToolCalls[0],
		Rules:     confirm.ToolCalls[0].SuggestedRules,
	}})
	reply, err := agent.Reply(context.Background(), confirmEvent)
	if err != nil {
		t.Fatalf("resume Reply returned error: %v", err)
	}
	if !executed {
		t.Fatal("confirmed tool call should be executed on resume")
	}
	if text := reply.GetTextContent(""); text == nil || *text != "done" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model should be called before and after tool execution, got %d", len(model.requests))
	}
}

func TestAgentReplyHookPropagatesModelError(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{}
	agent, err := agentpkg.NewAgent("Friday", "Use hooks.", model, agentpkg.WithMiddlewares(replyPassthroughMiddleware{}))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Hi")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	if _, err := agent.Reply(context.Background(), userMsg); err == nil || !strings.Contains(err.Error(), "no response") {
		t.Fatalf("Reply should return model error through reply hook, got %v", err)
	}
}

func TestAgentReasoningHookPropagatesModelError(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{}
	agent, err := agentpkg.NewAgent("Friday", "Use hooks.", model, agentpkg.WithMiddlewares(reasoningPassthroughMiddleware{}))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Hi")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	if _, err := agent.Reply(context.Background(), userMsg); err == nil || !strings.Contains(err.Error(), "no response") {
		t.Fatalf("Reply should return model error through reasoning hook, got %v", err)
	}
}

func TestAgentCompressContextKeepsUTF8Valid(t *testing.T) {
	t.Parallel()

	raw := "你好世界"
	msg, err := message.NewAssistantMessage("Friday", message.ContentBlockList{
		message.NewToolResultBlock("call-utf8", "Read", message.ToolResultOutput{
			Raw: raw,
			Blocks: message.ContentBlockList{
				message.NewTextBlock(raw),
			},
		}, message.ToolResultSuccess),
	})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state := statepkg.NewAgentState()
	state.Context = append(state.Context, msg)
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("unused")}, true),
	}}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Compress safely.",
		model,
		agentpkg.WithAgentState(state),
		agentpkg.WithContextConfig(func() agentpkg.ContextConfig {
			config := agentpkg.DefaultContextConfig()
			config.ToolResultLimit = 4
			return config
		}()),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}

	if err := agent.CompressContext(context.Background()); err != nil {
		t.Fatalf("CompressContext returned error: %v", err)
	}

	result := state.Context[0].GetContentBlocks("tool_result")[0].(*message.ToolResultBlock)
	if !utf8.ValidString(result.Output.Raw) {
		t.Fatalf("raw output should remain valid UTF-8, got %q", result.Output.Raw)
	}
	text := result.Output.Blocks.GetTextContent("")
	if text == nil || !utf8.ValidString(*text) {
		t.Fatalf("text output should remain valid UTF-8, got %v", text)
	}
}

type requestEditingMiddleware struct{}

func (requestEditingMiddleware) MiddlewareName() string {
	return "request-editing"
}

func (requestEditingMiddleware) OnSystemPrompt(
	_ context.Context,
	_ agentpkg.AgentAccessor,
	prompt string,
) (string, error) {
	return prompt + "\nMiddleware note.", nil
}

func (requestEditingMiddleware) OnModelCall(
	ctx context.Context,
	_ agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.ModelCallHandler,
) (<-chan modelpkg.ChatResponse, error) {
	request := input["request"].(modelpkg.CallRequest)
	request.Metadata = map[string]any{"from_middleware": true}
	input["request"] = request
	return next(ctx)
}

type replyPassthroughMiddleware struct{}

func (replyPassthroughMiddleware) MiddlewareName() string {
	return "reply-passthrough"
}

func (replyPassthroughMiddleware) OnReply(
	ctx context.Context,
	_ agentpkg.AgentAccessor,
	_ agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	return next(ctx)
}

type reasoningPassthroughMiddleware struct{}

func (reasoningPassthroughMiddleware) MiddlewareName() string {
	return "reasoning-passthrough"
}

func (reasoningPassthroughMiddleware) OnReasoning(
	ctx context.Context,
	_ agentpkg.AgentAccessor,
	_ agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	return next(ctx)
}

func hasEventType(events []message.EventType, want message.EventType) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
