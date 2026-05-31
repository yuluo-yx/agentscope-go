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

	"github.com/yuluo-yx/agentscope-go/message"
)

type modelDeltaBlockKind int

const (
	textDeltaBlock modelDeltaBlockKind = iota
	thinkingDeltaBlock
)

func (a *Agent) runReasoning(ctx context.Context, assistant *message.Message, emit func(message.Event) error) error {
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
			errs <- a.reason(ctx, assistant, func(event message.Event) error {
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

func (a *Agent) reason(ctx context.Context, _ *message.Message, emit func(message.Event) error) error {
	if err := emit(message.NewModelCallStartEvent(a.state.ReplyID, a.model.Name())); err != nil {
		return err
	}
	request, err := a.prepareModelInput(ctx)
	if err != nil {
		return err
	}
	response, err := a.callModel(ctx, request)
	if err != nil {
		return err
	}
	if response == nil {
		return fmt.Errorf("agentscope: model returned nil response")
	}
	if err := a.emitChatResponse(response, emit); err != nil {
		return err
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
	tools, err := a.toolkit.ToolSchemas(a.activeGroups()...)
	if err != nil {
		return CallRequest{}, err
	}
	return CallRequest{Messages: messages, Tools: tools}, nil
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

func (a *Agent) callModel(ctx context.Context, request CallRequest) (*ChatResponse, error) {
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
			response, err := a.applyModelCallHooks(ctx, input, func(ctx context.Context) (*ChatResponse, error) {
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
				return currentModel.Call(ctx, currentRequest)
			})
			if err == nil {
				return response, nil
			}
			lastErr = err
		}
	}
	return nil, lastErr
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
