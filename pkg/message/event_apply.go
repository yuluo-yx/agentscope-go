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

package message

import (
	"fmt"

	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// ApplyEvent applies a streaming event to the message using Python-compatible accumulation semantics.
func (m *Message) ApplyEvent(event Event) error {
	if event == nil {
		return fmt.Errorf("message: nil event")
	}
	if event.ReplyID() != m.ID {
		return nil
	}

	if m.applyLifecycleEvent(event) {
		return nil
	}
	if m.applyContentBlockEvent(event) {
		return nil
	}
	if m.applyToolCallEvent(event) {
		return nil
	}
	if m.applyToolResultEvent(event) {
		return nil
	}
	if m.applyPermissionEvent(event) {
		return nil
	}

	return fmt.Errorf("message: unsupported event %T", event)
}

func (m *Message) applyLifecycleEvent(event Event) bool {
	switch e := event.(type) {
	case *ReplyEndEvent:
		finishedAt := e.GetTime()
		m.FinishedAt = &finishedAt
	case *ModelCallEndEvent:
		if m.Usage == nil {
			m.Usage = &Usage{InputTokens: e.InputTokens, OutputTokens: e.OutputTokens}
		} else {
			m.Usage.InputTokens += e.InputTokens
			m.Usage.OutputTokens += e.OutputTokens
		}
	case *ReplyStartEvent, *ModelCallStartEvent, *ExceedMaxItersEvent:
	default:
		return false
	}

	return true
}

func (m *Message) applyContentBlockEvent(event Event) bool {
	switch e := event.(type) {
	case *TextBlockStartEvent:
		m.Content = append(m.Content, &TextBlock{Type: "text", ID: e.BlockID, Text: ""})
	case *TextBlockDeltaEvent:
		if block, ok := m.FindBlock("text", e.BlockID).(*TextBlock); ok {
			block.Text += e.Delta
		}
	case *TextBlockEndEvent:
	case *DataBlockStartEvent:
		m.Content = append(m.Content, &DataBlock{
			Type:   "data",
			ID:     e.BlockID,
			Source: NewBase64Source("", e.MediaType),
		})
	case *DataBlockDeltaEvent:
		if block, ok := m.FindBlock("data", e.BlockID).(*DataBlock); ok {
			if source, ok := block.Source.(*Base64Source); ok {
				source.Data += e.Data
			}
		}
	case *DataBlockEndEvent:
	case *ThinkingBlockStartEvent:
		m.Content = append(m.Content, &ThinkingBlock{Type: "thinking", ID: e.BlockID, Thinking: ""})
	case *ThinkingBlockDeltaEvent:
		if block, ok := m.FindBlock("thinking", e.BlockID).(*ThinkingBlock); ok {
			block.Thinking += e.Delta
		}
	case *ThinkingBlockEndEvent:
	case *HintBlockEvent:
		hint := hintBlockFromEvent(e)
		for index, block := range m.Content {
			if block == nil || block.BlockType() != hint.BlockType() || block.BlockID() != hint.BlockID() {
				continue
			}
			m.Content[index] = hint
			return true
		}
		m.Content = append(m.Content, hint)
	default:
		return false
	}

	return true
}

func (m *Message) applyToolCallEvent(event Event) bool {
	switch e := event.(type) {
	case *ToolCallStartEvent:
		m.Content = append(m.Content, NewToolCallBlock(e.ToolCallID, e.ToolCallName, ""))
	case *ToolCallDeltaEvent:
		if block, ok := m.FindBlock("tool_call", e.ToolCallID).(*ToolCallBlock); ok {
			block.Input += e.Delta
		}
	case *ToolCallEndEvent:
	default:
		return false
	}

	return true
}

func (m *Message) applyToolResultEvent(event Event) bool {
	switch e := event.(type) {
	case *ToolResultStartEvent:
		m.Content = append(m.Content, NewToolResultBlock(
			e.ToolCallID,
			e.ToolCallName,
			ToolResultOutput{Blocks: ContentBlockList{}},
			ToolResultRunning,
		))
	case *ToolResultTextDeltaEvent:
		if block, ok := m.FindBlock("tool_result", e.ToolCallID).(*ToolResultBlock); ok {
			appendToolResultText(block, e.Delta)
		}
	case *ToolResultDataDeltaEvent:
		if block, ok := m.FindBlock("tool_result", e.ToolCallID).(*ToolResultBlock); ok {
			appendToolResultData(block, e)
		}
	case *ToolResultEndEvent:
		if block, ok := m.FindBlock("tool_result", e.ToolCallID).(*ToolResultBlock); ok {
			block.State = e.State
			if len(e.Metadata) > 0 {
				if block.Metadata == nil {
					block.Metadata = map[string]any{}
				}
				for key, value := range e.Metadata {
					block.Metadata[key] = utils.CloneAny(value)
				}
			}
		}
		if block, ok := m.FindBlock("tool_call", e.ToolCallID).(*ToolCallBlock); ok {
			block.State = ToolCallFinished
		}
	default:
		return false
	}

	return true
}

func (m *Message) applyPermissionEvent(event Event) bool {
	switch e := event.(type) {
	case *RequireUserConfirmEvent:
		m.requireUserConfirm(e.ToolCalls)
	case *UserConfirmResultEvent:
		m.applyUserConfirmResults(e.ConfirmResults)
	case *RequireExternalExecutionEvent:
		m.requireExternalExecution(e.ToolCalls)
	case *ExternalExecutionResultEvent:
		m.applyExternalExecutionResults(e.ExecutionResults)
	default:
		return false
	}

	return true
}

func (m *Message) requireUserConfirm(toolCalls []*ToolCallBlock) {
	for _, toolCall := range toolCalls {
		if block, ok := m.FindBlock("tool_call", toolCall.ID).(*ToolCallBlock); ok {

			block.State = ToolCallAsking
			block.SuggestedRules = append([]permission.Rule(nil), toolCall.SuggestedRules...)
		}
	}
}

func (m *Message) applyUserConfirmResults(results []ConfirmResult) {
	for _, result := range results {
		if result.ToolCall == nil {
			continue
		}
		if block, ok := m.FindBlock("tool_call", result.ToolCall.ID).(*ToolCallBlock); ok {
			if result.Confirmed {
				block.State = ToolCallAllowed
			} else {
				block.State = ToolCallFinished
			}
		}
	}
}

func (m *Message) requireExternalExecution(toolCalls []*ToolCallBlock) {
	for _, toolCall := range toolCalls {
		if toolCall == nil {
			continue
		}

		if block, ok := m.FindBlock("tool_call", toolCall.ID).(*ToolCallBlock); ok {
			block.State = ToolCallSubmitted
		}
	}
}

func (m *Message) applyExternalExecutionResults(results []*ToolResultBlock) {
	for _, result := range results {
		if result != nil {
			m.Content = append(m.Content, result.Clone())
		}
	}
}

func appendToolResultText(block *ToolResultBlock, delta string) {
	ensureToolResultBlocks(block)

	if len(block.Output.Blocks) == 0 || block.Output.Blocks[len(block.Output.Blocks)-1].BlockType() != "text" {
		block.Output.Blocks = append(block.Output.Blocks, NewTextBlock(delta))
		return
	}

	block.Output.Blocks[len(block.Output.Blocks)-1].(*TextBlock).Text += delta
}

func appendToolResultData(block *ToolResultBlock, event *ToolResultDataDeltaEvent) {
	ensureToolResultBlocks(block)
	var source DataSource

	if event.Data != "" {
		source = NewBase64Source(event.Data, event.MediaType)
	} else {
		source = NewURLSource(event.URL, event.MediaType)
	}

	block.Output.Blocks = append(block.Output.Blocks, &DataBlock{Type: "data", ID: event.BlockID, Source: source})
}

func ensureToolResultBlocks(block *ToolResultBlock) {
	if block.Output.Blocks != nil {
		return
	}

	block.Output.Blocks = ContentBlockList{}
	if block.Output.Raw != "" {
		block.Output.Blocks = append(block.Output.Blocks, NewTextBlock(block.Output.Raw))
		block.Output.Raw = ""
	}
}

func hintBlockFromEvent(event *HintBlockEvent) *HintBlock {
	var block *HintBlock

	if event.Blocks != nil {
		block = NewHintBlock(event.Blocks, WithHintBlockID(event.BlockID))
	} else {
		block = NewHintBlock(event.Hint, WithHintBlockID(event.BlockID))
	}

	if event.Source != nil {
		block.Source = cloneString(*event.Source)
	}

	return block
}
