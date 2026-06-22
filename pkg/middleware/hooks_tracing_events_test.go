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

package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

func TestTracingMiddlewareWrapsStreamsAndRecordsTerminalMetadata(t *testing.T) {
	tracer := &recordingTracer{}
	state := statepkg.NewAgentState()
	state.SessionID = "session-1"
	agent := middlewareAgentStub{name: "Friday", state: state}
	mw := NewTracingMiddleware(tracer)

	replyEvents, err := mw.OnReply(context.Background(), agent, agentpkg.HookInput{"input": "hello"}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 2)
		out <- message.NewReplyStartEvent("session-1", "reply-1", "Friday")
		out <- message.NewReplyEndEvent("session-1", "reply-1")
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	if got := collectEvents(replyEvents); len(got) != 2 {
		t.Fatalf("reply events not preserved: %#v", got)
	}
	replySpan := tracer.spans[0]
	if replySpan.name != "invoke_agent Friday" || !replySpan.ended || replySpan.attributes["agentscope.reply.ended"] != true || replySpan.attributes["agentscope.input.type"] != "string" {
		t.Fatalf("reply span mismatch: %#v", replySpan)
	}

	modelErr := errors.New("model stream error")
	modelResponses, err := mw.OnModelCall(
		context.Background(),
		agent,
		agentpkg.HookInput{
			"model":   traceChatModelStub{name: "mock:model"},
			"request": asmodel.CallRequest{Messages: []*message.Message{{Role: message.RoleUser}}, Tools: []asmodel.ToolSchema{{}}},
		},
		func(context.Context) (<-chan asmodel.ChatResponse, error) {
			out := make(chan asmodel.ChatResponse, 1)
			out <- *asmodel.NewChatResponse(
				message.ContentBlockList{message.NewTextBlock("model done")},
				true,
				asmodel.WithChatResponseID("resp-1"),
				asmodel.WithChatResponseUsage(&asmodel.ChatUsage{InputTokens: 3, OutputTokens: 5}),
				asmodel.WithChatResponseError(modelErr),
			)
			close(out)
			return out, nil
		},
	)
	if err != nil {
		t.Fatalf("OnModelCall returned error: %v", err)
	}
	if got := collectResponses(modelResponses); len(got) != 1 {
		t.Fatalf("model responses not preserved: %#v", got)
	}
	modelSpan := tracer.spans[1]
	if !errors.Is(modelSpan.err, modelErr) || modelSpan.attributes["gen_ai.response.id"] != "resp-1" || modelSpan.attributes["gen_ai.usage.output_tokens"] != 5 {
		t.Fatalf("model span mismatch: %#v", modelSpan)
	}

	toolChunks, err := mw.OnActing(
		context.Background(),
		agent,
		agentpkg.HookInput{"tool_call": message.NewToolCallBlock("call-1", "Read", `{"file_path":"/tmp/a"}`)},
		func(context.Context) (<-chan agentpkg.ToolChunk, error) {
			out := make(chan agentpkg.ToolChunk, 1)
			out <- *astool.NewToolChunk(
				message.ContentBlockList{message.NewTextBlock("tool done")},
				astool.WithToolChunkState(message.ToolResultSuccess),
			)
			close(out)
			return out, nil
		},
	)
	if err != nil {
		t.Fatalf("OnActing returned error: %v", err)
	}
	if got := collectMiddlewareChunks(toolChunks); len(got) != 1 {
		t.Fatalf("tool chunks not preserved: %#v", got)
	}
	toolSpan := tracer.spans[2]
	if toolSpan.attributes["gen_ai.tool.call.result"] != "tool done" || toolSpan.attributes["agentscope.tool.state"] != string(message.ToolResultSuccess) {
		t.Fatalf("tool span mismatch: %#v", toolSpan)
	}
}

func TestTracingMiddlewareRecordsErrorsNilStreamsAndPassthrough(t *testing.T) {
	tracer := &recordingTracer{}
	agent := middlewareAgentStub{name: "Friday", state: statepkg.NewAgentState()}
	mw := NewTracingMiddleware(tracer)
	boom := errors.New("boom")

	if _, err := mw.OnReply(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		return nil, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("OnReply error mismatch: %v", err)
	}
	if _, err := mw.OnModelCall(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan asmodel.ChatResponse, error) {
		return nil, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("OnModelCall error mismatch: %v", err)
	}
	if _, err := mw.OnActing(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan agentpkg.ToolChunk, error) {
		return nil, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("OnActing error mismatch: %v", err)
	}
	if err := mw.OnCompressContext(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) error {
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("OnCompressContext error mismatch: %v", err)
	}
	for i, span := range tracer.spans[:4] {
		if !errors.Is(span.err, boom) || !span.ended {
			t.Fatalf("error span %d mismatch: %#v", i, span)
		}
	}

	nilSpan := &recordingTraceSpan{attributes: map[string]any{}}
	if wrapEvents(nil, nilSpan) != nil || nilSpan.err == nil || !nilSpan.ended {
		t.Fatalf("nil event stream should record and end span: %#v", nilSpan)
	}
	nilSpan = &recordingTraceSpan{attributes: map[string]any{}}
	if wrapResponses(nil, nilSpan) != nil || nilSpan.err == nil || !nilSpan.ended {
		t.Fatalf("nil model stream should record and end span: %#v", nilSpan)
	}
	nilSpan = &recordingTraceSpan{attributes: map[string]any{}}
	if wrapToolChunks(nil, nilSpan) != nil || nilSpan.err == nil || !nilSpan.ended {
		t.Fatalf("nil tool stream should record and end span: %#v", nilSpan)
	}
	recordAndEnd(nil, boom)
	if sessionID(nil) != "" || sessionID(middlewareAgentStub{name: "nil-state"}) != "" {
		t.Fatal("sessionID should be empty for nil agent/state")
	}
	if inputType(nil) != "nil" || inputType(42) != "int" {
		t.Fatal("inputType should expose nil and concrete Go types")
	}

	var nilTracing *TracingMiddleware
	events, err := nilTracing.OnReply(context.Background(), agent, agentpkg.HookInput{}, emptyEventStream)
	if err != nil || events == nil {
		t.Fatalf("nil tracing OnReply should delegate, events=%v err=%v", events, err)
	}
	responses, err := nilTracing.OnModelCall(context.Background(), agent, agentpkg.HookInput{}, emptyModelStream)
	if err != nil || responses == nil {
		t.Fatalf("nil tracing OnModelCall should delegate, responses=%v err=%v", responses, err)
	}
	chunks, err := nilTracing.OnActing(context.Background(), agent, agentpkg.HookInput{}, emptyToolStream)
	if err != nil || chunks == nil {
		t.Fatalf("nil tracing OnActing should delegate, chunks=%v err=%v", chunks, err)
	}
	if err := nilTracing.OnCompressContext(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("nil tracing OnCompressContext should delegate: %v", err)
	}
}

func TestEventConversionMiddlewareAGUIBranchesAndConverterErrors(t *testing.T) {
	conversionName := "event-" + "conversion"

	if NewAGUIEventMiddleware(nil).MiddlewareName() != "ag-ui-events" {
		t.Fatal("AG-UI middleware name mismatch")
	}
	if (*EventConversionMiddleware)(nil).MiddlewareName() != conversionName {
		t.Fatal("nil event middleware should return default name")
	}
	if NewEventConversionMiddleware("", nil, nil).MiddlewareName() != conversionName {
		t.Fatal("empty custom middleware name should use default")
	}

	sink := &recordingEventSink{events: make(chan ConvertedEvent, 1)}
	mw := NewEventConversionMiddleware("drop-errors", sink, func(message.Event) (ConvertedEvent, error) {
		return ConvertedEvent{}, errors.New("skip")
	})
	events, err := mw.OnReply(context.Background(), middlewareAgentStub{name: "Friday", state: statepkg.NewAgentState()}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		out := make(chan message.Event, 1)
		out <- message.NewTextBlockDeltaEvent("reply-1", "text-1", "hello")
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	if got := collectEvents(events); len(got) != 1 {
		t.Fatalf("events not preserved after converter error: %#v", got)
	}
	select {
	case event := <-sink.events:
		t.Fatalf("converter errors should not publish events: %#v", event)
	default:
	}
	if _, err := mw.OnReply(context.Background(), middlewareAgentStub{}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		return nil, errors.New("next failed")
	}); err == nil {
		t.Fatal("OnReply should return next errors")
	}
	if _, err := mw.OnReply(context.Background(), middlewareAgentStub{}, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("OnReply should reject nil event streams")
	}

	eventsToConvert := []message.Event{
		message.NewReplyStartEvent("session-1", "reply-1", "Friday"),
		message.NewExceedMaxItersEvent("reply-1", "Friday"),
		message.NewThinkingBlockDeltaEvent("reply-1", "think-1", "thought"),
		message.NewHintBlockEvent("reply-1", "hint-1", "remember", message.WithHintBlockEventSource("scheduler")),
		message.NewToolCallStartEvent("reply-1", "call-1", "Read"),
		message.NewToolCallDeltaEvent("reply-1", "call-1", `{"path"`),
		message.NewToolCallEndEvent("reply-1", "call-1"),
		message.NewToolResultStartEvent("reply-1", "call-1", "Read"),
		message.NewToolResultTextDeltaEvent("reply-1", "call-1", "done"),
		message.NewToolResultDataDeltaEvent("reply-1", "call-1", "data-1", "text/plain", "ZGF0YQ==", ""),
		message.NewToolResultEndEvent("reply-1", "call-1", message.ToolResultSuccess),
		message.NewCustomEvent("custom-event", map[string]any{"ok": true}),
	}
	for _, event := range eventsToConvert {
		converted, err := ConvertEventToAGUI(event)
		if err != nil {
			t.Fatalf("ConvertEventToAGUI(%T) returned error: %v", event, err)
		}
		payload := converted.Payload.(AGUIEvent)
		if converted.Protocol != "ag-ui" || payload.Type == "" || payload.Metadata["agentscope.event_type"] != string(event.GetType()) {
			t.Fatalf("AG-UI envelope mismatch for %T: %#v", event, converted)
		}
	}
}

func TestInboxStateChangeAndToolOffloadHelperBranches(t *testing.T) {
	state := statepkg.NewAgentState()
	agent := middlewareAgentStub{name: "Friday", state: state}
	blocks := inboxBlocks([]InboxItem{
		{Hint: "from hint", Source: "source"},
		{Blocks: message.ContentBlockList{message.NewTextBlock("from block")}},
		{},
	})
	if len(blocks) != 2 {
		t.Fatalf("inboxBlocks mismatch: %#v", blocks)
	}
	if err := appendInboxBlocks(agent, blocks); err != nil {
		t.Fatalf("appendInboxBlocks returned error: %v", err)
	}
	if len(state.Context) != 1 || state.Context[0].Role != message.RoleAssistant || len(state.Context[0].Content) != 2 {
		t.Fatalf("appendInboxBlocks should create assistant message: %#v", state.Context)
	}
	if err := appendInboxBlocks(agent, message.ContentBlockList{message.NewTextBlock("second")}); err != nil {
		t.Fatalf("appendInboxBlocks second returned error: %v", err)
	}
	if len(state.Context) != 1 || len(state.Context[0].Content) != 3 {
		t.Fatalf("appendInboxBlocks should append to assistant message: %#v", state.Context)
	}
	if NewInboxMiddleware(nil).MiddlewareName() != "inbox" {
		t.Fatal("inbox middleware name mismatch")
	}

	stateMW := NewStateChangeMiddleware(nil, WithTeamToolNames("", "NamedTeamTool"))
	if stateDigest(nil) != "" {
		t.Fatal("nil state digest should be empty")
	}
	if name, id := toolCallInfo(nil); name != "" || id != "" {
		t.Fatalf("nil tool call info mismatch: %q %q", name, id)
	}
	if !stateMW.isTeamTool(message.NewToolCallBlock("call-1", "NamedTeamTool", `{}`)) {
		t.Fatal("configured team tool should match")
	}
	if !stateMW.isTeamTool(message.NewToolCallBlock("call-2", "AnyTool", `{}`, message.WithToolCallExtra("team_tool", true))) {
		t.Fatal("team_tool extra should match")
	}
	if !stateMW.isTeamTool(message.NewToolCallBlock("call-3", "AnyTool", `{}`, message.WithToolCallExtra("agentscope.team_tool", true))) {
		t.Fatal("agentscope.team_tool extra should match")
	}
	if stateMW.isTeamTool(nil) {
		t.Fatal("nil tool call should not match team tool")
	}
	var nilStateMW *StateChangeMiddleware
	if chunks, err := nilStateMW.OnActing(context.Background(), agent, agentpkg.HookInput{}, emptyToolStream); err != nil || chunks == nil {
		t.Fatalf("nil state-change middleware should delegate, chunks=%v err=%v", chunks, err)
	}

	offload := NewToolOffloadMiddleware(nil, WithToolOffloadTimeout(0))
	if offload.MiddlewareName() != "tool-offload" {
		t.Fatal("tool offload middleware name mismatch")
	}
	if chunks, err := offload.OnActing(context.Background(), agent, agentpkg.HookInput{}, emptyToolStream); err != nil || chunks == nil {
		t.Fatalf("zero-timeout offload should delegate, chunks=%v err=%v", chunks, err)
	}
	fast := NewToolOffloadMiddleware(nil, WithToolOffloadTimeout(time.Second))
	if chunks, err := fast.OnActing(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan agentpkg.ToolChunk, error) {
		out := make(chan agentpkg.ToolChunk, 1)
		out <- *astool.NewToolChunk(message.ContentBlockList{message.NewTextBlock("fast")})
		close(out)
		return out, nil
	}); err != nil || len(collectMiddlewareChunks(chunks)) != 1 {
		t.Fatalf("fast offload should replay chunks, chunks=%v err=%v", chunks, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fast.OnActing(canceled, agent, agentpkg.HookInput{}, func(context.Context) (<-chan agentpkg.ToolChunk, error) {
		return make(chan agentpkg.ToolChunk), nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled offload should return context error: %v", err)
	}
	errorChunks := replayToolChunks(nil, errors.New("collect failed"))
	got := collectMiddlewareChunks(errorChunks)
	if len(got) != 1 || got[0].State != message.ToolResultError {
		t.Fatalf("replayToolChunks error mismatch: %#v", got)
	}
	if cloneToolCall(nil) != nil {
		t.Fatal("nil tool call clone should be nil")
	}
}

func emptyEventStream(context.Context) (<-chan message.Event, error) {
	out := make(chan message.Event)
	close(out)
	return out, nil
}

func emptyModelStream(context.Context) (<-chan asmodel.ChatResponse, error) {
	out := make(chan asmodel.ChatResponse)
	close(out)
	return out, nil
}

func emptyToolStream(context.Context) (<-chan agentpkg.ToolChunk, error) {
	out := make(chan agentpkg.ToolChunk)
	close(out)
	return out, nil
}

func collectEvents(events <-chan message.Event) []message.Event {
	collected := []message.Event{}
	for event := range events {
		collected = append(collected, event)
	}
	return collected
}

func collectResponses(responses <-chan asmodel.ChatResponse) []asmodel.ChatResponse {
	collected := []asmodel.ChatResponse{}
	for response := range responses {
		collected = append(collected, response)
	}
	return collected
}

func collectMiddlewareChunks(chunks <-chan agentpkg.ToolChunk) []agentpkg.ToolChunk {
	collected := []agentpkg.ToolChunk{}
	for chunk := range chunks {
		collected = append(collected, chunk)
	}
	return collected
}

type middlewareAgentStub struct {
	name  string
	state *statepkg.AgentState
}

func (a middlewareAgentStub) AgentName() string {
	return a.name
}

func (a middlewareAgentStub) AgentState() *statepkg.AgentState {
	return a.state
}

type traceChatModelStub struct {
	name string
}

func (m traceChatModelStub) Name() string {
	return m.name
}

func (m traceChatModelStub) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return nil, nil
}

func (m traceChatModelStub) Stream(context.Context, asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	return emptyModelStream(context.Background())
}

func (m traceChatModelStub) CountTokens(asmodel.CallRequest) (int, error) {
	return 0, nil
}

type recordingTracer struct {
	spans []*recordingTraceSpan
}

func (t *recordingTracer) StartSpan(ctx context.Context, name string, attributes map[string]any) (context.Context, TraceSpan) {
	span := &recordingTraceSpan{name: name, attributes: map[string]any{}}
	for key, value := range attributes {
		span.attributes[key] = value
	}
	t.spans = append(t.spans, span)
	return ctx, span
}

type recordingTraceSpan struct {
	name       string
	attributes map[string]any
	err        error
	ended      bool
}

func (s *recordingTraceSpan) SetAttributes(attributes map[string]any) {
	if s.attributes == nil {
		s.attributes = map[string]any{}
	}
	for key, value := range attributes {
		s.attributes[key] = value
	}
}

func (s *recordingTraceSpan) RecordError(err error) {
	s.err = err
}

func (s *recordingTraceSpan) End() {
	s.ended = true
}

type recordingEventSink struct {
	events chan ConvertedEvent
}

func (s *recordingEventSink) PublishEvent(_ context.Context, event ConvertedEvent) error {
	s.events <- event
	return nil
}
