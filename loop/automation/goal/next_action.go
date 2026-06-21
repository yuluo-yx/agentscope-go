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

package goal

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/yuluo-yx/agentscope-go/loop/core"
	"github.com/yuluo-yx/agentscope-go/message"
)

// NextActionMapper maps verifier feedback into the next user message.
type NextActionMapper interface {
	MapNextAction(context.Context, core.VerificationInput, core.VerificationResult) (*message.Message, error)
}

// NextActionMapperFunc adapts a function to NextActionMapper.
type NextActionMapperFunc func(context.Context, core.VerificationInput, core.VerificationResult) (*message.Message, error)

// MapNextAction calls f(ctx, input, result).
func (f NextActionMapperFunc) MapNextAction(ctx context.Context, input core.VerificationInput, result core.VerificationResult) (*message.Message, error) {
	if f == nil {
		return nil, fmt.Errorf("automation: next action mapper is nil")
	}
	return f(ctx, input, result)
}

// TemplateNextActionMapper renders verifier feedback into a user message.
type TemplateNextActionMapper struct {
	tmpl     *template.Template
	userName string
}

// NewTemplateNextActionMapper creates a template-backed next-action mapper.
func NewTemplateNextActionMapper(text string) (*TemplateNextActionMapper, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("automation: next action template is empty")
	}
	tmpl, err := template.New("automation_next_action").Parse(text)
	if err != nil {
		return nil, fmt.Errorf("automation: parse next action template: %w", err)
	}
	return &TemplateNextActionMapper{tmpl: tmpl, userName: "user"}, nil
}

// MapNextAction renders the template into a user message.
func (m *TemplateNextActionMapper) MapNextAction(ctx context.Context, input core.VerificationInput, result core.VerificationResult) (*message.Message, error) {
	if m == nil || m.tmpl == nil {
		return nil, fmt.Errorf("automation: next action mapper is nil")
	}
	if ctx == nil {
		return nil, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var builder bytes.Buffer
	if err := m.tmpl.Execute(&builder, NextActionTemplateData{Input: input, Result: result}); err != nil {
		return nil, fmt.Errorf("automation: execute next action template: %w", err)
	}
	return message.NewUserMessage(m.userName, builder.String(), message.WithMessageMetadata(map[string]any{
		"automation_reply_id": input.ReplyID,
		"automation_loop":     input.Spec.Name,
	}))
}

// NextActionTemplateData is passed to TemplateNextActionMapper templates.
type NextActionTemplateData struct {
	Input  core.VerificationInput
	Result core.VerificationResult
}
