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

package runner

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/yuluo-yx/agentscope-go/loop/automation/event"
	"github.com/yuluo-yx/agentscope-go/message"
)

// InputMapper turns an automation event into Agent input.
type InputMapper interface {
	MapInput(context.Context, event.Event, event.RouteDecision) (*message.Message, error)
}

// InputMapperFunc adapts a function to InputMapper.
type InputMapperFunc func(context.Context, event.Event, event.RouteDecision) (*message.Message, error)

// MapInput calls f(ctx, event, decision).
func (f InputMapperFunc) MapInput(ctx context.Context, event event.Event, decision event.RouteDecision) (*message.Message, error) {
	if f == nil {
		return nil, fmt.Errorf("automation: input mapper is nil")
	}
	return f(ctx, event, decision)
}

// TemplateMapperOption configures a TemplateMapper.
type TemplateMapperOption func(*templateMapperOptions) error

type templateMapperOptions struct {
	userName string
}

// WithTemplateMapperUserName sets the user message name for TemplateMapper.
func WithTemplateMapperUserName(name string) TemplateMapperOption {
	return func(opts *templateMapperOptions) error {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("automation: template mapper user name is empty")
		}
		opts.userName = name
		return nil
	}
}

// TemplateMapper renders event fields into a user message.
type TemplateMapper struct {
	tmpl     *template.Template
	userName string
}

// NewTemplateMapper creates a mapper backed by text/template.
func NewTemplateMapper(text string, opts ...TemplateMapperOption) (*TemplateMapper, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("automation: template is empty")
	}
	options := templateMapperOptions{userName: "user"}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&options); err != nil {
			return nil, err
		}
	}
	tmpl, err := template.New("automation_input").Parse(text)
	if err != nil {
		return nil, fmt.Errorf("automation: parse template: %w", err)
	}
	return &TemplateMapper{tmpl: tmpl, userName: options.userName}, nil
}

// MapInput renders the configured template and returns a user message.
func (m *TemplateMapper) MapInput(ctx context.Context, event event.Event, decision event.RouteDecision) (*message.Message, error) {
	if m == nil || m.tmpl == nil {
		return nil, fmt.Errorf("automation: template mapper is nil")
	}
	if ctx == nil {
		return nil, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var builder bytes.Buffer
	if err := m.tmpl.Execute(&builder, TemplateData{
		Event:    event.Clone(),
		Route:    decision.Clone(),
		ID:       event.ID,
		Source:   event.Source,
		Type:     event.Type,
		Subject:  event.Subject,
		Time:     event.Time,
		Labels:   append([]string(nil), event.Labels...),
		Priority: event.Priority,
	}); err != nil {
		return nil, fmt.Errorf("automation: execute template: %w", err)
	}
	return message.NewUserMessage(m.userName, builder.String(), message.WithMessageMetadata(map[string]any{
		"automation_event_id":     event.ID,
		"automation_event_source": event.Source,
		"automation_event_type":   event.Type,
		"automation_loop_name":    decision.LoopName,
	}))
}

// TemplateData is the data object passed to TemplateMapper templates.
type TemplateData struct {
	Event    event.Event
	Route    event.RouteDecision
	ID       string
	Source   string
	Type     string
	Subject  string
	Time     time.Time
	Labels   []string
	Priority int
}
