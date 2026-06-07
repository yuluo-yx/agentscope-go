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

package middleware_test

import (
	"context"
	"testing"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/middleware"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func TestTracingMiddlewareCompressContextSpan(t *testing.T) {
	t.Parallel()

	tracer := &recordingTracer{}
	mw := middleware.NewTracingMiddleware(tracer)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	err := mw.OnCompressContext(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("OnCompressContext returned error: %v", err)
	}

	spans := tracer.Spans()
	if len(spans) != 1 || spans[0].name != "compress_context Friday" || !spans[0].ended {
		t.Fatalf("compress span mismatch: %#v", spans)
	}
}

func TestInboxMiddlewareInjectsHintsBeforeReasoning(t *testing.T) {
	t.Parallel()

	state := statepkg.NewAgentState()
	assistant, err := message.NewAssistantMessage("Friday", nil)
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	state.Context = append(state.Context, assistant)
	source := &recordingInboxSource{items: []middleware.InboxItem{{Hint: "external update", Source: "scheduler"}}}
	mw := middleware.NewInboxMiddleware(source)
	agent := fakeAgent{name: "Friday", state: state}

	events, err := mw.OnReasoning(context.Background(), agent, agentpkg.HookInput{}, func(context.Context) (<-chan message.Event, error) {
		hints := assistant.GetContentBlocks("hint")
		if len(hints) != 1 || hints[0].(*message.HintBlock).Hint != "external update" {
			t.Fatalf("inbox hint was not injected: %#v", assistant.Content)
		}
		if source := hints[0].(*message.HintBlock).Source; source == nil || *source != "scheduler" {
			t.Fatalf("inbox hint source was not preserved: %#v", hints[0])
		}
		out := make(chan message.Event)
		close(out)
		return out, nil
	})
	if err != nil {
		t.Fatalf("OnReasoning returned error: %v", err)
	}
	for range events {
	}
	if source.drained != 1 {
		t.Fatalf("inbox source should be drained once, got %d", source.drained)
	}
}

func TestToolOffloadMiddlewareReturnsPlaceholderAndPublishesCompletion(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	sink := &recordingOffloadSink{results: make(chan middleware.ToolOffloadResult, 1)}
	mw := middleware.NewToolOffloadMiddleware(sink, middleware.WithToolOffloadTimeout(10*time.Millisecond))
	call := message.NewToolCallBlock("call-1", "SlowTool", `{}`)
	agent := fakeAgent{name: "Friday", state: statepkg.NewAgentState()}

	chunks, err := mw.OnActing(
		context.Background(),
		agent,
		agentpkg.HookInput{"tool_call": call},
		func(context.Context) (<-chan agentpkg.ToolChunk, error) {
			out := make(chan agentpkg.ToolChunk)
			go func() {
				defer close(out)
				<-release
				out <- *tool.NewToolChunk(
					message.ContentBlockList{message.NewTextBlock("complete")},
					tool.WithToolChunkState(message.ToolResultSuccess),
				)
			}()
			return out, nil
		},
	)
	if err != nil {
		t.Fatalf("OnActing returned error: %v", err)
	}
	placeholder, ok := <-chunks
	if !ok {
		t.Fatalf("expected placeholder chunk")
	}
	if placeholder.State != message.ToolResultRunning || placeholder.Metadata["agentscope.offloaded"] != true {
		t.Fatalf("placeholder mismatch: %#v", placeholder)
	}
	close(release)

	select {
	case result := <-sink.results:
		if result.ToolCall == nil || result.ToolCall.ID != "call-1" || len(result.Chunks) != 1 {
			t.Fatalf("offloaded result mismatch: %#v", result)
		}
		if text := result.Chunks[0].Content.GetTextContent(""); text == nil || *text != "complete" {
			t.Fatalf("offloaded chunk text mismatch: %#v", result.Chunks)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for offloaded result")
	}
}

func TestStateChangeMiddlewarePublishesStateAndTeamUpdates(t *testing.T) {
	t.Parallel()

	sink := &recordingChangeSink{events: make(chan middleware.ChangeEvent, 2)}
	mw := middleware.NewStateChangeMiddleware(sink, middleware.WithTeamToolNames("TeamUpdate"))
	state := statepkg.NewAgentState()
	agent := fakeAgent{name: "Friday", state: state}
	call := message.NewToolCallBlock("call-team", "TeamUpdate", `{}`)

	chunks, err := mw.OnActing(
		context.Background(),
		agent,
		agentpkg.HookInput{"tool_call": call},
		func(context.Context) (<-chan agentpkg.ToolChunk, error) {
			state.TaskContext.AddTask(statepkg.NewTask("sync", "team state changed", nil))
			out := make(chan agentpkg.ToolChunk, 1)
			out <- *tool.NewToolChunk(
				message.ContentBlockList{message.NewTextBlock("changed")},
				tool.WithToolChunkState(message.ToolResultSuccess),
			)
			close(out)
			return out, nil
		},
	)
	if err != nil {
		t.Fatalf("OnActing returned error: %v", err)
	}
	for range chunks {
	}

	got := []string{}
	for len(got) < 2 {
		select {
		case event := <-sink.events:
			got = append(got, event.Type)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for change events, got %v", got)
		}
	}
	if got[0] != middleware.StateUpdatedEvent || got[1] != middleware.TeamUpdatedEvent {
		t.Fatalf("change event order mismatch: %#v", got)
	}
}

func TestEventConversionMiddlewarePublishesSSEAndPreservesEvents(t *testing.T) {
	t.Parallel()

	sink := &recordingEventSink{events: make(chan middleware.ConvertedEvent, 1)}
	mw := middleware.NewSSEEventMiddleware(sink)
	original := message.NewTextBlockDeltaEvent("reply-1", "text-1", "hello")

	events, err := mw.OnReply(
		context.Background(),
		fakeAgent{name: "Friday", state: statepkg.NewAgentState()},
		agentpkg.HookInput{},
		func(context.Context) (<-chan message.Event, error) {
			out := make(chan message.Event, 1)
			out <- original
			close(out)
			return out, nil
		},
	)
	if err != nil {
		t.Fatalf("OnReply returned error: %v", err)
	}
	if got := <-events; got != original {
		t.Fatalf("original event stream was not preserved: %#v", got)
	}

	select {
	case converted := <-sink.events:
		if converted.Protocol != "sse" || converted.Type != string(message.TextBlockDeltaType) {
			t.Fatalf("converted SSE event mismatch: %#v", converted)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for converted event")
	}

	agui, err := middleware.ConvertEventToAGUI(original)
	if err != nil {
		t.Fatalf("ConvertEventToAGUI returned error: %v", err)
	}
	payload := agui.Payload.(middleware.AGUIEvent)
	if payload.Type != "TEXT_MESSAGE_CONTENT" || payload.Delta != "hello" {
		t.Fatalf("AG-UI payload mismatch: %#v", payload)
	}
}

type fakeAgent struct {
	name  string
	state *statepkg.AgentState
}

func (a fakeAgent) AgentName() string {
	return a.name
}

func (a fakeAgent) AgentState() *statepkg.AgentState {
	return a.state
}

type recordingTracer struct {
	spans []*recordingSpan
}

func (t *recordingTracer) StartSpan(ctx context.Context, name string, attributes map[string]any) (context.Context, middleware.TraceSpan) {
	span := &recordingSpan{name: name, attributes: attributes}
	t.spans = append(t.spans, span)
	return ctx, span
}

func (t *recordingTracer) Spans() []*recordingSpan {
	return append([]*recordingSpan(nil), t.spans...)
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

func (s *recordingSpan) RecordError(err error) {
	s.err = err
}

func (s *recordingSpan) End() {
	s.ended = true
}

type recordingInboxSource struct {
	items   []middleware.InboxItem
	drained int
}

func (s *recordingInboxSource) DrainInbox(context.Context, agentpkg.AgentAccessor) ([]middleware.InboxItem, error) {
	s.drained++
	return append([]middleware.InboxItem(nil), s.items...), nil
}

type recordingOffloadSink struct {
	results chan middleware.ToolOffloadResult
}

func (s *recordingOffloadSink) CompleteOffloadedTool(_ context.Context, result middleware.ToolOffloadResult) error {
	s.results <- result
	return nil
}

type recordingChangeSink struct {
	events chan middleware.ChangeEvent
}

func (s *recordingChangeSink) PublishChange(_ context.Context, event middleware.ChangeEvent) error {
	s.events <- event
	return nil
}

type recordingEventSink struct {
	events chan middleware.ConvertedEvent
}

func (s *recordingEventSink) PublishEvent(_ context.Context, event middleware.ConvertedEvent) error {
	s.events <- event
	return nil
}
