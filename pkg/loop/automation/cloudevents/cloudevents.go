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

// Package cloudevents converts between CloudEvents SDK events and the generic
// event.Event envelope.
package cloudevents

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	cesdk "github.com/cloudevents/sdk-go/v2"

	"github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
)

const (
	extensionCorrelationID = "correlationid"
	extensionCausationID   = "causationid"
	extensionDedupKey      = "dedupkey"
	extensionLabels        = "labels"
	extensionPriority      = "priority"
)

// FromCloudEvent converts a CloudEvents SDK event to event.Event.
func FromCloudEvent(cloudEvent cesdk.Event) (event.Event, error) {
	out := event.Event{
		ID:              cloudEvent.ID(),
		Source:          cloudEvent.Source(),
		Type:            cloudEvent.Type(),
		Subject:         cloudEvent.Subject(),
		Time:            cloudEvent.Time(),
		DataContentType: cloudEvent.DataContentType(),
		Data:            append(json.RawMessage(nil), cloudEvent.Data()...),
		Extensions:      map[string]string{},
	}
	for key, value := range cloudEvent.Extensions() {
		switch key {
		case extensionCorrelationID:
			out.CorrelationID = extensionString(value)
		case extensionCausationID:
			out.CausationID = extensionString(value)
		case extensionDedupKey:
			out.DedupKey = extensionString(value)
		case extensionLabels:
			out.Labels = splitLabels(extensionString(value))
		case extensionPriority:
			out.Priority = extensionInt(value)
		default:
			if text := extensionString(value); text != "" {
				out.Extensions[key] = text
			}
		}
	}
	if len(out.Extensions) == 0 {
		out.Extensions = nil
	}
	if err := out.Validate(); err != nil {
		return event.Event{}, err
	}
	return out, nil
}

// ToCloudEvent converts event.Event to a CloudEvents SDK event.
func ToCloudEvent(in event.Event) (cesdk.Event, error) {
	if err := in.Validate(); err != nil {
		return cesdk.Event{}, err
	}
	out := cesdk.NewEvent()
	out.SetID(in.ID)
	out.SetSource(in.Source)
	out.SetType(in.Type)
	if in.Subject != "" {
		out.SetSubject(in.Subject)
	}
	if !in.Time.IsZero() {
		out.SetTime(in.Time)
	}
	if len(in.Data) > 0 || in.DataContentType != "" {
		if err := out.SetData(in.DataContentType, append(json.RawMessage(nil), in.Data...)); err != nil {
			return cesdk.Event{}, fmt.Errorf("cloudevents: set data: %w", err)
		}
	}
	setStringExtension(&out, extensionCorrelationID, in.CorrelationID)
	setStringExtension(&out, extensionCausationID, in.CausationID)
	setStringExtension(&out, extensionDedupKey, in.DedupKey)
	if len(in.Labels) > 0 {
		out.SetExtension(extensionLabels, strings.Join(in.Labels, ","))
	}
	if in.Priority != 0 {
		out.SetExtension(extensionPriority, in.Priority)
	}
	for key, value := range in.Extensions {
		switch key {
		case extensionCorrelationID, extensionCausationID, extensionDedupKey, extensionLabels, extensionPriority:
			continue
		default:
			setStringExtension(&out, key, value)
		}
	}
	return out, nil
}

func setStringExtension(event *cesdk.Event, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	event.SetExtension(key, value)
}

func extensionString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case []byte:
		return string(typed)
	case int:
		return strconv.Itoa(typed)
	case int32:
		return strconv.Itoa(int(typed))
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func extensionInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func splitLabels(value string) []string {
	parts := strings.Split(value, ",")
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			labels = append(labels, part)
		}
	}
	return labels
}
