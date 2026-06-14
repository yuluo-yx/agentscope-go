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

// JSON serialization and deserialization utilities.

package message

import (
	"encoding/json"
	"fmt"
)

func MarshalEvent(event Event) ([]byte, error) {
	return json.Marshal(event)
}

var eventFactories = map[EventType]func() Event{
	ReplyStartType:               func() Event { return &ReplyStartEvent{} },
	ReplyEndType:                 func() Event { return &ReplyEndEvent{} },
	ModelCallStartType:           func() Event { return &ModelCallStartEvent{} },
	ModelCallEndType:             func() Event { return &ModelCallEndEvent{} },
	TextBlockStartType:           func() Event { return &TextBlockStartEvent{} },
	TextBlockDeltaType:           func() Event { return &TextBlockDeltaEvent{} },
	TextBlockEndType:             func() Event { return &TextBlockEndEvent{} },
	DataBlockStartType:           func() Event { return &DataBlockStartEvent{} },
	DataBlockDeltaType:           func() Event { return &DataBlockDeltaEvent{} },
	DataBlockEndType:             func() Event { return &DataBlockEndEvent{} },
	ThinkingBlockStartType:       func() Event { return &ThinkingBlockStartEvent{} },
	ThinkingBlockDeltaType:       func() Event { return &ThinkingBlockDeltaEvent{} },
	ThinkingBlockEndType:         func() Event { return &ThinkingBlockEndEvent{} },
	HintBlockType:                func() Event { return &HintBlockEvent{} },
	ToolCallStartType:            func() Event { return &ToolCallStartEvent{} },
	ToolCallDeltaType:            func() Event { return &ToolCallDeltaEvent{} },
	ToolCallEndType:              func() Event { return &ToolCallEndEvent{} },
	ToolResultStartType:          func() Event { return &ToolResultStartEvent{} },
	ToolResultTextDeltaType:      func() Event { return &ToolResultTextDeltaEvent{} },
	ToolResultDataDeltaType:      func() Event { return &ToolResultDataDeltaEvent{} },
	ToolResultEndType:            func() Event { return &ToolResultEndEvent{} },
	ExceedMaxItersType:           func() Event { return &ExceedMaxItersEvent{} },
	RequireUserConfirmType:       func() Event { return &RequireUserConfirmEvent{} },
	RequireExternalExecutionType: func() Event { return &RequireExternalExecutionEvent{} },
	UserConfirmResultType:        func() Event { return &UserConfirmResultEvent{} },
	ExternalExecutionResultType:  func() Event { return &ExternalExecutionResultEvent{} },
	CustomType:                   func() Event { return &CustomEvent{} },
}

func UnmarshalEvent(data []byte) (Event, error) {
	var probe struct {
		Type EventType `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}

	factory, ok := eventFactories[probe.Type]
	if !ok {
		return nil, fmt.Errorf("message: unsupported event type %q", probe.Type)
	}
	event := factory()
	return event, json.Unmarshal(data, event)
}

func (e HintBlockEvent) MarshalJSON() ([]byte, error) {
	var hint any = e.Hint
	if e.Blocks != nil {
		hint = e.Blocks
	}
	type wire struct {
		Type      EventType      `json:"type"`
		ID        string         `json:"id"`
		CreatedAt string         `json:"created_at"`
		Metadata  map[string]any `json:"metadata,omitempty"`
		ReplyID   string         `json:"reply_id"`
		BlockID   string         `json:"block_id"`
		Source    *string        `json:"source,omitempty"`
		Hint      any            `json:"hint"`
	}
	return json.Marshal(wire{
		Type:      e.Type,
		ID:        e.ID,
		CreatedAt: e.CreatedAt,
		Metadata:  e.Metadata,
		ReplyID:   e.ReplyIDValue,
		BlockID:   e.BlockID,
		Source:    e.Source,
		Hint:      hint,
	})
}

func (e *HintBlockEvent) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type      EventType       `json:"type"`
		ID        string          `json:"id"`
		CreatedAt string          `json:"created_at"`
		Metadata  map[string]any  `json:"metadata"`
		ReplyID   string          `json:"reply_id"`
		BlockID   string          `json:"block_id"`
		Source    *string         `json:"source"`
		Hint      json.RawMessage `json:"hint"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	hint, blocks, err := decodeHintContent(raw.Hint)
	if err != nil {
		return err
	}
	e.ReplyEventBase = ReplyEventBase{
		EventBase: EventBase{
			Type:      raw.Type,
			ID:        raw.ID,
			CreatedAt: raw.CreatedAt,
			Metadata:  raw.Metadata,
		},
		ReplyIDValue: raw.ReplyID,
	}
	e.BlockID = raw.BlockID
	e.Source = raw.Source
	e.Hint = hint
	e.Blocks = blocks
	return nil
}

func (e *CustomEvent) UnmarshalJSON(data []byte) error {
	type alias CustomEvent
	var value alias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*e = CustomEvent(value)
	if e.Value == nil {
		e.Value = map[string]any{}
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	return nil
}
