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

package event

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// EventTypeScheduleTick is the default event type emitted by TickerSource.
	EventTypeScheduleTick = "schedule.tick"
)

// Event is the generic automation envelope that can represent schedule ticks,
// webhooks, queue messages, MCP changes, manual requests, and adapter-defined
// business events without adding platform-specific types to the core package.
type Event struct {
	ID              string            `json:"id"`
	Source          string            `json:"source"`
	Type            string            `json:"type"`
	Subject         string            `json:"subject,omitempty"`
	Time            time.Time         `json:"time,omitempty"`
	DataContentType string            `json:"datacontenttype,omitempty"`
	Data            json.RawMessage   `json:"data,omitempty"`
	Extensions      map[string]string `json:"extensions,omitempty"`
	CorrelationID   string            `json:"correlation_id,omitempty"`
	CausationID     string            `json:"causation_id,omitempty"`
	DedupKey        string            `json:"dedup_key,omitempty"`
	Labels          []string          `json:"labels,omitempty"`
	Priority        int               `json:"priority,omitempty"`
}

// Validate checks the generic event envelope required by Runner.
func (e Event) Validate() error {
	switch {
	case strings.TrimSpace(e.ID) == "":
		return fmt.Errorf("automation: event id is empty")
	case strings.TrimSpace(e.Source) == "":
		return fmt.Errorf("automation: event source is empty")
	case strings.TrimSpace(e.Type) == "":
		return fmt.Errorf("automation: event type is empty")
	default:
		return nil
	}
}

// DeduplicationKey returns the key used for event de-duplication.
func (e Event) DeduplicationKey() string {
	if key := strings.TrimSpace(e.DedupKey); key != "" {
		return key
	}
	return strings.TrimSpace(e.ID)
}

// Clone returns a deep copy of the event envelope.
func (e Event) Clone() Event {
	cp := e
	if e.Data != nil {
		cp.Data = append(json.RawMessage(nil), e.Data...)
	}
	if e.Extensions != nil {
		cp.Extensions = make(map[string]string, len(e.Extensions))
		for key, value := range e.Extensions {
			cp.Extensions[key] = value
		}
	}
	cp.Labels = append([]string(nil), e.Labels...)
	return cp
}

// EventSource produces generic automation events.
type EventSource interface {
	Start(context.Context, EventHandler) error
}

// EventHandler handles one generic automation event.
type EventHandler interface {
	HandleEvent(context.Context, Event) error
}

// EventHandlerFunc adapts a function to EventHandler.
type EventHandlerFunc func(context.Context, Event) error

// HandleEvent calls f(ctx, event).
func (f EventHandlerFunc) HandleEvent(ctx context.Context, event Event) error {
	if f == nil {
		return fmt.Errorf("automation: event handler is nil")
	}
	return f(ctx, event)
}

// CloneStringMap returns a deep copy of a string map.
func CloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cp := make(map[string]string, len(values))
	for key, value := range values {
		cp[key] = value
	}
	return cp
}
