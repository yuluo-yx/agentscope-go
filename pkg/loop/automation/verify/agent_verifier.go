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

package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/core"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

// VerificationMapper maps verification input into the checker Agent request.
type VerificationMapper interface {
	MapVerification(context.Context, core.VerificationInput) (*message.Message, error)
}

// VerificationMapperFunc adapts a function to VerificationMapper.
type VerificationMapperFunc func(context.Context, core.VerificationInput) (*message.Message, error)

// MapVerification calls f(ctx, input).
func (f VerificationMapperFunc) MapVerification(
	ctx context.Context,
	input core.VerificationInput,
) (*message.Message, error) {
	if f == nil {
		return nil, fmt.Errorf("automation: verification mapper is nil")
	}
	return f(ctx, input)
}

// VerificationParser parses the checker Agent reply into a verification result.
type VerificationParser interface {
	ParseVerification(context.Context, core.VerificationInput, *message.Message) (core.VerificationResult, error)
}

// VerificationParserFunc adapts a function to VerificationParser.
type VerificationParserFunc func(context.Context, core.VerificationInput, *message.Message) (core.VerificationResult, error)

// ParseVerification calls f(ctx, input, reply).
func (f VerificationParserFunc) ParseVerification(
	ctx context.Context,
	input core.VerificationInput,
	reply *message.Message,
) (core.VerificationResult, error) {
	if f == nil {
		return core.VerificationResult{}, fmt.Errorf("automation: verification parser is nil")
	}
	return f(ctx, input, reply)
}

// AgentVerifier uses a checker Agent to implement core.Verifier.
type AgentVerifier struct {
	Agent  *agent.Agent
	Mapper VerificationMapper
	Parser VerificationParser
}

// Verify asks the checker Agent to evaluate a maker Agent run.
func (v AgentVerifier) Verify(
	ctx context.Context,
	input core.VerificationInput,
) (core.VerificationResult, error) {
	if ctx == nil {
		return core.VerificationResult{}, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return core.VerificationResult{}, err
	}
	if v.Agent == nil {
		return core.VerificationResult{}, fmt.Errorf("automation: verifier agent is nil")
	}

	mapper := v.Mapper
	if mapper == nil {
		mapper = DefaultVerificationMapper{}
	}
	parser := v.Parser
	if parser == nil {
		parser = JSONVerificationParser{}
	}

	request, err := mapper.MapVerification(ctx, input)
	if err != nil {
		return core.VerificationResult{}, err
	}
	if request == nil {
		return core.VerificationResult{}, fmt.Errorf("automation: verification request is nil")
	}
	reply, err := v.Agent.Reply(ctx, request)
	if err != nil {
		return core.VerificationResult{}, err
	}
	return parser.ParseVerification(ctx, input, reply)
}

// DefaultVerificationMapper builds a structured checker prompt.
type DefaultVerificationMapper struct {
	Name string
}

// MapVerification returns a user message asking the checker Agent to emit JSON.
func (m DefaultVerificationMapper) MapVerification(
	ctx context.Context,
	input core.VerificationInput,
) (*message.Message, error) {
	if ctx == nil {
		return nil, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = "verifier"
	}
	return message.NewUserMessage(name, verificationPrompt(input))
}

func verificationPrompt(input core.VerificationInput) string {
	var builder strings.Builder
	builder.WriteString("Review the maker Agent run against the loop contract.\n")
	builder.WriteString("Return only JSON with this shape: ")
	builder.WriteString(`{"passed":true,"reason":"...","evidence":["..."],"next_action":"..."}`)
	builder.WriteString(".\n\n")
	builder.WriteString("Maker agent: ")
	builder.WriteString(input.AgentName)
	builder.WriteString("\nSession ID: ")
	builder.WriteString(input.SessionID)
	builder.WriteString("\nReply ID: ")
	builder.WriteString(input.ReplyID)
	builder.WriteString("\nLoop: ")
	builder.WriteString(input.Spec.Name)
	builder.WriteString("\nGoal: ")
	builder.WriteString(input.Spec.Goal)
	if len(input.Spec.SuccessCriteria) > 0 {
		builder.WriteString("\nSuccess criteria:")
		for _, criterion := range input.Spec.SuccessCriteria {
			builder.WriteString("\n- ")
			builder.WriteString(criterion.Name)
			if criterion.Description != "" {
				builder.WriteString(": ")
				builder.WriteString(criterion.Description)
			}
			if criterion.Required {
				builder.WriteString(" (required)")
			}
		}
	}
	if input.State != nil && input.State.LoopContext != nil {
		builder.WriteString("\nLoop stop reason: ")
		builder.WriteString(string(input.State.LoopContext.StopReason))
	}
	return builder.String()
}

// JSONVerificationParser parses checker replies formatted as JSON.
type JSONVerificationParser struct{}

// ParseVerification parses the reply text into core.VerificationResult.
func (p JSONVerificationParser) ParseVerification(
	ctx context.Context,
	_ core.VerificationInput,
	reply *message.Message,
) (core.VerificationResult, error) {
	if ctx == nil {
		return core.VerificationResult{}, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return core.VerificationResult{}, err
	}
	if reply == nil {
		return core.VerificationResult{}, fmt.Errorf("automation: verification reply is nil")
	}
	text := reply.GetTextContent("")
	if text == nil || strings.TrimSpace(*text) == "" {
		return core.VerificationResult{}, fmt.Errorf("automation: verification reply is empty")
	}
	payload := struct {
		Passed     bool     `json:"passed"`
		Reason     string   `json:"reason"`
		Evidence   []string `json:"evidence"`
		NextAction string   `json:"next_action"`
	}{}
	if err := json.Unmarshal([]byte(stripJSONFence(*text)), &payload); err != nil {
		return core.VerificationResult{}, fmt.Errorf("automation: parse verification json: %w", err)
	}
	return core.VerificationResult{
		Passed:     payload.Passed,
		Reason:     payload.Reason,
		Evidence:   append([]string(nil), payload.Evidence...),
		NextAction: payload.NextAction,
	}, nil
}

func stripJSONFence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return text
	}
	if strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

var _ core.Verifier = AgentVerifier{}
