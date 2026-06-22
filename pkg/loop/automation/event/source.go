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

	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// TickerSource emits schedule tick events at a fixed interval.
type TickerSource struct {
	Source          string
	Type            string
	Subject         string
	Interval        time.Duration
	DataContentType string
	Data            json.RawMessage
	Extensions      map[string]string
	Labels          []string
	Priority        int
}

// Start emits events until ctx is canceled or the handler returns an error.
func (s TickerSource) Start(ctx context.Context, handler EventHandler) error {
	if ctx == nil {
		return fmt.Errorf("automation: context is nil")
	}
	if handler == nil {
		return fmt.Errorf("automation: event handler is nil")
	}
	if strings.TrimSpace(s.Source) == "" {
		return fmt.Errorf("automation: ticker source is empty")
	}
	if s.Interval <= 0 {
		return fmt.Errorf("automation: ticker interval must be positive")
	}

	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			event := s.event(now)
			if err := handler.HandleEvent(ctx, event); err != nil {
				return err
			}
		}
	}
}

func (s TickerSource) event(now time.Time) Event {
	eventType := strings.TrimSpace(s.Type)
	if eventType == "" {
		eventType = EventTypeScheduleTick
	}
	return Event{
		ID:              utils.NewID(),
		Source:          strings.TrimSpace(s.Source),
		Type:            eventType,
		Subject:         s.Subject,
		Time:            now,
		DataContentType: s.DataContentType,
		Data:            append(json.RawMessage(nil), s.Data...),
		Extensions:      CloneStringMap(s.Extensions),
		Labels:          append([]string(nil), s.Labels...),
		Priority:        s.Priority,
	}
}
