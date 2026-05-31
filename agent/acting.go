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
	"encoding/json"
	"fmt"
	"sync"

	"github.com/yuluo-yx/agentscope-go/internal/jsonutil"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
)

type toolExecutionPlan struct {
	tool     Tool
	toolCall *message.ToolCallBlock
}

type toolBatchRunner struct {
	agent     *Agent
	ctx       context.Context
	assistant *message.Message
	emit      func(message.Event) error
	batch     []*toolExecutionPlan
}

func (a *Agent) runActing(ctx context.Context, assistant *message.Message, toolCalls []*message.ToolCallBlock, emit func(message.Event) error) (bool, error) {
	if len(toolCalls) == 1 {
		return a.executeToolCall(ctx, assistant, toolCalls[0], emit)
	}
	runner := &toolBatchRunner{
		agent:     a,
		ctx:       ctx,
		assistant: assistant,
		emit:      emit,
		batch:     make([]*toolExecutionPlan, 0, len(toolCalls)),
	}
	for _, toolCall := range toolCalls {
		waiting, err := runner.handle(toolCall)
		if err != nil || waiting {
			return waiting, err
		}
	}
	return false, runner.flush()
}

func (a *Agent) executeToolCall(ctx context.Context, assistant *message.Message, toolCall *message.ToolCallBlock, emit func(message.Event) error) (bool, error) {
	plan, waiting, err := a.prepareToolCall(ctx, assistant, toolCall, emit)
	if err != nil || waiting || plan == nil {
		return waiting, err
	}
	if err := a.executeLocalTool(ctx, assistant, plan.toolCall, emit); err != nil {
		return false, err
	}
	plan.toolCall.State = message.ToolCallFinished
	return false, nil
}

func (a *Agent) prepareToolCall(ctx context.Context, assistant *message.Message, toolCall *message.ToolCallBlock, emit func(message.Event) error) (*toolExecutionPlan, bool, error) {
	if toolCall == nil {
		return nil, false, nil
	}
	tool, ok := a.toolkit.FindTool(toolCall.Name, a.activeGroups()...)
	if !ok {
		return nil, false, a.emitToolError(assistant, toolCall, fmt.Sprintf("tool %s not found", toolCall.Name), message.ToolResultError, emit)
	}
	input, err := jsonutil.LoadObject(toolCall.Input)
	if err != nil {
		return nil, false, a.emitToolError(assistant, toolCall, err.Error(), message.ToolResultError, emit)
	}

	decision := &permission.Decision{Behavior: permission.BehaviorAllow, Message: "Already allowed by user confirmation."}
	if toolCall.State != message.ToolCallAllowed {
		decision, err = permission.NewEngine(a.state.PermissionContext).CheckPermission(ctx, tool, input)
		if err != nil {
			return nil, false, err
		}
	}
	if decision.UpdatedInput != nil {
		input = decision.UpdatedInput
		updatedInput, err := json.Marshal(input)
		if err != nil {
			return nil, false, err
		}
		toolCall.Input = string(updatedInput)
	}
	switch decision.Behavior {
	case permission.BehaviorAsk, permission.BehaviorPassthrough:
		toolCall.State = message.ToolCallAsking
		toolCall.SuggestedRules = append([]permission.Rule(nil), decision.SuggestedRules...)
		return nil, true, a.emitAndApply(assistant, message.NewRequireUserConfirmEvent(a.state.ReplyID, []*message.ToolCallBlock{toolCall.Clone().(*message.ToolCallBlock)}), emit)
	case permission.BehaviorDeny:
		toolCall.State = message.ToolCallFinished
		return nil, false, a.emitToolError(assistant, toolCall, decision.Message, message.ToolResultDenied, emit)
	case permission.BehaviorAllow:
		toolCall.State = message.ToolCallAllowed
		if err := a.emitAndApply(assistant, message.NewToolResultStartEvent(a.state.ReplyID, toolCall.ID, toolCall.Name), emit); err != nil {
			return nil, false, err
		}
		if tool.IsExternalTool() {
			toolCall.State = message.ToolCallSubmitted
			return nil, true, a.emitAndApply(assistant, message.NewRequireExternalExecutionEvent(a.state.ReplyID, []*message.ToolCallBlock{toolCall.Clone().(*message.ToolCallBlock)}), emit)
		}
		return &toolExecutionPlan{tool: tool, toolCall: toolCall}, false, nil
	default:
		return nil, false, fmt.Errorf("agentscope: unsupported permission behavior %q", decision.Behavior)
	}
}

func (r *toolBatchRunner) handle(toolCall *message.ToolCallBlock) (bool, error) {
	if err := r.flushBeforeIfNeeded(toolCall); err != nil {
		return false, err
	}
	plan, waiting, err := r.agent.prepareToolCall(r.ctx, r.assistant, toolCall, r.emit)
	if err != nil {
		return false, err
	}
	if waiting {
		return true, r.flush()
	}
	if plan == nil {
		return false, nil
	}
	if !plan.tool.IsConcurrencySafe() {
		if err := r.flush(); err != nil {
			return false, err
		}
		if err := r.agent.executeLocalTool(r.ctx, r.assistant, plan.toolCall, r.emit); err != nil {
			return false, err
		}
		plan.toolCall.State = message.ToolCallFinished
		return false, nil
	}
	r.batch = append(r.batch, plan)
	return false, nil
}

func (r *toolBatchRunner) flushBeforeIfNeeded(toolCall *message.ToolCallBlock) error {
	if len(r.batch) == 0 {
		return nil
	}
	tool, ok := r.agent.toolkit.FindTool(toolCall.Name, r.agent.activeGroups()...)
	if ok && !tool.IsExternalTool() && tool.IsConcurrencySafe() {
		return nil
	}
	return r.flush()
}

func (r *toolBatchRunner) flush() error {
	if len(r.batch) == 0 {
		return nil
	}
	if err := r.agent.executeLocalToolBatch(r.ctx, r.assistant, r.batch, r.emit); err != nil {
		return err
	}
	for _, plan := range r.batch {
		plan.toolCall.State = message.ToolCallFinished
	}
	r.batch = r.batch[:0]
	return nil
}

func (a *Agent) executeLocalTool(ctx context.Context, assistant *message.Message, toolCall *message.ToolCallBlock, emit func(message.Event) error) error {
	chunks, err := a.applyActingHooks(ctx, HookInput{"tool_call": toolCall}, func(ctx context.Context) (<-chan ToolChunk, error) {
		return a.toolkit.CallTool(ctx, toolCall, a.state)
	})
	if err != nil {
		if emitErr := a.emitToolExecutionError(assistant, toolCall, err.Error(), message.ToolResultError, emit); emitErr != nil {
			return emitErr
		}
		return nil
	}
	if chunks == nil {
		return a.emitToolExecutionError(assistant, toolCall, "tool returned nil chunk stream", message.ToolResultError, emit)
	}
	finalState := message.ToolResultSuccess
	for chunk := range chunks {
		if chunk.State != "" {
			finalState = chunk.State
		}
		if err := a.emitToolChunk(assistant, toolCall, &chunk, emit); err != nil {
			return err
		}
	}
	return a.emitAndApply(assistant, message.NewToolResultEndEvent(a.state.ReplyID, toolCall.ID, finalState), emit)
}

func (a *Agent) executeLocalToolBatch(ctx context.Context, assistant *message.Message, plans []*toolExecutionPlan, emit func(message.Event) error) error {
	type batchResult struct {
		chunks []ToolChunk
		err    error
	}
	results := make([]batchResult, len(plans))
	var wg sync.WaitGroup
	for index, plan := range plans {
		wg.Add(1)
		go func(index int, plan *toolExecutionPlan) {
			defer wg.Done()
			chunks, err := a.collectLocalToolChunks(ctx, plan.toolCall)
			results[index] = batchResult{chunks: chunks, err: err}
		}(index, plan)
	}
	wg.Wait()

	for index, plan := range plans {
		result := results[index]
		if result.err != nil {
			if err := a.emitToolExecutionError(assistant, plan.toolCall, result.err.Error(), message.ToolResultError, emit); err != nil {
				return err
			}
			continue
		}
		finalState := message.ToolResultSuccess
		for _, chunk := range result.chunks {
			if chunk.State != "" {
				finalState = chunk.State
			}
			if err := a.emitToolChunk(assistant, plan.toolCall, &chunk, emit); err != nil {
				return err
			}
		}
		if err := a.emitAndApply(assistant, message.NewToolResultEndEvent(a.state.ReplyID, plan.toolCall.ID, finalState), emit); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) collectLocalToolChunks(ctx context.Context, toolCall *message.ToolCallBlock) ([]ToolChunk, error) {
	chunks, err := a.applyActingHooks(ctx, HookInput{"tool_call": toolCall}, func(ctx context.Context) (<-chan ToolChunk, error) {
		return a.toolkit.CallTool(ctx, toolCall, a.state)
	})
	if err != nil {
		return nil, err
	}
	if chunks == nil {
		return nil, fmt.Errorf("tool returned nil chunk stream")
	}
	collected := []ToolChunk{}
	for chunk := range chunks {
		cloned := chunk.Clone()
		if cloned == nil {
			continue
		}
		collected = append(collected, *cloned)
	}
	return collected, nil
}

func (a *Agent) emitToolChunk(assistant *message.Message, toolCall *message.ToolCallBlock, chunk *ToolChunk, emit func(message.Event) error) error {
	if chunk == nil {
		return nil
	}
	for _, block := range chunk.Content {
		switch typed := block.(type) {
		case *message.TextBlock:
			if err := a.emitAndApply(assistant, message.NewToolResultTextDeltaEvent(a.state.ReplyID, toolCall.ID, typed.Text), emit); err != nil {
				return err
			}
		case *message.DataBlock:
			if err := a.emitToolResultData(assistant, toolCall, typed, emit); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Agent) emitToolResultData(assistant *message.Message, toolCall *message.ToolCallBlock, block *message.DataBlock, emit func(message.Event) error) error {
	switch source := block.Source.(type) {
	case *message.Base64Source:
		return a.emitAndApply(assistant, message.NewToolResultDataDeltaEvent(a.state.ReplyID, toolCall.ID, block.ID, source.MediaType, source.Data, ""), emit)
	case *message.URLSource:
		return a.emitAndApply(assistant, message.NewToolResultDataDeltaEvent(a.state.ReplyID, toolCall.ID, block.ID, source.MediaType, "", source.URL), emit)
	default:
		return nil
	}
}

func (a *Agent) emitToolError(assistant *message.Message, toolCall *message.ToolCallBlock, text string, state message.ToolResultState, emit func(message.Event) error) error {
	if err := a.emitAndApply(assistant, message.NewToolResultStartEvent(a.state.ReplyID, toolCall.ID, toolCall.Name), emit); err != nil {
		return err
	}
	return a.emitToolExecutionError(assistant, toolCall, text, state, emit)
}

func (a *Agent) emitToolExecutionError(assistant *message.Message, toolCall *message.ToolCallBlock, text string, state message.ToolResultState, emit func(message.Event) error) error {
	if text != "" {
		if err := a.emitAndApply(assistant, message.NewToolResultTextDeltaEvent(a.state.ReplyID, toolCall.ID, text), emit); err != nil {
			return err
		}
	}
	return a.emitAndApply(assistant, message.NewToolResultEndEvent(a.state.ReplyID, toolCall.ID, state), emit)
}

func pendingToolCalls(msg *message.Message) []*message.ToolCallBlock {
	if msg == nil {
		return nil
	}
	resultIDs := map[string]bool{}
	for _, block := range msg.GetContentBlocks("tool_result") {
		resultIDs[block.BlockID()] = true
	}
	var pending []*message.ToolCallBlock
	for _, block := range msg.GetContentBlocks("tool_call") {
		toolCall, ok := block.(*message.ToolCallBlock)
		if !ok || resultIDs[toolCall.ID] {
			continue
		}
		switch toolCall.State {
		case message.ToolCallPending, message.ToolCallAllowed:
			pending = append(pending, toolCall)
		}
	}
	return pending
}
