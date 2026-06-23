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
	"fmt"
	"sync/atomic"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

type modelDeltaBlockKind int

const (
	textDeltaBlock modelDeltaBlockKind = iota
	thinkingDeltaBlock
)

// runReasoning is the entry point for the Reasoning phase in the ReAct loop,
// responsible for invoking the LLM to obtain reasoning content.
func (a *Agent) runReasoning(ctx context.Context, assistant *message.Message, emit func(message.Event) error) error {

	// Non hook, directly exec.
	if len(a.reasoningHooks) == 0 {
		return a.reason(ctx, assistant, func(event message.Event) error {
			return a.emitAndApply(assistant, event, emit)
		})
	}

	hookCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var finalCalled atomic.Bool
	errs := make(chan error, 1)
	final := func(ctx context.Context) (<-chan message.Event, error) {
		finalCalled.Store(true)
		events := make(chan message.Event)
		go func() {
			errs <- a.reason(
				ctx,
				assistant,
				func(event message.Event) error {
					select {
					case events <- event:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			close(events)
		}()

		return events, nil
	}

	events, err := a.applyReasoningHooks(hookCtx, HookInput{}, final)
	if err != nil {
		return err
	}
	for event := range events {
		if err := a.emitAndApply(assistant, event, emit); err != nil {
			cancel()
			return err
		}
	}
	if finalCalled.Load() {
		select {
		case err := <-errs:
			return err
		default:
			return nil
		}
	}

	return nil
}

// reason is the actual execution function for the Reasoning phase — it interacts
// directly with the LLM. The flow follows the standard four steps:
//
//	prepare → invoke → output → finalize.
func (a *Agent) reason(ctx context.Context, _ *message.Message, emit func(message.Event) error) error {

	if err := emit(message.NewModelCallStartEvent(a.state.ReplyID, a.model.Name())); err != nil {
		return err
	}

	request, err := a.prepareModelInput(ctx)
	if err != nil {
		return err
	}

	responses, err := a.callModel(ctx, request)
	if err != nil {
		return err
	}

	response, err := a.emitChatResponseStream(responses, emit)
	if err != nil {
		return err
	}
	if response == nil {
		return fmt.Errorf("agentscope: model returned nil response")
	}

	inputTokens, outputTokens := 0, 0
	if response.Usage != nil {
		inputTokens = response.Usage.InputTokens
		outputTokens = response.Usage.OutputTokens
	}

	return emit(message.NewModelCallEndEvent(a.state.ReplyID, inputTokens, outputTokens))
}

func (a *Agent) prepareModelInput(ctx context.Context) (CallRequest, error) {

	systemPrompt, err := a.buildSystemPrompt(ctx)
	if err != nil {
		return CallRequest{}, err
	}

	systemMsg, err := message.NewSystemMessage("system", systemPrompt)
	if err != nil {
		return CallRequest{}, err
	}

	messages := []*message.Message{systemMsg}
	if summary := a.summaryMessage(); summary != nil {
		messages = append(messages, summary)
	}

	for _, msg := range a.state.Context {
		if msg != nil {
			messages = append(messages, msg.Clone())
		}
	}

	tools, err := a.effectiveToolProvider().ToolSchemas(a.activeGroups()...)
	if err != nil {
		return CallRequest{}, err
	}

	return CallRequest{
		Messages:   messages,
		Tools:      tools,
		ToolChoice: a.modelConfig.ToolChoice.Clone(),
	}, nil
}

func (a *Agent) buildSystemPrompt(ctx context.Context) (string, error) {
	return ApplySystemPromptHooks(ctx, a, a.systemPrompt, a.systemPromptHooks...)
}

func (a *Agent) summaryMessage() *message.Message {
	if a.state == nil {
		return nil
	}
	switch {
	case a.state.Summary.Text != "":
		msg, err := message.NewUserMessage("user", a.state.Summary.Text)
		if err != nil {
			return nil
		}
		return msg
	case len(a.state.Summary.Blocks) > 0:
		msg, err := message.NewUserMessage("user", a.state.Summary.Blocks)
		if err != nil {
			return nil
		}
		return msg
	default:
		return nil
	}
}

func (a *Agent) callModel(ctx context.Context, request CallRequest) (<-chan ChatResponse, error) {

	models := []ChatModel{a.model}
	if a.modelConfig.FallbackModel != nil {
		models = append(models, a.modelConfig.FallbackModel)
	}
	var lastErr error
	for _, model := range models {
		for attempt := 0; attempt < a.modelConfig.MaxRetries; attempt++ {
			currentModel := model
			currentRequest := request.Clone()
			input := HookInput{"model": currentModel, "request": currentRequest.Clone()}
			responses, err := a.applyModelCallHooks(ctx, input, func(ctx context.Context) (<-chan ChatResponse, error) {
				if updatedModel, ok := input["model"].(ChatModel); ok && updatedModel != nil {
					currentModel = updatedModel
				}
				switch updatedRequest := input["request"].(type) {
				case CallRequest:
					currentRequest = updatedRequest.Clone()
				case *CallRequest:
					if updatedRequest != nil {
						currentRequest = updatedRequest.Clone()
					}
				}
				currentRequest.Stream = true
				return currentModel.Stream(ctx, currentRequest)
			})
			if err == nil {
				if responses == nil {
					return nil, fmt.Errorf("agentscope: model returned nil response stream")
				}
				return responses, nil
			}
			lastErr = err
		}
	}
	return nil, lastErr
}

// State tracker used during LLM streaming response processing, responsible for
// assembling fragmented LLM chunks into a structured event stream within
// emitChatResponseStream.
type modelStreamState struct {

	// Text content id, used to correlate streaming text deltas and determine when text blocks start and end.
	textID string
	// Thinking content id, used to correlate streaming thinking deltas and determine when thinking blocks start and end.
	thinkingID string

	// Tracks whether a start event has been emitted for each tool call ID
	// (prevents duplicate start events).
	toolActive map[string]bool
	// Records the order in which tool calls appear, ensuring output order matches
	// the LLM production order.
	toolOrder []string

	// Whether a text block has been processed
	seenText bool
	// Whether a thinking block has been processed
	seenThinking bool
	// Which tool call IDs have been seen
	seenTools map[string]bool
	// Which data block IDs have been seen
	seenData map[string]bool

	// Whether any event has been emitted, used to determine edge-case handling
	// such as whether to send an empty reply.
	emitted bool
}

type modelStreamChunkState struct {

	//  Which tool call IDs have appeared.
	currentTools map[string]bool
	// Whether the content includes text.
	hasText bool
	// Whether the think content includes text.
	hasThinking bool
}

func newModelStreamState() *modelStreamState {

	return &modelStreamState{
		toolActive: map[string]bool{},
		seenTools:  map[string]bool{},
		seenData:   map[string]bool{},
	}
}

func (a *Agent) emitChatResponseStream(responses <-chan ChatResponse, emit func(message.Event) error) (*ChatResponse, error) {
	state := newModelStreamState()
	var completed *ChatResponse
	for response := range responses {
		chunk := response.Clone()
		if chunk == nil {
			continue
		}
		if chunk.Error != nil {
			return nil, chunk.Error
		}
		if chunk.IsLast {
			completed = chunk
			continue
		}
		if err := a.emitChatResponseChunk(chunk, state, emit); err != nil {
			return nil, err
		}
	}
	if completed == nil {
		return nil, fmt.Errorf("agentscope: model stream did not return a final response")
	}
	if !state.emitted {
		if err := a.emitChatResponse(completed, emit); err != nil {
			return nil, err
		}
		return completed, nil
	}
	if err := a.emitFinalUnseenBlocks(completed, state, emit); err != nil {
		return nil, err
	}
	if err := a.endOpenStreamBlocks(state, emit); err != nil {
		return nil, err
	}
	return completed, nil
}

func (a *Agent) emitChatResponseChunk(response *ChatResponse, state *modelStreamState, emit func(message.Event) error) error {
	chunkState := modelStreamChunkState{currentTools: map[string]bool{}}
	for _, block := range response.Content {
		if err := a.emitStreamChunkBlock(block, state, &chunkState, emit); err != nil {
			return err
		}
	}
	return a.endInactiveChunkBlocks(state, chunkState, emit)
}

func (a *Agent) emitStreamChunkBlock(block message.ContentBlock, state *modelStreamState, chunkState *modelStreamChunkState, emit func(message.Event) error) error {

	switch typed := block.(type) {
	case *message.TextBlock:
		chunkState.hasText = true
		return a.emitStreamDeltaBlock(textDeltaBlock, typed.ID, typed.Text, state, emit)
	case *message.ThinkingBlock:
		chunkState.hasThinking = true
		return a.emitStreamDeltaBlock(thinkingDeltaBlock, typed.ID, typed.Thinking, state, emit)
	case *message.ToolCallBlock:
		chunkState.currentTools[typed.ID] = true
		return a.emitStreamToolCallBlock(typed, state, emit)
	case *message.DataBlock:
		state.seenData[typed.ID] = true
		state.emitted = true
		return a.emitDataBlock(typed, emit)
	default:
		return nil
	}
}

func (a *Agent) emitStreamDeltaBlock(kind modelDeltaBlockKind, blockID, delta string, state *modelStreamState, emit func(message.Event) error) error {
	currentID, seen := state.deltaBlockPointers(kind)
	if *currentID == "" {
		*currentID = blockID
		start, _, _ := a.deltaBlockEvents(kind, *currentID, delta)
		if err := emit(start); err != nil {
			return err
		}
	}
	*seen = true
	state.emitted = true
	if delta == "" {
		return nil
	}
	_, partial, _ := a.deltaBlockEvents(kind, *currentID, delta)
	return emit(partial)
}

func (s *modelStreamState) deltaBlockPointers(kind modelDeltaBlockKind) (*string, *bool) {
	if kind == thinkingDeltaBlock {
		return &s.thinkingID, &s.seenThinking
	}
	return &s.textID, &s.seenText
}

func (a *Agent) emitStreamToolCallBlock(block *message.ToolCallBlock, state *modelStreamState, emit func(message.Event) error) error {
	if !state.seenTools[block.ID] {
		state.toolActive[block.ID] = true
		state.toolOrder = append(state.toolOrder, block.ID)
		state.seenTools[block.ID] = true
		if err := emit(message.NewToolCallStartEvent(a.state.ReplyID, block.ID, block.Name)); err != nil {
			return err
		}
	}
	state.emitted = true
	if block.Input == "" {
		return nil
	}
	return emit(message.NewToolCallDeltaEvent(a.state.ReplyID, block.ID, block.Input))
}

func (a *Agent) endInactiveChunkBlocks(state *modelStreamState, chunkState modelStreamChunkState, emit func(message.Event) error) error {
	if !chunkState.hasText && state.textID != "" {
		if err := emit(message.NewTextBlockEndEvent(a.state.ReplyID, state.textID)); err != nil {
			return err
		}
		state.textID = ""
	}
	if !chunkState.hasThinking && state.thinkingID != "" {
		if err := emit(message.NewThinkingBlockEndEvent(a.state.ReplyID, state.thinkingID)); err != nil {
			return err
		}
		state.thinkingID = ""
	}
	for _, toolID := range state.toolOrder {
		if state.toolActive[toolID] && !chunkState.currentTools[toolID] {
			if err := emit(message.NewToolCallEndEvent(a.state.ReplyID, toolID)); err != nil {
				return err
			}
			state.toolActive[toolID] = false
		}
	}
	return nil
}

func (a *Agent) emitFinalUnseenBlocks(response *ChatResponse, state *modelStreamState, emit func(message.Event) error) error {
	for _, block := range response.Content {
		switch typed := block.(type) {
		case *message.TextBlock:
			if !state.seenText {
				if err := a.emitDeltaBlock(textDeltaBlock, typed.ID, typed.Text, emit); err != nil {
					return err
				}
			}
		case *message.ThinkingBlock:
			if !state.seenThinking {
				if err := a.emitDeltaBlock(thinkingDeltaBlock, typed.ID, typed.Thinking, emit); err != nil {
					return err
				}
			}
		case *message.ToolCallBlock:
			if !state.seenTools[typed.ID] {
				if err := a.emitToolCallBlock(typed, emit); err != nil {
					return err
				}
			}
		case *message.DataBlock:
			if !state.seenData[typed.ID] {
				if err := a.emitDataBlock(typed, emit); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (a *Agent) endOpenStreamBlocks(state *modelStreamState, emit func(message.Event) error) error {
	if state.textID != "" {
		if err := emit(message.NewTextBlockEndEvent(a.state.ReplyID, state.textID)); err != nil {
			return err
		}
	}
	if state.thinkingID != "" {
		if err := emit(message.NewThinkingBlockEndEvent(a.state.ReplyID, state.thinkingID)); err != nil {
			return err
		}
	}
	for _, toolID := range state.toolOrder {
		if state.toolActive[toolID] {
			if err := emit(message.NewToolCallEndEvent(a.state.ReplyID, toolID)); err != nil {
				return err
			}
			state.toolActive[toolID] = false
		}
	}
	return nil
}

func (a *Agent) emitChatResponse(response *ChatResponse, emit func(message.Event) error) error {
	for _, block := range response.Content {
		switch typed := block.(type) {
		case *message.TextBlock:
			if err := a.emitDeltaBlock(textDeltaBlock, typed.ID, typed.Text, emit); err != nil {
				return err
			}
		case *message.ThinkingBlock:
			if err := a.emitDeltaBlock(thinkingDeltaBlock, typed.ID, typed.Thinking, emit); err != nil {
				return err
			}
		case *message.ToolCallBlock:
			if err := a.emitToolCallBlock(typed, emit); err != nil {
				return err
			}
		case *message.DataBlock:
			if err := a.emitDataBlock(typed, emit); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Agent) emitDeltaBlock(kind modelDeltaBlockKind, blockID, delta string, emit func(message.Event) error) error {
	start, partial, end := a.deltaBlockEvents(kind, blockID, delta)
	if err := emit(start); err != nil {
		return err
	}
	if delta != "" {
		if err := emit(partial); err != nil {
			return err
		}
	}
	return emit(end)
}

func (a *Agent) deltaBlockEvents(kind modelDeltaBlockKind, blockID, delta string) (message.Event, message.Event, message.Event) {
	switch kind {
	case thinkingDeltaBlock:
		return message.NewThinkingBlockStartEvent(a.state.ReplyID, blockID),
			message.NewThinkingBlockDeltaEvent(a.state.ReplyID, blockID, delta),
			message.NewThinkingBlockEndEvent(a.state.ReplyID, blockID)
	default:
		return message.NewTextBlockStartEvent(a.state.ReplyID, blockID),
			message.NewTextBlockDeltaEvent(a.state.ReplyID, blockID, delta),
			message.NewTextBlockEndEvent(a.state.ReplyID, blockID)
	}
}

func (a *Agent) emitToolCallBlock(block *message.ToolCallBlock, emit func(message.Event) error) error {
	if err := emit(message.NewToolCallStartEvent(a.state.ReplyID, block.ID, block.Name)); err != nil {
		return err
	}
	if block.Input != "" {
		if err := emit(message.NewToolCallDeltaEvent(a.state.ReplyID, block.ID, block.Input)); err != nil {
			return err
		}
	}
	return emit(message.NewToolCallEndEvent(a.state.ReplyID, block.ID))
}

func (a *Agent) emitDataBlock(block *message.DataBlock, emit func(message.Event) error) error {
	switch source := block.Source.(type) {
	case *message.Base64Source:
		if err := emit(message.NewDataBlockStartEvent(a.state.ReplyID, block.ID, source.MediaType)); err != nil {
			return err
		}
		if source.Data != "" {
			if err := emit(message.NewDataBlockDeltaEvent(a.state.ReplyID, block.ID, source.Data, source.MediaType)); err != nil {
				return err
			}
		}
		return emit(message.NewDataBlockEndEvent(a.state.ReplyID, block.ID))
	case *message.URLSource:
		if err := emit(message.NewDataBlockStartEvent(a.state.ReplyID, block.ID, source.MediaType)); err != nil {
			return err
		}
		if err := emit(message.NewDataBlockDeltaEvent(a.state.ReplyID, block.ID, "", source.MediaType)); err != nil {
			return err
		}
		return emit(message.NewDataBlockEndEvent(a.state.ReplyID, block.ID))
	default:
		return nil
	}
}

func (a *Agent) activeGroups() []string {
	if a.state == nil || a.state.ToolContext == nil {
		return nil
	}
	return append([]string(nil), a.state.ToolContext.ActivatedGroups...)
}
