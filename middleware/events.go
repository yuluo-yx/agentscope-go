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
	"encoding/json"
	"fmt"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
)

// SSEEvent is a Server-Sent Events envelope derived from an Agent event.
type SSEEvent struct {
	Event string `json:"event"`
	ID    string `json:"id,omitempty"`
	Data  string `json:"data"`
}

// AGUIEvent is a lightweight AG-UI-compatible envelope derived from an Agent event.
type AGUIEvent struct {
	Type       string         `json:"type"`
	ID         string         `json:"id,omitempty"`
	MessageID  string         `json:"message_id,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Delta      string         `json:"delta,omitempty"`
	Name       string         `json:"name,omitempty"`
	Raw        message.Event  `json:"raw,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// ConvertedEvent is published by EventConversionMiddleware.
type ConvertedEvent struct {
	Protocol string
	Type     string
	ID       string
	Payload  any
}

// EventSink receives converted protocol events.
type EventSink interface {
	PublishEvent(context.Context, ConvertedEvent) error
}

// EventConverter converts a message event into a protocol envelope.
type EventConverter func(message.Event) (ConvertedEvent, error)

// EventConversionMiddleware publishes converted events while preserving the original Agent event stream.
type EventConversionMiddleware struct {
	name      string
	converter EventConverter
	sink      EventSink
}

// NewSSEEventMiddleware creates middleware that publishes SSE envelopes.
func NewSSEEventMiddleware(sink EventSink) *EventConversionMiddleware {
	return &EventConversionMiddleware{name: "sse-events", converter: ConvertEventToSSE, sink: sink}
}

// NewAGUIEventMiddleware creates middleware that publishes AG-UI envelopes.
func NewAGUIEventMiddleware(sink EventSink) *EventConversionMiddleware {
	return &EventConversionMiddleware{name: "ag-ui-events", converter: ConvertEventToAGUI, sink: sink}
}

// NewEventConversionMiddleware creates middleware with a custom converter.
func NewEventConversionMiddleware(name string, sink EventSink, converter EventConverter) *EventConversionMiddleware {
	if name == "" {
		name = "event-conversion"
	}
	return &EventConversionMiddleware{name: name, converter: converter, sink: sink}
}

// MiddlewareName returns the middleware name.
func (m *EventConversionMiddleware) MiddlewareName() string {
	if m == nil || m.name == "" {
		return "event-conversion"
	}
	return m.name
}

// OnReply converts every reply event and publishes it to the configured sink.
func (m *EventConversionMiddleware) OnReply(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	_ = agent
	_ = input
	if m == nil || m.sink == nil || m.converter == nil {
		return next(ctx)
	}
	events, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return nil, fmt.Errorf("agentscope/middleware: nil event stream")
	}
	out := make(chan message.Event)
	go func() {
		defer close(out)
		for event := range events {
			converted, err := m.converter(event)
			if err == nil {
				_ = m.sink.PublishEvent(context.WithoutCancel(ctx), converted)
			}
			out <- event
		}
	}()
	return out, nil
}

// ConvertEventToSSE converts an Agent event into a Server-Sent Events envelope.
func ConvertEventToSSE(event message.Event) (ConvertedEvent, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return ConvertedEvent{}, err
	}
	payload := SSEEvent{
		Event: string(event.GetType()),
		ID:    event.GetID(),
		Data:  string(data),
	}
	return ConvertedEvent{
		Protocol: "sse",
		Type:     payload.Event,
		ID:       payload.ID,
		Payload:  payload,
	}, nil
}

// ConvertEventToAGUI converts an Agent event into a lightweight AG-UI event envelope.
func ConvertEventToAGUI(event message.Event) (ConvertedEvent, error) {
	payload := AGUIEvent{
		Type:      aguiType(event),
		ID:        event.GetID(),
		MessageID: event.ReplyID(),
		Raw:       event,
		Metadata:  map[string]any{"agentscope.event_type": string(event.GetType())},
	}
	switch typed := event.(type) {
	case *message.ReplyStartEvent:
		payload.Name = typed.Name
	case *message.ExceedMaxItersEvent:
		payload.Name = typed.Name
	case *message.TextBlockDeltaEvent:
		payload.Delta = typed.Delta
	case *message.ThinkingBlockDeltaEvent:
		payload.Delta = typed.Delta
	case *message.HintBlockEvent:
		payload.Name = "hint_block"
		if typed.Source != nil {
			payload.Metadata["agentscope.hint_source"] = *typed.Source
		}
	case *message.ToolCallStartEvent:
		payload.ToolCallID = typed.ToolCallID
		payload.Name = typed.ToolCallName
	case *message.ToolCallDeltaEvent:
		payload.ToolCallID = typed.ToolCallID
		payload.Delta = typed.Delta
	case *message.ToolCallEndEvent:
		payload.ToolCallID = typed.ToolCallID
	case *message.ToolResultStartEvent:
		payload.ToolCallID = typed.ToolCallID
		payload.Name = typed.ToolCallName
	case *message.ToolResultTextDeltaEvent:
		payload.ToolCallID = typed.ToolCallID
		payload.Delta = typed.Delta
	case *message.ToolResultDataDeltaEvent:
		payload.ToolCallID = typed.ToolCallID
	case *message.ToolResultEndEvent:
		payload.ToolCallID = typed.ToolCallID
	case *message.CustomEvent:
		payload.Name = typed.Name
		payload.Metadata["agentscope.custom_value"] = typed.Value
	}
	return ConvertedEvent{
		Protocol: "ag-ui",
		Type:     payload.Type,
		ID:       payload.ID,
		Payload:  payload,
	}, nil
}

var aguiEventTypes = map[message.EventType]string{
	message.ReplyStartType:               "RUN_STARTED",
	message.ReplyEndType:                 "RUN_FINISHED",
	message.ModelCallStartType:           "MODEL_CALL_STARTED",
	message.ModelCallEndType:             "MODEL_CALL_FINISHED",
	message.TextBlockStartType:           "TEXT_MESSAGE_START",
	message.TextBlockDeltaType:           "TEXT_MESSAGE_CONTENT",
	message.TextBlockEndType:             "TEXT_MESSAGE_END",
	message.ThinkingBlockStartType:       "THINKING_START",
	message.ThinkingBlockDeltaType:       "THINKING_CONTENT",
	message.ThinkingBlockEndType:         "THINKING_END",
	message.HintBlockType:                "CUSTOM",
	message.ToolCallStartType:            "TOOL_CALL_START",
	message.ToolCallDeltaType:            "TOOL_CALL_ARGS",
	message.ToolCallEndType:              "TOOL_CALL_END",
	message.ToolResultStartType:          "TOOL_RESULT_START",
	message.ToolResultTextDeltaType:      "TOOL_RESULT_CONTENT",
	message.ToolResultDataDeltaType:      "TOOL_RESULT_CONTENT",
	message.ToolResultEndType:            "TOOL_RESULT_END",
	message.RequireUserConfirmType:       "REQUIRE_USER_CONFIRM",
	message.RequireExternalExecutionType: "REQUIRE_EXTERNAL_EXECUTION",
	message.UserConfirmResultType:        "USER_CONFIRM_RESULT",
	message.ExternalExecutionResultType:  "EXTERNAL_EXECUTION_RESULT",
	message.ExceedMaxItersType:           "RUN_ERROR",
	message.CustomType:                   "CUSTOM",
}

func aguiType(event message.Event) string {
	if typ, ok := aguiEventTypes[event.GetType()]; ok {
		return typ
	}
	return "CUSTOM"
}
