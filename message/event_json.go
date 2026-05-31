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
	"encoding/json"
	"fmt"
)

func MarshalEvent(event Event) ([]byte, error) {
	return json.Marshal(event)
}

func UnmarshalEvent(data []byte) (Event, error) {
	var probe struct {
		Type EventType `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}

	switch probe.Type {
	case ReplyStartType:
		var event ReplyStartEvent
		return &event, json.Unmarshal(data, &event)
	case ReplyEndType:
		var event ReplyEndEvent
		return &event, json.Unmarshal(data, &event)
	case ModelCallStartType:
		var event ModelCallStartEvent
		return &event, json.Unmarshal(data, &event)
	case ModelCallEndType:
		var event ModelCallEndEvent
		return &event, json.Unmarshal(data, &event)
	case TextBlockStartType:
		var event TextBlockStartEvent
		return &event, json.Unmarshal(data, &event)
	case TextBlockDeltaType:
		var event TextBlockDeltaEvent
		return &event, json.Unmarshal(data, &event)
	case TextBlockEndType:
		var event TextBlockEndEvent
		return &event, json.Unmarshal(data, &event)
	case DataBlockStartType:
		var event DataBlockStartEvent
		return &event, json.Unmarshal(data, &event)
	case DataBlockDeltaType:
		var event DataBlockDeltaEvent
		return &event, json.Unmarshal(data, &event)
	case DataBlockEndType:
		var event DataBlockEndEvent
		return &event, json.Unmarshal(data, &event)
	case ThinkingBlockStartType:
		var event ThinkingBlockStartEvent
		return &event, json.Unmarshal(data, &event)
	case ThinkingBlockDeltaType:
		var event ThinkingBlockDeltaEvent
		return &event, json.Unmarshal(data, &event)
	case ThinkingBlockEndType:
		var event ThinkingBlockEndEvent
		return &event, json.Unmarshal(data, &event)
	case ToolCallStartType:
		var event ToolCallStartEvent
		return &event, json.Unmarshal(data, &event)
	case ToolCallDeltaType:
		var event ToolCallDeltaEvent
		return &event, json.Unmarshal(data, &event)
	case ToolCallEndType:
		var event ToolCallEndEvent
		return &event, json.Unmarshal(data, &event)
	case ToolResultStartType:
		var event ToolResultStartEvent
		return &event, json.Unmarshal(data, &event)
	case ToolResultTextDeltaType:
		var event ToolResultTextDeltaEvent
		return &event, json.Unmarshal(data, &event)
	case ToolResultDataDeltaType:
		var event ToolResultDataDeltaEvent
		return &event, json.Unmarshal(data, &event)
	case ToolResultEndType:
		var event ToolResultEndEvent
		return &event, json.Unmarshal(data, &event)
	case ExceedMaxItersType:
		var event ExceedMaxItersEvent
		return &event, json.Unmarshal(data, &event)
	case RequireUserConfirmType:
		var event RequireUserConfirmEvent
		return &event, json.Unmarshal(data, &event)
	case RequireExternalExecutionType:
		var event RequireExternalExecutionEvent
		return &event, json.Unmarshal(data, &event)
	case UserConfirmResultType:
		var event UserConfirmResultEvent
		return &event, json.Unmarshal(data, &event)
	case ExternalExecutionResultType:
		var event ExternalExecutionResultEvent
		return &event, json.Unmarshal(data, &event)
	default:
		return nil, fmt.Errorf("message: unsupported event type %q", probe.Type)
	}
}
