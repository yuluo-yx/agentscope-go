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

package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

func TestAgentOptionsInputAndObservationBranches(t *testing.T) {

	t.Parallel()

	model := &coverageChatModel{name: "coverage"}
	if _, err := NewAgent(" ", "system", model); err == nil || !strings.Contains(err.Error(), "agent name is empty") {
		t.Fatalf("NewAgent empty name error = %v", err)
	}
	if _, err := NewAgent("agent", "system", nil); err == nil || !strings.Contains(err.Error(), "agent model is nil") {
		t.Fatalf("NewAgent nil model error = %v", err)
	}

	invalidOptions := []struct {
		name string
		opt  AgentOption
		want string
	}{
		{name: "toolkit", opt: WithToolkit(nil), want: "toolkit is nil"},
		{name: "offloader", opt: WithOffloader(nil), want: "offloader is nil"},
		{name: "resources", opt: WithAgentResources(nil), want: "resources are nil"},
		{name: "state", opt: WithAgentState(nil), want: "state is nil"},
		{name: "model config", opt: WithModelConfig(ModelConfig{}), want: "invalid model config"},
		{name: "context config", opt: WithContextConfig(ContextConfig{}), want: "invalid context config"},
		{name: "react config", opt: WithReActConfig(ReActConfig{}), want: "invalid ReAct config"},
		{name: "strategy", opt: WithContextStrategies(nil), want: "context strategy is nil"},
		{name: "middleware", opt: WithMiddlewares(nil), want: "middleware is nil"},
	}
	for _, tt := range invalidOptions {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAgent("agent", "system", model, tt.opt)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewAgent error = %v, want containing %q", err, tt.want)
			}
		})
	}

	state := NewAgentState()
	agent, err := NewAgent(
		"agent",
		"base",
		model,
		nil,
		WithAgentState(state),
		WithModelConfig(ModelConfig{MaxRetries: 1, FallbackModel: model}),
		WithReActConfig(ReActConfig{MaxIters: 1, StopOnReject: true}),
		WithContextStrategies(),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	if agent.AgentName() != "agent" || agent.AgentState() != state {
		t.Fatalf("agent identity/state mismatch")
	}
	if appendSystemPrompt("", " extra ") != "extra" || appendSystemPrompt(" base ", "") != "base" || appendSystemPrompt("base", "extra") != "base\nextra" {
		t.Fatalf("appendSystemPrompt mismatch")
	}
	var nilAgent *Agent
	if nilAgent.AgentName() != "" || nilAgent.AgentState() != nil || nilAgent.lastMessage() != nil {
		t.Fatalf("nil agent accessors mismatch")
	}
	if err := nilAgent.ReplyStream(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "agent is nil") {
		t.Fatalf("nil ReplyStream error = %v", err)
	}
	if err := nilAgent.Observe(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "agent is nil") {
		t.Fatalf("nil Observe error = %v", err)
	}
	if err := (&Agent{}).Observe(context.Background(), nil); err != nil {
		t.Fatalf("Observe with nil state should be a no-op: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := agent.Observe(canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Observe canceled error = %v", err)
	}

	if err := agent.appendInput(42); err == nil || !strings.Contains(err.Error(), "unsupported reply input") {
		t.Fatalf("appendInput unsupported error = %v", err)
	}
	if err := agent.appendInput(nil); err != nil {
		t.Fatalf("appendInput nil returned error: %v", err)
	}
	user, err := message.NewUserMessage("user", "hello")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	if err := agent.appendInput([]*message.Message{nil, user}); err != nil {
		t.Fatalf("appendInput messages returned error: %v", err)
	}
	user.Content = nil
	if len(agent.state.Context) != 1 || agent.state.Context[0].GetTextContent("") == nil {
		t.Fatalf("appendInput should clone non-nil messages, got %#v", agent.state.Context)
	}
	eventOnly := &Agent{state: NewAgentState()}
	if err := eventOnly.appendInput(message.NewReplyEndEvent("session", "reply")); err == nil || !strings.Contains(err.Error(), "without context") {
		t.Fatalf("appendInput event without context error = %v", err)
	}

	assistant, err := message.NewAssistantMessage("agent", []message.ContentBlock{
		message.NewToolCallBlock("call-1", "Bash", "{}"),
	}, message.WithMessageID("reply-1"))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	agent.state.Context = []*message.Message{assistant}
	confirmEvent := message.NewUserConfirmResultEvent("reply-1", []message.ConfirmResult{{
		Confirmed: true,
		ToolCall:  message.NewToolCallBlock("call-1", "Bash", "{}"),
		Rules: []permission.Rule{{
			ToolName:    "Bash",
			RuleContent: "go test:*",
			Behavior:    permission.BehaviorAllow,
			Source:      "test",
		}},
	}})
	if err := agent.observeEvent(confirmEvent); err != nil {
		t.Fatalf("observeEvent confirm returned error: %v", err)
	}
	if len(agent.state.PermissionContext.AllowRules["Bash"]) != 1 {
		t.Fatalf("confirm event should add allow rule, got %#v", agent.state.PermissionContext.AllowRules)
	}
	external := message.NewExternalExecutionResultEvent("reply-1", []*message.ToolResultBlock{
		nil,
		message.NewToolResultBlock("call-1", "Bash", message.ToolResultOutput{Raw: "ok"}, message.ToolResultSuccess),
	})
	if err := agent.observeEvent(external); err != nil {
		t.Fatalf("observeEvent external returned error: %v", err)
	}
	call := assistant.FindBlock("tool_call", "call-1").(*message.ToolCallBlock)
	if call.State != message.ToolCallFinished {
		t.Fatalf("external result should mark tool call finished, got %s", call.State)
	}
	if err := agent.Observe(context.Background(), 42); err == nil || !strings.Contains(err.Error(), "unsupported observe input") {
		t.Fatalf("Observe unsupported input error = %v", err)
	}
	system, err := message.NewSystemMessage("system", "invalid")
	if err != nil {
		t.Fatalf("NewSystemMessage returned error: %v", err)
	}
	if err := agent.observeMessage(system); err == nil || !strings.Contains(err.Error(), "invalid observed message") {
		t.Fatalf("observeMessage system error = %v", err)
	}
	thinking, err := message.NewAssistantMessage("assistant", []message.ContentBlock{message.NewThinkingBlock("hidden")})
	if err != nil {
		t.Fatalf("NewAssistantMessage thinking returned error: %v", err)
	}
	if err := agent.observeMessage(thinking); err == nil || !strings.Contains(err.Error(), "invalid observed message") {
		t.Fatalf("observeMessage thinking error = %v", err)
	}
	if err := agent.Observe(context.Background(), []*message.Message{nil}); err != nil {
		t.Fatalf("Observe nil message slice returned error: %v", err)
	}
	if err := agent.Observe(context.Background(), []message.Event{nil}); err != nil {
		t.Fatalf("Observe nil event slice returned error: %v", err)
	}
	if err := agent.observeEvent(message.NewReplyEndEvent("session", "missing")); err == nil || !strings.Contains(err.Error(), "without matching context") {
		t.Fatalf("observeEvent missing reply error = %v", err)
	}
	if err := agent.applyIncomingEventToMessage(nil, message.NewReplyEndEvent("session", "reply-1")); err == nil || !strings.Contains(err.Error(), "without context") {
		t.Fatalf("applyIncomingEventToMessage nil error = %v", err)
	}
	if err := agent.emitAndApply(assistant, nil, func(message.Event) error { return errors.New("should not run") }); err != nil {
		t.Fatalf("emitAndApply nil event should be a no-op: %v", err)
	}
}

func TestContextOffloadSplitAndTruncateBranches(t *testing.T) {

	t.Parallel()

	offloader := &coverageOffloader{}
	agent := &Agent{
		state:         NewAgentState(),
		offloader:     offloader,
		contextConfig: ContextConfig{ToolResultLimit: 8},
	}
	data := message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"), message.WithDataBlockID("data-1"))
	nested := message.NewToolResultBlock("tool-1", "Read", message.ToolResultOutput{Blocks: message.ContentBlockList{
		message.NewTextBlock("prefix"),
		message.NewDataBlock(message.NewBase64Source("aW1n", "image/png"), message.WithDataBlockID("data-2")),
	}}, message.ToolResultSuccess)
	msg, err := message.NewAssistantMessage("agent", []message.ContentBlock{data, nested})
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	agent.state.Context = []*message.Message{nil, msg}
	agent.state.Summary.Blocks = message.ContentBlockList{
		message.NewDataBlock(message.NewBase64Source("c3VtbWFyeQ==", "text/plain"), message.WithDataBlockID("summary-data")),
	}
	if err := agent.offloadDataBlocks(context.Background()); err != nil {
		t.Fatalf("offloadDataBlocks returned error: %v", err)
	}
	directData := msg.FindBlock("data", "data-1").(*message.DataBlock)
	if _, ok := directData.Source.(*message.URLSource); !ok {
		t.Fatalf("direct DataBlock should be replaced with URL source: %#v", directData.Source)
	}
	nestedResult := msg.FindBlock("tool_result", "tool-1").(*message.ToolResultBlock)
	if _, ok := nestedResult.Output.Blocks[1].(*message.DataBlock).Source.(*message.URLSource); !ok {
		t.Fatalf("nested DataBlock should be replaced with URL source: %#v", nestedResult.Output.Blocks[1])
	}
	if _, ok := agent.state.Summary.Blocks[0].(*message.DataBlock).Source.(*message.URLSource); !ok {
		t.Fatalf("summary DataBlock should be replaced with URL source")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := agent.offloadDataBlocksInList(canceled, message.ContentBlockList{message.NewTextBlock("x")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("offloadDataBlocksInList canceled error = %v", err)
	}

	if reserved, offloaded, ok := splitToolResult(nil, 10); ok || reserved != nil || offloaded != nil {
		t.Fatalf("splitToolResult nil mismatch")
	}
	raw := message.NewToolResultBlock("raw", "Bash", message.ToolResultOutput{Raw: "hello世界"}, message.ToolResultSuccess)
	reserved, offloaded, ok := splitToolResult(raw, 7)
	if !ok || reserved.Output.Raw != "hello" || offloaded.Output.Raw != "世界" {
		t.Fatalf("splitToolResult raw mismatch: reserved=%#v offloaded=%#v ok=%v", reserved, offloaded, ok)
	}
	blocks := message.NewToolResultBlock("blocks", "Read", message.ToolResultOutput{Blocks: message.ContentBlockList{
		message.NewDataBlock(message.NewURLSource("file:///kept", "text/plain")),
		message.NewTextBlock("abcdef", message.WithBlockID("text-1")),
		message.NewTextBlock("tail"),
	}}, message.ToolResultSuccess)
	reserved, offloaded, ok = splitToolResult(blocks, 3)
	if !ok || len(reserved.Output.Blocks) != 2 || len(offloaded.Output.Blocks) != 2 {
		t.Fatalf("splitToolResult blocks mismatch: reserved=%#v offloaded=%#v ok=%v", reserved, offloaded, ok)
	}

	large := message.NewToolResultBlock("large", "Read", message.ToolResultOutput{Raw: "0123456789abcdef"}, message.ToolResultSuccess)
	if err := agent.offloadToolResult(context.Background(), large, 5); err != nil {
		t.Fatalf("offloadToolResult returned error: %v", err)
	}
	if !toolResultHasOffloadReminder(large) || len(offloader.toolResults) != 1 {
		t.Fatalf("offloadToolResult should append reminder and offload tail, got %#v", large)
	}
	if err := agent.offloadToolResult(context.Background(), large, 5); err != nil || len(offloader.toolResults) != 1 {
		t.Fatalf("offloadToolResult should skip existing reminders, err=%v count=%d", err, len(offloader.toolResults))
	}

	truncated := message.NewToolResultBlock("truncate", "Read", message.ToolResultOutput{
		Raw: "0123456789",
		Blocks: message.ContentBlockList{
			message.NewTextBlock("abcdefghij"),
			message.NewDataBlock(message.NewURLSource("file:///x", "text/plain")),
		},
	}, message.ToolResultSuccess)
	truncateToolResult(truncated, 4)
	if !strings.Contains(truncated.Output.Raw, "truncated") || !strings.Contains(truncated.Output.Blocks[0].(*message.TextBlock).Text, "truncated") {
		t.Fatalf("truncateToolResult mismatch: %#v", truncated)
	}
	if head, tail := splitUTF8("世界abc", 4); head != "世" || tail != "界abc" {
		t.Fatalf("splitUTF8 multibyte = %q, %q", head, tail)
	}
	if got := truncateUTF8("世界", 0); got != "" {
		t.Fatalf("truncateUTF8 zero = %q", got)
	}
}

func TestContextStrategyInputAndSummaryBranches(t *testing.T) {

	t.Parallel()

	model := &coverageChatModel{name: "summary", countTokens: 20}
	state := NewAgentState()
	state.ToolContext.ActivatedGroups = []string{"default"}
	state.Summary.Text = "previous summary"
	user, err := message.NewUserMessage("user", "current")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	state.Context = []*message.Message{nil, user}
	schema := ToolSchema{Type: "function", Function: FunctionSchema{Name: "Search"}}
	input := &ContextStrategyInput{
		State:        state,
		Model:        model,
		Config:       ContextConfig{MaxTokens: 10, TriggerRatio: 0.5, ReserveRatio: 0.1, SummaryTemplate: "{task_overview}", SummarySchema: DefaultSummarySchema()},
		activeGroups: []string{"default"},
		systemPrompt: func(context.Context) (string, error) { return "system", nil },
		toolSchemas:  func() ([]ToolSchema, error) { return []ToolSchema{schema}, nil },
	}
	groups := input.ActiveGroups()
	groups[0] = "changed"
	if input.activeGroups[0] != "default" {
		t.Fatalf("ActiveGroups should clone, got %#v", input.activeGroups)
	}
	if prompt, err := input.SystemPrompt(context.Background()); err != nil || prompt != "system" {
		t.Fatalf("SystemPrompt = %q, %v", prompt, err)
	}
	if tools, err := input.ToolSchemas(); err != nil || len(tools) != 1 || tools[0].Function.Name != "Search" {
		t.Fatalf("ToolSchemas = %#v, %v", tools, err)
	}
	request, err := input.CurrentModelRequest(context.Background())
	if err != nil {
		t.Fatalf("CurrentModelRequest returned error: %v", err)
	}
	if len(request.Messages) != 3 || len(request.Tools) != 1 {
		t.Fatalf("CurrentModelRequest mismatch: %#v", request)
	}
	var nilInput *ContextStrategyInput
	if nilInput.ActiveGroups() != nil {
		t.Fatalf("nil ActiveGroups should return nil")
	}
	if prompt, err := nilInput.SystemPrompt(context.Background()); err != nil || prompt != "" {
		t.Fatalf("nil SystemPrompt = %q, %v", prompt, err)
	}
	if tools, err := nilInput.ToolSchemas(); err != nil || tools != nil {
		t.Fatalf("nil ToolSchemas = %#v, %v", tools, err)
	}
	if request, err := nilInput.CurrentModelRequest(context.Background()); err != nil || len(request.Messages) != 0 {
		t.Fatalf("nil CurrentModelRequest = %#v, %v", request, err)
	}

	if NewToolResultContextStrategy().ContextStrategyName() != "tool-result" || NewSummaryContextStrategy().ContextStrategyName() != "summary" {
		t.Fatalf("strategy names mismatch")
	}
	if err := (ToolResultContextStrategy{}).ApplyContextStrategy(context.Background(), nil); err != nil {
		t.Fatalf("ToolResultContextStrategy nil input returned error: %v", err)
	}
	if summaryPreconditionsMet(nil) || summaryPreconditionsMet(&ContextStrategyInput{State: state}) {
		t.Fatalf("summary preconditions should reject incomplete input")
	}
	needed, err := isSummaryNeeded(context.Background(), input)
	if err != nil || !needed {
		t.Fatalf("isSummaryNeeded = %v, %v", needed, err)
	}
	model.countErr = errors.New("count failed")
	if _, err := isSummaryNeeded(context.Background(), input); !errors.Is(err, model.countErr) {
		t.Fatalf("isSummaryNeeded count error = %v", err)
	}

	if _, err := summaryTextFromResponse(nil, input.Config); err == nil || !strings.Contains(err.Error(), "nil response") {
		t.Fatalf("summaryTextFromResponse nil error = %v", err)
	}
	if _, err := summaryTextFromResponse(asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("  ")}, true), input.Config); err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("summaryTextFromResponse empty error = %v", err)
	}
	text, err := summaryTextFromResponse(asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("```json\n{\"task_overview\":\"done\"}\n```")}, true), input.Config)
	if err != nil || text != "done" {
		t.Fatalf("summaryTextFromResponse JSON = %q, %v", text, err)
	}
	text, err = summaryTextFromResponse(asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("plain summary")}, true), input.Config)
	if err != nil || text != "plain summary" {
		t.Fatalf("summaryTextFromResponse plain = %q, %v", text, err)
	}
	if got := stripJSONFence("```{\"a\":1}```"); got != "{\"a\":1}" {
		t.Fatalf("stripJSONFence = %q", got)
	}
	if got := marshalSchema(map[string]any{"bad": func() {}}); got != "{}" {
		t.Fatalf("marshalSchema bad = %q", got)
	}
	state.Summary.Text = ""
	state.Summary.Blocks = message.ContentBlockList{message.NewTextBlock("block summary")}
	if summary := summaryMessageFromState(state); summary == nil || summary.GetTextContent("") == nil || *summary.GetTextContent("") != "block summary" {
		t.Fatalf("summaryMessageFromState blocks = %#v", summary)
	}
	if cloneMessages(nil) != nil {
		t.Fatalf("cloneMessages nil should return nil")
	}
	cloned := cloneMessages([]*message.Message{nil, user})
	user.Content = nil
	if len(cloned) != 2 || cloned[1].GetTextContent("") == nil {
		t.Fatalf("cloneMessages should preserve nil and clone messages: %#v", cloned)
	}
}

func TestReasoningEventEmissionBranches(t *testing.T) {

	t.Parallel()

	agent := &Agent{state: NewAgentState()}
	agent.state.ReplyID = "reply-1"
	events := collectAgentEvents(t, func(emit func(message.Event) error) error {
		return agent.emitChatResponse(asmodel.NewChatResponse(message.ContentBlockList{
			message.NewTextBlock("hello", message.WithBlockID("text-1")),
			message.NewThinkingBlock("think", message.WithThinkingBlockID("think-1")),
			message.NewToolCallBlock("call-1", "Search", `{"q":"go"}`),
			message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"), message.WithDataBlockID("data-1")),
			message.NewDataBlock(message.NewURLSource("https://example.test/data", "text/plain"), message.WithDataBlockID("data-2")),
		}, true), emit)
	})
	assertAgentEventTypes(t, events, []message.EventType{
		message.TextBlockStartType,
		message.TextBlockDeltaType,
		message.TextBlockEndType,
		message.ThinkingBlockStartType,
		message.ThinkingBlockDeltaType,
		message.ThinkingBlockEndType,
		message.ToolCallStartType,
		message.ToolCallDeltaType,
		message.ToolCallEndType,
		message.DataBlockStartType,
		message.DataBlockDeltaType,
		message.DataBlockEndType,
		message.DataBlockStartType,
		message.DataBlockDeltaType,
		message.DataBlockEndType,
	})

	emptyStream := make(chan ChatResponse)
	close(emptyStream)
	if _, err := agent.emitChatResponseStream(emptyStream, func(message.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "did not return a final response") {
		t.Fatalf("emitChatResponseStream empty error = %v", err)
	}
	errorStream := make(chan ChatResponse, 1)
	errorStream <- *asmodel.NewChatResponse(nil, false, asmodel.WithChatResponseError(errors.New("model chunk failed")))
	close(errorStream)
	if _, err := agent.emitChatResponseStream(errorStream, func(message.Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "model chunk failed") {
		t.Fatalf("emitChatResponseStream model error = %v", err)
	}
	finalOnly := make(chan ChatResponse, 1)
	finalOnly <- *asmodel.NewChatResponse(message.ContentBlockList{
		message.NewTextBlock("final", message.WithBlockID("final-text")),
		message.NewDataBlock(nil, message.WithDataBlockID("unknown-data")),
	}, true)
	close(finalOnly)
	events = collectAgentEvents(t, func(emit func(message.Event) error) error {
		_, err := agent.emitChatResponseStream(finalOnly, emit)
		return err
	})
	if !hasAgentEventType(events, message.TextBlockStartType) {
		t.Fatalf("final-only stream should emit full response, got %#v", eventTypes(events))
	}

	stream := make(chan ChatResponse, 3)
	stream <- *asmodel.NewChatResponse(message.ContentBlockList{
		message.NewTextBlock("hel", message.WithBlockID("text-stream")),
		message.NewToolCallBlock("call-stream", "Search", `{"q":"a"}`),
	}, false)
	stream <- *asmodel.NewChatResponse(message.ContentBlockList{
		message.NewThinkingBlock("partial", message.WithThinkingBlockID("think-stream")),
	}, false)
	stream <- *asmodel.NewChatResponse(message.ContentBlockList{
		message.NewTextBlock("hello", message.WithBlockID("text-stream")),
		message.NewThinkingBlock("partial", message.WithThinkingBlockID("think-stream")),
		message.NewToolCallBlock("call-stream", "Search", `{"q":"a"}`),
		message.NewDataBlock(message.NewBase64Source("ZmluYWw=", "text/plain"), message.WithDataBlockID("data-final")),
	}, true)
	close(stream)
	events = collectAgentEvents(t, func(emit func(message.Event) error) error {
		_, err := agent.emitChatResponseStream(stream, emit)
		return err
	})
	if !hasAgentEventType(events, message.DataBlockStartType) || !hasAgentEventType(events, message.ToolCallEndType) || !hasAgentEventType(events, message.ThinkingBlockEndType) {
		t.Fatalf("stream should emit final unseen/open block events, got %#v", eventTypes(events))
	}
	if err := agent.emitDeltaBlock(textDeltaBlock, "text-error", "x", func(message.Event) error { return errors.New("emit failed") }); err == nil {
		t.Fatalf("emitDeltaBlock should propagate emit errors")
	}
	if err := agent.emitToolCallBlock(message.NewToolCallBlock("call-error", "Search", "{}"), func(message.Event) error { return errors.New("emit failed") }); err == nil {
		t.Fatalf("emitToolCallBlock should propagate emit errors")
	}
	if err := agent.emitStreamChunkBlock(nil, newModelStreamState(), &modelStreamChunkState{currentTools: map[string]bool{}}, func(message.Event) error { return nil }); err != nil {
		t.Fatalf("emitStreamChunkBlock unknown block should be ignored: %v", err)
	}
	if err := agent.emitDataBlock(&message.DataBlock{}, func(message.Event) error { return errors.New("should not run") }); err != nil {
		t.Fatalf("emitDataBlock unknown source should be ignored: %v", err)
	}
}

func TestReasoningModelInputCallModelAndSummaryBranches(t *testing.T) {

	t.Parallel()

	state := NewAgentState()
	state.Summary.Text = "previous summary"
	user, err := message.NewUserMessage("user", "current task")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	state.Context = []*message.Message{nil, user}
	state.ToolContext.ActivatedGroups = []string{"active"}
	provider := &coverageToolProvider{schemas: []ToolSchema{{Type: "function", Function: FunctionSchema{Name: "Search"}}}}
	primaryErr := errors.New("primary failed")
	primary := &coverageChatModel{name: "primary", streamErr: primaryErr}
	fallback := &coverageChatModel{name: "fallback", responses: []*ChatResponse{
		asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("fallback")}, true),
	}}
	agent := &Agent{
		name:         "agent",
		systemPrompt: "base system",
		state:        state,
		model:        primary,
		modelConfig:  ModelConfig{MaxRetries: 2, FallbackModel: fallback},
		toolkit:      provider,
	}

	request, err := agent.prepareModelInput(context.Background())
	if err != nil {
		t.Fatalf("prepareModelInput returned error: %v", err)
	}
	if len(request.Messages) != 3 || len(request.Tools) != 1 {
		t.Fatalf("prepareModelInput should include system, summary, context, and tools: %#v", request)
	}
	if text := request.Messages[1].GetTextContent(""); text == nil || *text != "previous summary" {
		t.Fatalf("summary text message mismatch: %#v", request.Messages[1])
	}
	user.Content = nil
	if text := request.Messages[2].GetTextContent(""); text == nil || *text != "current task" {
		t.Fatalf("context message should be cloned: %#v", request.Messages[2])
	}
	if summary := (&Agent{}).summaryMessage(); summary != nil {
		t.Fatalf("missing state summary should be nil: %#v", summary)
	}
	state.Summary.Text = ""
	state.Summary.Blocks = message.ContentBlockList{message.NewTextBlock("block summary")}
	if summary := agent.summaryMessage(); summary == nil || summary.GetTextContent("") == nil || *summary.GetTextContent("") != "block summary" {
		t.Fatalf("summary block message mismatch: %#v", summary)
	}

	responses, err := agent.callModel(context.Background(), request)
	if err != nil {
		t.Fatalf("callModel should fall back after retries: %v", err)
	}
	if len(primary.streams) != 2 || len(fallback.streams) != 1 {
		t.Fatalf("callModel retry/fallback counts mismatch: primary=%d fallback=%d", len(primary.streams), len(fallback.streams))
	}
	if !fallback.streams[0].Stream {
		t.Fatalf("callModel should force streaming requests: %#v", fallback.streams[0])
	}
	drained := drainChatResponses(responses)
	if len(drained) != 1 || !drained[0].IsLast {
		t.Fatalf("fallback response stream mismatch: %#v", drained)
	}

	nilStreamModel := &coverageChatModel{name: "nil-stream", nilStream: true}
	agent.model = nilStreamModel
	agent.modelConfig = ModelConfig{MaxRetries: 1}
	agent.modelCallHooks = nil
	if _, err := agent.callModel(context.Background(), request); err == nil || !strings.Contains(err.Error(), "nil response stream") {
		t.Fatalf("nil stream error mismatch: %v", err)
	}

	baseModel := &coverageChatModel{name: "base", responses: []*ChatResponse{
		asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("base")}, true),
	}}
	replacement := &coverageChatModel{name: "replacement", responses: []*ChatResponse{
		asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("replacement")}, true),
	}}
	agent.model = baseModel
	agent.modelCallHooks = []ModelCallHook{
		func(ctx context.Context, _ AgentAccessor, input HookInput, next ModelCallHandler) (<-chan ChatResponse, error) {
			input["model"] = replacement
			updated := input["request"].(CallRequest)
			updated.Metadata = map[string]any{"from_hook": true}
			input["request"] = &updated
			return next(ctx)
		},
	}
	responses, err = agent.callModel(context.Background(), request)
	if err != nil {
		t.Fatalf("callModel with hook returned error: %v", err)
	}
	if len(baseModel.streams) != 0 || len(replacement.streams) != 1 {
		t.Fatalf("model hook should replace model: base=%d replacement=%d", len(baseModel.streams), len(replacement.streams))
	}
	if got, ok := replacement.streams[0].Metadata["from_hook"].(bool); !ok || !got {
		t.Fatalf("model hook should update request metadata: %#v", replacement.streams[0].Metadata)
	}
	if len(drainChatResponses(responses)) != 1 {
		t.Fatalf("hook response stream should be drainable")
	}
}

func TestCompositeToolProviderAndEmptyProviderBranches(t *testing.T) {

	t.Parallel()

	empty := emptyToolProvider{}
	if schemas, err := empty.ToolSchemas(); err != nil || schemas != nil {
		t.Fatalf("empty ToolSchemas = %#v, %v", schemas, err)
	}
	if found, ok := empty.FindTool("missing"); ok || found != nil {
		t.Fatalf("empty FindTool = %#v, %v", found, ok)
	}
	if _, err := empty.CallTool(context.Background(), message.NewToolCallBlock("call", "Missing", "{}"), NewAgentState()); err == nil || !strings.Contains(err.Error(), "no toolkit configured") {
		t.Fatalf("empty CallTool error = %v", err)
	}
	if _, err := (compositeToolProvider{}).CallTool(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "nil tool call") {
		t.Fatalf("composite nil call error = %v", err)
	}
	if _, err := (compositeToolProvider{
		primary:   &coverageToolProvider{schemas: []ToolSchema{{Type: "function", Function: FunctionSchema{Name: "dup"}}}},
		secondary: &coverageToolProvider{schemas: []ToolSchema{{Type: "function", Function: FunctionSchema{Name: "dup"}}}},
	}).ToolSchemas(); err == nil || !strings.Contains(err.Error(), "duplicate tool schema") {
		t.Fatalf("composite duplicate schema error = %v", err)
	}
	schemaErr := errors.New("schema failed")
	if _, err := (compositeToolProvider{primary: &coverageToolProvider{schemaErr: schemaErr}}).ToolSchemas(); !errors.Is(err, schemaErr) {
		t.Fatalf("composite primary schema error = %v", err)
	}
	if got := composeToolProviders(nil, empty); got == nil {
		t.Fatalf("composeToolProviders should return secondary when primary is nil")
	}
	if got := composeToolProviders(empty, nil); got == nil {
		t.Fatalf("composeToolProviders should return primary when secondary is nil")
	}

	state := NewAgentState()
	state.ToolContext.ActivatedGroups = []string{"group-a"}
	primary := &coverageToolProvider{name: "primary", tools: map[string]bool{"Primary": true}}
	secondary := &coverageToolProvider{name: "secondary", tools: map[string]bool{"Secondary": true}}
	provider := compositeToolProvider{primary: primary, secondary: secondary}
	if _, ok := provider.FindTool("Primary", "group-a"); !ok {
		t.Fatalf("FindTool should find primary tool")
	}
	if _, ok := provider.FindTool("Secondary", "group-a"); !ok {
		t.Fatalf("FindTool should find secondary tool")
	}
	if _, err := provider.CallTool(context.Background(), message.NewToolCallBlock("call-primary", "Primary", "{}"), state); err != nil || primary.callCount != 1 {
		t.Fatalf("CallTool primary err=%v count=%d", err, primary.callCount)
	}
	if _, err := provider.CallTool(context.Background(), message.NewToolCallBlock("call-secondary", "Secondary", "{}"), state); err != nil || secondary.callCount != 1 {
		t.Fatalf("CallTool secondary err=%v count=%d", err, secondary.callCount)
	}
	if _, err := provider.CallTool(context.Background(), message.NewToolCallBlock("call-fallback", "Missing", "{}"), state); err != nil || primary.callCount != 2 {
		t.Fatalf("CallTool should fall back to primary provider, err=%v count=%d", err, primary.callCount)
	}
	groups := activeGroupsFromState(state)
	groups[0] = "changed"
	if state.ToolContext.ActivatedGroups[0] != "group-a" {
		t.Fatalf("activeGroupsFromState should clone, got %#v", state.ToolContext.ActivatedGroups)
	}
	if (&Agent{}).activeGroups() != nil {
		t.Fatalf("agent activeGroups without state should return nil")
	}

	primary.schemas = []ToolSchema{{Type: "function", Function: FunctionSchema{Name: "Primary"}}}
	secondary.schemas = []ToolSchema{{Type: "function", Function: FunctionSchema{Name: "Secondary"}}}
	model := &coverageChatModel{name: "toolkit"}
	agent, err := NewAgent("agent", "system", model, WithToolkit(primary), WithAdditionalToolkit(secondary))
	if err != nil {
		t.Fatalf("NewAgent with additional toolkit returned error: %v", err)
	}
	schemas, err := agent.effectiveToolProvider().ToolSchemas()
	if err != nil || len(schemas) != 2 {
		t.Fatalf("additional toolkit schemas = %#v, %v", schemas, err)
	}
}

func TestActingPreparationExecutionAndDataEmissionBranches(t *testing.T) {

	t.Parallel()

	state := NewAgentState()
	state.ReplyID = "reply-acting"
	assistant, err := message.NewAssistantMessage("agent", []message.ContentBlock{
		message.NewToolCallBlock("call-1", "Search", `{"q":"go"}`),
	}, message.WithMessageID("reply-acting"))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}

	missingAgent := &Agent{state: state, toolkit: &actingProvider{}}
	events := collectAgentEvents(t, func(emit func(message.Event) error) error {
		_, waiting, err := missingAgent.prepareToolCall(context.Background(), assistant, message.NewToolCallBlock("missing", "Missing", `{}`), emit)
		if waiting {
			t.Fatalf("missing tool should not wait")
		}
		return err
	})
	assertAgentEventTypes(t, events, []message.EventType{
		message.ToolResultStartType,
		message.ToolResultTextDeltaType,
		message.ToolResultEndType,
	})

	provider := &actingProvider{tools: map[string]actingTool{
		"Search": {name: "Search", decision: &permission.Decision{Behavior: permission.BehaviorAllow, UpdatedInput: map[string]any{"q": "rewritten"}}},
		"Deny":   {name: "Deny", decision: &permission.Decision{Behavior: permission.BehaviorDeny, Message: "denied"}},
		"Remote": {name: "Remote", external: true, decision: &permission.Decision{Behavior: permission.BehaviorAllow}},
	}}
	agent := &Agent{state: state, toolkit: provider}
	toolCall := message.NewToolCallBlock("call-allow", "Search", `{"q":"go"}`)
	plan, waiting, err := agent.prepareToolCall(context.Background(), assistant, toolCall, func(message.Event) error { return nil })
	if err != nil || waiting || plan == nil {
		t.Fatalf("allow prepare mismatch: plan=%#v waiting=%v err=%v", plan, waiting, err)
	}
	if !strings.Contains(toolCall.Input, "rewritten") {
		t.Fatalf("UpdatedInput should rewrite tool call JSON, got %s", toolCall.Input)
	}
	_, waiting, err = agent.prepareToolCall(context.Background(), assistant, message.NewToolCallBlock("bad-json", "Search", `{bad`), func(message.Event) error { return nil })
	if err != nil || waiting {
		t.Fatalf("invalid JSON should emit tool error without returning error: waiting=%v err=%v", waiting, err)
	}
	_, waiting, err = agent.prepareToolCall(context.Background(), assistant, message.NewToolCallBlock("deny", "Deny", `{}`), func(message.Event) error { return nil })
	if err != nil || waiting {
		t.Fatalf("deny should finish without waiting: waiting=%v err=%v", waiting, err)
	}
	_, waiting, err = agent.prepareToolCall(context.Background(), assistant, message.NewToolCallBlock("remote", "Remote", `{}`), func(message.Event) error { return nil })
	if err != nil || !waiting {
		t.Fatalf("external tool should wait for external execution: waiting=%v err=%v", waiting, err)
	}

	provider.callErr = errors.New("tool failed")
	if err := agent.executeLocalTool(context.Background(), assistant, message.NewToolCallBlock("call-error", "Search", `{}`), func(message.Event) error { return nil }); err != nil {
		t.Fatalf("executeLocalTool should emit tool errors and swallow provider error: %v", err)
	}
	provider.callErr = nil
	provider.nilStream = true
	if err := agent.executeLocalTool(context.Background(), assistant, message.NewToolCallBlock("call-nil", "Search", `{}`), func(message.Event) error { return nil }); err != nil {
		t.Fatalf("executeLocalTool should emit nil stream error: %v", err)
	}

	events = collectAgentEvents(t, func(emit func(message.Event) error) error {
		if err := agent.emitToolResultData(assistant, message.NewToolCallBlock("data-b64", "Search", `{}`), message.NewDataBlock(message.NewBase64Source("aGVsbG8=", "text/plain"), message.WithDataBlockID("data-b64")), emit); err != nil {
			return err
		}
		if err := agent.emitToolResultData(assistant, message.NewToolCallBlock("data-url", "Search", `{}`), message.NewDataBlock(message.NewURLSource("https://example.test/file", "text/plain"), message.WithDataBlockID("data-url")), emit); err != nil {
			return err
		}
		return agent.emitToolResultData(assistant, message.NewToolCallBlock("data-empty", "Search", `{}`), &message.DataBlock{}, emit)
	})
	assertAgentEventTypes(t, events, []message.EventType{
		message.ToolResultDataDeltaType,
		message.ToolResultDataDeltaType,
	})
}

func TestRunActingBatchesSafeToolsFlushesSerialToolsAndAppliesHooks(t *testing.T) {
	t.Parallel()

	state := NewAgentState()
	state.ReplyID = "reply-batch"
	provider := &actingProvider{
		tools: map[string]actingTool{
			"SafeText":   {name: "SafeText"},
			"SafeData":   {name: "SafeData"},
			"SerialText": {name: "SerialText", serial: true},
		},
		chunks: map[string][]ToolChunk{
			"SafeText": {
				*astool.NewToolChunk(
					message.ContentBlockList{message.NewTextBlock("safe")},
					astool.WithToolChunkState(message.ToolResultSuccess),
				),
			},
			"SafeData": {
				*astool.NewToolChunk(
					message.ContentBlockList{
						message.NewDataBlock(
							message.NewBase64Source("aGVsbG8=", "text/plain"),
							message.WithDataBlockID("data-safe"),
						),
					},
					astool.WithToolChunkState(message.ToolResultSuccess),
				),
			},
			"SerialText": {
				*astool.NewToolChunk(
					message.ContentBlockList{message.NewTextBlock("serial")},
					astool.WithToolChunkState(message.ToolResultSuccess),
				),
			},
		},
	}
	agent := &Agent{state: state, toolkit: provider}

	var hookCalls atomic.Int64
	agent.actingHooks = []ActingHook{
		func(ctx context.Context, _ AgentAccessor, input HookInput, next ToolHandler) (<-chan ToolChunk, error) {
			if input[toolCallHook] == nil {
				t.Fatalf("acting hook should receive tool_call input")
			}
			hookCalls.Add(1)
			return next(ctx)
		},
		func(ctx context.Context, _ AgentAccessor, _ HookInput, next ToolHandler) (<-chan ToolChunk, error) {
			hookCalls.Add(1)
			return next(ctx)
		},
	}

	calls := []*message.ToolCallBlock{
		message.NewToolCallBlock("safe-text", "SafeText", `{}`),
		message.NewToolCallBlock("safe-data", "SafeData", `{}`),
		message.NewToolCallBlock("serial-text", "SerialText", `{}`),
	}
	assistant, err := message.NewAssistantMessage("agent", []message.ContentBlock{calls[0], calls[1], calls[2]}, message.WithMessageID("reply-batch"))
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}

	events := collectAgentEvents(t, func(emit func(message.Event) error) error {
		waiting, err := agent.runActing(context.Background(), assistant, calls, emit)
		if waiting {
			t.Fatalf("runActing should not wait for local tools")
		}
		return err
	})
	assertAgentEventTypes(t, events, []message.EventType{
		message.ToolResultStartType,
		message.ToolResultStartType,
		message.ToolResultTextDeltaType,
		message.ToolResultEndType,
		message.ToolResultDataDeltaType,
		message.ToolResultEndType,
		message.ToolResultStartType,
		message.ToolResultTextDeltaType,
		message.ToolResultEndType,
	})
	for _, call := range calls {
		if call.State != message.ToolCallFinished {
			t.Fatalf("tool call %s should be finished, got %s", call.ID, call.State)
		}
	}
	if hookCalls.Load() != 6 {
		t.Fatalf("two acting hooks should wrap three executions, got %d calls", hookCalls.Load())
	}

	singleCall := message.NewToolCallBlock("single", "SafeText", `{}`)
	singleAssistant, err := message.NewAssistantMessage("agent", []message.ContentBlock{singleCall}, message.WithMessageID("reply-batch-single"))
	if err != nil {
		t.Fatalf("NewAssistantMessage single returned error: %v", err)
	}
	events = collectAgentEvents(t, func(emit func(message.Event) error) error {
		waiting, err := agent.runActing(context.Background(), singleAssistant, []*message.ToolCallBlock{singleCall}, emit)
		if waiting {
			t.Fatalf("single runActing should not wait")
		}
		return err
	})
	assertAgentEventTypes(t, events, []message.EventType{
		message.ToolResultStartType,
		message.ToolResultTextDeltaType,
		message.ToolResultEndType,
	})
	if singleCall.State != message.ToolCallFinished {
		t.Fatalf("single tool call should be finished, got %s", singleCall.State)
	}
}

func collectAgentEvents(t *testing.T, run func(func(message.Event) error) error) []message.Event {

	t.Helper()

	var events []message.Event
	if err := run(func(event message.Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	return events
}

func assertAgentEventTypes(t *testing.T, events []message.Event, want []message.EventType) {

	t.Helper()

	got := eventTypes(events)
	if len(got) != len(want) {
		t.Fatalf("event type length mismatch: got %#v want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("event type mismatch: got %#v want %#v", got, want)
		}
	}
}

func eventTypes(events []message.Event) []message.EventType {

	out := make([]message.EventType, 0, len(events))
	for _, event := range events {
		out = append(out, event.GetType())
	}
	return out
}

func hasAgentEventType(events []message.Event, target message.EventType) bool {

	for _, event := range events {
		if event.GetType() == target {
			return true
		}
	}
	return false
}

type coverageChatModel struct {
	name        string
	responses   []*ChatResponse
	callErr     error
	streamErr   error
	nilStream   bool
	countTokens int
	countErr    error
	calls       []CallRequest
	streams     []CallRequest
}

func (m *coverageChatModel) Name() string {

	if m.name == "" {
		return "coverage"
	}
	return m.name
}

func (m *coverageChatModel) Call(_ context.Context, request CallRequest) (*ChatResponse, error) {

	m.calls = append(m.calls, request.Clone())
	if m.callErr != nil {
		return nil, m.callErr
	}
	if len(m.responses) == 0 {
		return asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("ok")}, true), nil
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func (m *coverageChatModel) Stream(_ context.Context, request CallRequest) (<-chan ChatResponse, error) {

	m.streams = append(m.streams, request.Clone())
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	if m.nilStream {
		return nil, nil
	}
	ch := make(chan ChatResponse, len(m.responses))
	for _, response := range m.responses {
		ch <- *response.Clone()
	}
	close(ch)
	return ch, nil
}

func (m *coverageChatModel) CountTokens(request CallRequest) (int, error) {

	if m.countErr != nil {
		return 0, m.countErr
	}
	if m.countTokens > 0 {
		return m.countTokens, nil
	}
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func drainChatResponses(responses <-chan ChatResponse) []ChatResponse {

	var out []ChatResponse
	for response := range responses {
		out = append(out, response)
	}
	return out
}

type coverageOffloader struct {
	dataErr     error
	toolErr     error
	contextErr  error
	dataBlocks  []*message.DataBlock
	toolResults []*message.ToolResultBlock
	contexts    [][]*message.Message
}

func (o *coverageOffloader) OffloadContext(_ context.Context, _ string, messages []*message.Message) (string, error) {

	if o.contextErr != nil {
		return "", o.contextErr
	}
	o.contexts = append(o.contexts, cloneMessages(messages))
	return "workspace://context.jsonl", nil
}

func (o *coverageOffloader) OffloadToolResult(_ context.Context, _ string, result *message.ToolResultBlock) (string, error) {

	if o.toolErr != nil {
		return "", o.toolErr
	}
	o.toolResults = append(o.toolResults, result.Clone().(*message.ToolResultBlock))
	return "workspace://tool-result.txt", nil
}

func (o *coverageOffloader) OffloadDataBlock(_ context.Context, block *message.DataBlock) (*message.DataBlock, error) {

	if o.dataErr != nil {
		return nil, o.dataErr
	}
	o.dataBlocks = append(o.dataBlocks, block.Clone().(*message.DataBlock))
	return message.NewDataBlock(message.NewURLSource("workspace://"+block.ID, block.Source.(*message.Base64Source).MediaType), message.WithDataBlockID(block.ID)), nil
}

type coverageToolProvider struct {
	name      string
	schemas   []ToolSchema
	schemaErr error
	tools     map[string]bool
	callCount int
}

func (p *coverageToolProvider) ToolSchemas(...string) ([]ToolSchema, error) {

	if p.schemaErr != nil {
		return nil, p.schemaErr
	}
	return append([]ToolSchema(nil), p.schemas...), nil
}

func (p *coverageToolProvider) FindTool(name string, _ ...string) (Tool, bool) {

	if p.tools[name] {
		return coverageAgentTool{name: name}, true
	}
	return nil, false
}

func (p *coverageToolProvider) CallTool(context.Context, *message.ToolCallBlock, *AgentState) (<-chan ToolChunk, error) {

	p.callCount++
	chunks := make(chan ToolChunk, 1)
	chunks <- *astool.NewToolChunk(message.ContentBlockList{message.NewTextBlock(p.name)}, astool.WithToolChunkState(message.ToolResultSuccess))
	close(chunks)
	return chunks, nil
}

type coverageAgentTool struct {
	name string
}

func (t coverageAgentTool) Name() string { return t.name }

func (coverageAgentTool) Description() string { return "coverage tool" }

func (coverageAgentTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }

func (coverageAgentTool) IsConcurrencySafe() bool { return true }

func (coverageAgentTool) IsReadOnly() bool { return true }

func (coverageAgentTool) IsExternalTool() bool { return false }

func (coverageAgentTool) IsStateInjected() bool { return false }

func (coverageAgentTool) IsMCP() bool { return false }

func (coverageAgentTool) MCPName() string { return "" }

func (coverageAgentTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {

	return &permission.Decision{Behavior: permission.BehaviorAllow}, nil
}

func (coverageAgentTool) MatchRule(string, map[string]any) bool { return true }

func (coverageAgentTool) GenerateSuggestions(map[string]any) []permission.Rule { return nil }

func (coverageAgentTool) Execute(context.Context, map[string]any, *AgentState) (<-chan ToolChunk, error) {

	chunks := make(chan ToolChunk, 1)
	chunks <- *astool.NewToolChunk(message.ContentBlockList{message.NewTextBlock("ok")}, astool.WithToolChunkState(message.ToolResultSuccess))
	close(chunks)
	return chunks, nil
}

type actingProvider struct {
	tools     map[string]actingTool
	chunks    map[string][]ToolChunk
	callErr   error
	nilStream bool
}

func (p *actingProvider) ToolSchemas(...string) ([]ToolSchema, error) { return nil, nil }

func (p *actingProvider) FindTool(name string, _ ...string) (Tool, bool) {

	tool, ok := p.tools[name]
	return tool, ok
}

func (p *actingProvider) CallTool(_ context.Context, toolCall *message.ToolCallBlock, _ *AgentState) (<-chan ToolChunk, error) {

	if p.callErr != nil {
		return nil, p.callErr
	}
	if p.nilStream {
		return nil, nil
	}
	toolChunks := p.chunks[toolCall.Name]
	if len(toolChunks) == 0 {
		toolChunks = []ToolChunk{
			*astool.NewToolChunk(message.ContentBlockList{message.NewTextBlock("ok")}, astool.WithToolChunkState(message.ToolResultSuccess)),
		}
	}
	chunks := make(chan ToolChunk, len(toolChunks))
	for _, chunk := range toolChunks {
		chunks <- chunk
	}
	close(chunks)
	return chunks, nil
}

type actingTool struct {
	name     string
	external bool
	serial   bool
	decision *permission.Decision
}

func (t actingTool) Name() string { return t.name }

func (actingTool) Description() string { return "acting tool" }

func (actingTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }

func (t actingTool) IsConcurrencySafe() bool { return !t.serial }

func (actingTool) IsReadOnly() bool { return true }

func (t actingTool) IsExternalTool() bool { return t.external }

func (actingTool) IsStateInjected() bool { return false }

func (actingTool) IsMCP() bool { return false }

func (actingTool) MCPName() string { return "" }

func (t actingTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {

	if t.decision != nil {
		cp := *t.decision
		return &cp, nil
	}
	return &permission.Decision{Behavior: permission.BehaviorAllow}, nil
}

func (actingTool) MatchRule(string, map[string]any) bool { return true }

func (actingTool) GenerateSuggestions(map[string]any) []permission.Rule { return nil }

func (actingTool) Execute(context.Context, map[string]any, *AgentState) (<-chan ToolChunk, error) {

	chunks := make(chan ToolChunk, 1)
	chunks <- *astool.NewToolChunk(message.ContentBlockList{message.NewTextBlock("ok")}, astool.WithToolChunkState(message.ToolResultSuccess))
	close(chunks)
	return chunks, nil
}
