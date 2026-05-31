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

import "github.com/yuluo-yx/agentscope-go/permission"

type EventType string

const (
	ReplyStartType               EventType = "REPLY_START"
	ReplyEndType                 EventType = "REPLY_END"
	ModelCallStartType           EventType = "MODEL_CALL_START"
	ModelCallEndType             EventType = "MODEL_CALL_END"
	TextBlockStartType           EventType = "TEXT_BLOCK_START"
	TextBlockDeltaType           EventType = "TEXT_BLOCK_DELTA"
	TextBlockEndType             EventType = "TEXT_BLOCK_END"
	DataBlockStartType           EventType = "DATA_BLOCK_START"
	DataBlockDeltaType           EventType = "DATA_BLOCK_DELTA"
	DataBlockEndType             EventType = "DATA_BLOCK_END"
	ThinkingBlockStartType       EventType = "THINKING_BLOCK_START"
	ThinkingBlockDeltaType       EventType = "THINKING_BLOCK_DELTA"
	ThinkingBlockEndType         EventType = "THINKING_BLOCK_END"
	ToolCallStartType            EventType = "TOOL_CALL_START"
	ToolCallDeltaType            EventType = "TOOL_CALL_DELTA"
	ToolCallEndType              EventType = "TOOL_CALL_END"
	ToolResultStartType          EventType = "TOOL_RESULT_START"
	ToolResultTextDeltaType      EventType = "TOOL_RESULT_TEXT_DELTA"
	ToolResultDataDeltaType      EventType = "TOOL_RESULT_DATA_DELTA"
	ToolResultEndType            EventType = "TOOL_RESULT_END"
	ExceedMaxItersType           EventType = "EXCEED_MAX_ITERS"
	RequireUserConfirmType       EventType = "REQUIRE_USER_CONFIRM"
	RequireExternalExecutionType EventType = "REQUIRE_EXTERNAL_EXECUTION"
	UserConfirmResultType        EventType = "USER_CONFIRM_RESULT"
	ExternalExecutionResultType  EventType = "EXTERNAL_EXECUTION_RESULT"
)

type Event interface {
	GetType() EventType
	GetID() string
	GetTime() string
	ReplyID() string
	event()
}

type eventMarker struct{}

func (eventMarker) event() {}

type EventBase struct {
	eventMarker
	Type      EventType `json:"type"`
	ID        string    `json:"id"`
	CreatedAt string    `json:"created_at"`
}

func (e *EventBase) GetType() EventType { return e.Type }
func (e *EventBase) GetID() string      { return e.ID }
func (e *EventBase) GetTime() string    { return e.CreatedAt }

type ReplyEventBase struct {
	EventBase
	ReplyIDValue string `json:"reply_id"`
}

func (e *ReplyEventBase) ReplyID() string { return e.ReplyIDValue }

type ReplyStartEvent struct {
	ReplyEventBase
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Role      Role   `json:"role"`
}

type ReplyEndEvent struct {
	ReplyEventBase
	SessionID string `json:"session_id"`
}

type ModelCallStartEvent struct {
	ReplyEventBase
	ModelName string `json:"model_name"`
}

type ModelCallEndEvent struct {
	ReplyEventBase
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type TextBlockStartEvent struct {
	ReplyEventBase
	BlockID string `json:"block_id"`
}

type TextBlockDeltaEvent struct {
	ReplyEventBase
	BlockID string `json:"block_id"`
	Delta   string `json:"delta"`
}

type TextBlockEndEvent struct {
	ReplyEventBase
	BlockID string `json:"block_id"`
}

type DataBlockStartEvent struct {
	ReplyEventBase
	BlockID   string `json:"block_id"`
	MediaType string `json:"media_type"`
}

type DataBlockDeltaEvent struct {
	ReplyEventBase
	BlockID   string `json:"block_id"`
	Data      string `json:"data"`
	MediaType string `json:"media_type"`
}

type DataBlockEndEvent struct {
	ReplyEventBase
	BlockID string `json:"block_id"`
}

type ThinkingBlockStartEvent struct {
	ReplyEventBase
	BlockID string `json:"block_id"`
}

type ThinkingBlockDeltaEvent struct {
	ReplyEventBase
	BlockID string `json:"block_id"`
	Delta   string `json:"delta"`
}

type ThinkingBlockEndEvent struct {
	ReplyEventBase
	BlockID string `json:"block_id"`
}

type ToolCallStartEvent struct {
	ReplyEventBase
	ToolCallID   string `json:"tool_call_id"`
	ToolCallName string `json:"tool_call_name"`
}

type ToolCallDeltaEvent struct {
	ReplyEventBase
	ToolCallID string `json:"tool_call_id"`
	Delta      string `json:"delta"`
}

type ToolCallEndEvent struct {
	ReplyEventBase
	ToolCallID string `json:"tool_call_id"`
}

type ToolResultStartEvent struct {
	ReplyEventBase
	ToolCallID   string `json:"tool_call_id"`
	ToolCallName string `json:"tool_call_name"`
}

type ToolResultTextDeltaEvent struct {
	ReplyEventBase
	ToolCallID string `json:"tool_call_id"`
	Delta      string `json:"delta"`
}

type ToolResultDataDeltaEvent struct {
	ReplyEventBase
	ToolCallID string `json:"tool_call_id"`
	BlockID    string `json:"block_id"`
	MediaType  string `json:"media_type"`
	Data       string `json:"data,omitempty"`
	URL        string `json:"url,omitempty"`
}

type ToolResultEndEvent struct {
	ReplyEventBase
	ToolCallID string          `json:"tool_call_id"`
	State      ToolResultState `json:"state"`
}

type ExceedMaxItersEvent struct {
	ReplyEventBase
	Name string `json:"name"`
}

type RequireUserConfirmEvent struct {
	ReplyEventBase
	ToolCalls []*ToolCallBlock `json:"tool_calls"`
}

type RequireExternalExecutionEvent struct {
	ReplyEventBase
	ToolCalls []*ToolCallBlock `json:"tool_calls"`
}

type ConfirmResult struct {
	Confirmed bool              `json:"confirmed"`
	ToolCall  *ToolCallBlock    `json:"tool_call"`
	Rules     []permission.Rule `json:"rules,omitempty"`
}

type UserConfirmResultEvent struct {
	ReplyEventBase
	ConfirmResults []ConfirmResult `json:"confirm_results"`
}

type ExternalExecutionResultEvent struct {
	ReplyEventBase
	ExecutionResults []*ToolResultBlock `json:"execution_results"`
}

func newEventBase(typ EventType) EventBase {
	return EventBase{Type: typ, ID: newID(), CreatedAt: nowISO()}
}

func newReplyEventBase(typ EventType, replyID string) ReplyEventBase {
	return ReplyEventBase{EventBase: newEventBase(typ), ReplyIDValue: replyID}
}

func NewReplyStartEvent(sessionID, replyID, name string) *ReplyStartEvent {
	return &ReplyStartEvent{ReplyEventBase: newReplyEventBase(ReplyStartType, replyID), SessionID: sessionID, Name: name, Role: RoleAssistant}
}

func NewReplyEndEvent(sessionID, replyID string) *ReplyEndEvent {
	return &ReplyEndEvent{ReplyEventBase: newReplyEventBase(ReplyEndType, replyID), SessionID: sessionID}
}

func NewModelCallStartEvent(replyID, modelName string) *ModelCallStartEvent {
	return &ModelCallStartEvent{ReplyEventBase: newReplyEventBase(ModelCallStartType, replyID), ModelName: modelName}
}

func NewModelCallEndEvent(replyID string, inputTokens, outputTokens int) *ModelCallEndEvent {
	return &ModelCallEndEvent{ReplyEventBase: newReplyEventBase(ModelCallEndType, replyID), InputTokens: inputTokens, OutputTokens: outputTokens}
}

func NewTextBlockStartEvent(replyID, blockID string) *TextBlockStartEvent {
	return &TextBlockStartEvent{ReplyEventBase: newReplyEventBase(TextBlockStartType, replyID), BlockID: blockID}
}

func NewTextBlockDeltaEvent(replyID, blockID, delta string) *TextBlockDeltaEvent {
	return &TextBlockDeltaEvent{ReplyEventBase: newReplyEventBase(TextBlockDeltaType, replyID), BlockID: blockID, Delta: delta}
}

func NewTextBlockEndEvent(replyID, blockID string) *TextBlockEndEvent {
	return &TextBlockEndEvent{ReplyEventBase: newReplyEventBase(TextBlockEndType, replyID), BlockID: blockID}
}

func NewDataBlockStartEvent(replyID, blockID, mediaType string) *DataBlockStartEvent {
	return &DataBlockStartEvent{ReplyEventBase: newReplyEventBase(DataBlockStartType, replyID), BlockID: blockID, MediaType: mediaType}
}

func NewDataBlockDeltaEvent(replyID, blockID, data, mediaType string) *DataBlockDeltaEvent {
	return &DataBlockDeltaEvent{ReplyEventBase: newReplyEventBase(DataBlockDeltaType, replyID), BlockID: blockID, Data: data, MediaType: mediaType}
}

func NewDataBlockEndEvent(replyID, blockID string) *DataBlockEndEvent {
	return &DataBlockEndEvent{ReplyEventBase: newReplyEventBase(DataBlockEndType, replyID), BlockID: blockID}
}

func NewThinkingBlockStartEvent(replyID, blockID string) *ThinkingBlockStartEvent {
	return &ThinkingBlockStartEvent{ReplyEventBase: newReplyEventBase(ThinkingBlockStartType, replyID), BlockID: blockID}
}

func NewThinkingBlockDeltaEvent(replyID, blockID, delta string) *ThinkingBlockDeltaEvent {
	return &ThinkingBlockDeltaEvent{ReplyEventBase: newReplyEventBase(ThinkingBlockDeltaType, replyID), BlockID: blockID, Delta: delta}
}

func NewThinkingBlockEndEvent(replyID, blockID string) *ThinkingBlockEndEvent {
	return &ThinkingBlockEndEvent{ReplyEventBase: newReplyEventBase(ThinkingBlockEndType, replyID), BlockID: blockID}
}

func NewToolCallStartEvent(replyID, toolCallID, toolCallName string) *ToolCallStartEvent {
	return &ToolCallStartEvent{ReplyEventBase: newReplyEventBase(ToolCallStartType, replyID), ToolCallID: toolCallID, ToolCallName: toolCallName}
}

func NewToolCallDeltaEvent(replyID, toolCallID, delta string) *ToolCallDeltaEvent {
	return &ToolCallDeltaEvent{ReplyEventBase: newReplyEventBase(ToolCallDeltaType, replyID), ToolCallID: toolCallID, Delta: delta}
}

func NewToolCallEndEvent(replyID, toolCallID string) *ToolCallEndEvent {
	return &ToolCallEndEvent{ReplyEventBase: newReplyEventBase(ToolCallEndType, replyID), ToolCallID: toolCallID}
}

func NewToolResultStartEvent(replyID, toolCallID, toolCallName string) *ToolResultStartEvent {
	return &ToolResultStartEvent{ReplyEventBase: newReplyEventBase(ToolResultStartType, replyID), ToolCallID: toolCallID, ToolCallName: toolCallName}
}

func NewToolResultTextDeltaEvent(replyID, toolCallID, delta string) *ToolResultTextDeltaEvent {
	return &ToolResultTextDeltaEvent{ReplyEventBase: newReplyEventBase(ToolResultTextDeltaType, replyID), ToolCallID: toolCallID, Delta: delta}
}

func NewToolResultDataDeltaEvent(replyID, toolCallID, blockID, mediaType, data, url string) *ToolResultDataDeltaEvent {
	return &ToolResultDataDeltaEvent{ReplyEventBase: newReplyEventBase(ToolResultDataDeltaType, replyID), ToolCallID: toolCallID, BlockID: blockID, MediaType: mediaType, Data: data, URL: url}
}

func NewToolResultEndEvent(replyID, toolCallID string, state ToolResultState) *ToolResultEndEvent {
	return &ToolResultEndEvent{ReplyEventBase: newReplyEventBase(ToolResultEndType, replyID), ToolCallID: toolCallID, State: state}
}

func NewExceedMaxItersEvent(replyID, name string) *ExceedMaxItersEvent {
	return &ExceedMaxItersEvent{ReplyEventBase: newReplyEventBase(ExceedMaxItersType, replyID), Name: name}
}

func NewRequireUserConfirmEvent(replyID string, toolCalls []*ToolCallBlock) *RequireUserConfirmEvent {
	return &RequireUserConfirmEvent{ReplyEventBase: newReplyEventBase(RequireUserConfirmType, replyID), ToolCalls: toolCalls}
}

func NewRequireExternalExecutionEvent(replyID string, toolCalls []*ToolCallBlock) *RequireExternalExecutionEvent {
	return &RequireExternalExecutionEvent{ReplyEventBase: newReplyEventBase(RequireExternalExecutionType, replyID), ToolCalls: toolCalls}
}

func NewUserConfirmResultEvent(replyID string, results []ConfirmResult) *UserConfirmResultEvent {
	return &UserConfirmResultEvent{ReplyEventBase: newReplyEventBase(UserConfirmResultType, replyID), ConfirmResults: results}
}

func NewExternalExecutionResultEvent(replyID string, results []*ToolResultBlock) *ExternalExecutionResultEvent {
	return &ExternalExecutionResultEvent{ReplyEventBase: newReplyEventBase(ExternalExecutionResultType, replyID), ExecutionResults: results}
}
