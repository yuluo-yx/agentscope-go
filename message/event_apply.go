// Copyright 20\d\d AgentScope Go
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

	"github.com/yuluo-yx/agentscope-go/permission"
)

// ApplyEvent applies a streaming event to the message using Python-compatible accumulation semantics.
func (m *Message) ApplyEvent(event Event) error {
	if event == nil {
		return fmt.Errorf("message: nil event")
	}
	if event.ReplyID() != m.ID {
		return nil
	}

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
	case *ToolCallStartEvent:
		m.Content = append(m.Content, NewToolCallBlock(e.ToolCallID, e.ToolCallName, ""))
	case *ToolCallDeltaEvent:
		if block, ok := m.FindBlock("tool_call", e.ToolCallID).(*ToolCallBlock); ok {
			block.Input += e.Delta
		}
	case *ToolCallEndEvent:
	case *ToolResultStartEvent:
		m.Content = append(m.Content, NewToolResultBlock(
			e.ToolCallID,
			e.ToolCallName,
			ToolResultOutput{Blocks: ContentBlockList{}},
			ToolResultRunning,
		))
	case *ToolResultTextDeltaEvent:
		if block, ok := m.FindBlock("tool_result", e.ToolCallID).(*ToolResultBlock); ok {
			ensureToolResultBlocks(block)
			if len(block.Output.Blocks) == 0 || block.Output.Blocks[len(block.Output.Blocks)-1].BlockType() != "text" {
				block.Output.Blocks = append(block.Output.Blocks, NewTextBlock(e.Delta))
			} else {
				block.Output.Blocks[len(block.Output.Blocks)-1].(*TextBlock).Text += e.Delta
			}
		}
	case *ToolResultDataDeltaEvent:
		if block, ok := m.FindBlock("tool_result", e.ToolCallID).(*ToolResultBlock); ok {
			ensureToolResultBlocks(block)
			var source DataSource
			if e.Data != "" {
				source = NewBase64Source(e.Data, e.MediaType)
			} else {
				source = NewURLSource(e.URL, e.MediaType)
			}
			block.Output.Blocks = append(block.Output.Blocks, &DataBlock{Type: "data", ID: e.BlockID, Source: source})
		}
	case *ToolResultEndEvent:
		if block, ok := m.FindBlock("tool_result", e.ToolCallID).(*ToolResultBlock); ok {
			block.State = e.State
		}
	case *RequireUserConfirmEvent:
		for _, toolCall := range e.ToolCalls {
			if block, ok := m.FindBlock("tool_call", toolCall.ID).(*ToolCallBlock); ok {
				block.State = ToolCallAsking
				block.SuggestedRules = append([]permission.Rule(nil), toolCall.SuggestedRules...)
			}
		}
	case *UserConfirmResultEvent:
		for _, result := range e.ConfirmResults {
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
	case *RequireExternalExecutionEvent:
		for _, toolCall := range e.ToolCalls {
			if toolCall == nil {
				continue
			}
			if block, ok := m.FindBlock("tool_call", toolCall.ID).(*ToolCallBlock); ok {
				block.State = ToolCallSubmitted
			}
		}
	case *ExternalExecutionResultEvent:
		for _, result := range e.ExecutionResults {
			if result != nil {
				m.Content = append(m.Content, result.Clone())
			}
		}
	case *ReplyStartEvent, *ModelCallStartEvent, *ExceedMaxItersEvent:
	default:
		return fmt.Errorf("message: unsupported event %T", event)
	}
	return nil
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
