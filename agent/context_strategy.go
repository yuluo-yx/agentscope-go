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

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/types"
	"github.com/yuluo-yx/agentscope-go/utils"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

// ContextStrategy applies one context compression or offload step.
type ContextStrategy interface {
	// ContextStrategyName returns a stable strategy name for diagnostics.
	ContextStrategyName() string
	// ApplyContextStrategy mutates the provided Agent state when compression or offload is needed.
	ApplyContextStrategy(context.Context, *ContextStrategyInput) error
}

// ContextStrategyInput exposes the Agent state and helpers needed by context strategies.
type ContextStrategyInput struct {
	Agent        AgentAccessor
	State        *AgentState
	Model        ChatModel
	ToolProvider ToolProvider
	Offloader    asworkspace.Offloader
	Config       ContextConfig

	activeGroups []string
	systemPrompt func(context.Context) (string, error)
	toolSchemas  func() ([]ToolSchema, error)
}

// ActiveGroups returns the tool groups visible to the current Agent turn.
func (i *ContextStrategyInput) ActiveGroups() []string {
	if i == nil {
		return nil
	}
	return append([]string(nil), i.activeGroups...)
}

// SystemPrompt returns the current system prompt after system-prompt middleware.
func (i *ContextStrategyInput) SystemPrompt(ctx context.Context) (string, error) {
	if i == nil || i.systemPrompt == nil {
		return "", nil
	}
	return i.systemPrompt(ctx)
}

// ToolSchemas returns model-facing tool schemas for the active tool groups.
func (i *ContextStrategyInput) ToolSchemas() ([]ToolSchema, error) {
	if i == nil || i.toolSchemas == nil {
		return nil, nil
	}
	return i.toolSchemas()
}

// CurrentModelRequest builds the request currently represented by system prompt, summary, context, and tools.
func (i *ContextStrategyInput) CurrentModelRequest(ctx context.Context) (CallRequest, error) {
	if i == nil {
		return CallRequest{}, nil
	}
	systemPrompt, err := i.SystemPrompt(ctx)
	if err != nil {
		return CallRequest{}, err
	}
	systemMsg, err := message.NewSystemMessage("system", systemPrompt)
	if err != nil {
		return CallRequest{}, err
	}
	messages := []*message.Message{systemMsg}
	if summary := summaryMessageFromState(i.State); summary != nil {
		messages = append(messages, summary)
	}
	for _, msg := range i.State.Context {
		if msg != nil {
			messages = append(messages, msg.Clone())
		}
	}
	tools, err := i.ToolSchemas()
	if err != nil {
		return CallRequest{}, err
	}
	return CallRequest{Messages: messages, Tools: tools}, nil
}

// DefaultContextStrategies returns the built-in context strategy chain.
func DefaultContextStrategies() []ContextStrategy {
	return []ContextStrategy{
		NewToolResultContextStrategy(),
		NewSummaryContextStrategy(),
	}
}

// ToolResultContextStrategy offloads base64 data blocks and truncates or offloads oversized tool results.
type ToolResultContextStrategy struct{}

// NewToolResultContextStrategy creates the default tool-result cleanup strategy.
func NewToolResultContextStrategy() ContextStrategy {
	return ToolResultContextStrategy{}
}

// ContextStrategyName returns the strategy name.
func (ToolResultContextStrategy) ContextStrategyName() string {
	return "tool-result"
}

// ApplyContextStrategy runs the legacy tool-result and data-block cleanup behavior.
func (ToolResultContextStrategy) ApplyContextStrategy(ctx context.Context, input *ContextStrategyInput) error {
	if input == nil {
		return nil
	}
	agent, ok := input.Agent.(*Agent)
	if !ok || agent == nil {
		return nil
	}
	return agent.compressToolResults(ctx)
}

// SummaryContextStrategy summarizes old conversation context and offloads the compressed messages when possible.
type SummaryContextStrategy struct{}

// NewSummaryContextStrategy creates the default summary compression strategy.
func NewSummaryContextStrategy() ContextStrategy {
	return SummaryContextStrategy{}
}

// ContextStrategyName returns the strategy name.
func (SummaryContextStrategy) ContextStrategyName() string {
	return "summary"
}

// ApplyContextStrategy summarizes old messages when MaxTokens and TriggerRatio indicate pressure.
func (SummaryContextStrategy) ApplyContextStrategy(ctx context.Context, input *ContextStrategyInput) error {
	if !summaryPreconditionsMet(input) {
		return nil
	}
	needed, err := isSummaryNeeded(ctx, input)
	if err != nil {
		return err
	}
	if !needed {
		return nil
	}
	tools, err := input.ToolSchemas()
	if err != nil {
		return err
	}
	reserveLimit := int(float64(input.Config.MaxTokens) * input.Config.ReserveRatio)
	toCompress, toReserve, err := splitContextForSummary(ctx, input.Model, input.State.Context, tools, reserveLimit)
	if err != nil {
		return err
	}
	if len(toCompress) == 0 {
		return nil
	}
	return executeSummary(ctx, input, toCompress, toReserve)
}

// summaryPreconditionsMet checks whether the input and config allow summary compression.
func summaryPreconditionsMet(input *ContextStrategyInput) bool {
	if input == nil || input.State == nil || input.Model == nil {
		return false
	}
	return input.Config.MaxTokens > 0 && len(input.State.Context) > 1
}

// isSummaryNeeded returns true when current token count exceeds the summary threshold.
func isSummaryNeeded(ctx context.Context, input *ContextStrategyInput) (bool, error) {
	currentRequest, err := input.CurrentModelRequest(ctx)
	if err != nil {
		return false, err
	}
	currentTokens, err := input.Model.CountTokens(currentRequest)
	if err != nil {
		return false, err
	}
	threshold := int(float64(input.Config.MaxTokens) * input.Config.TriggerRatio)
	if threshold <= 0 || currentTokens < threshold {
		return false, nil
	}
	return true, nil
}

// executeSummary builds the summary, calls the model, and applies the result to state.
func executeSummary(ctx context.Context, input *ContextStrategyInput, toCompress, toReserve []*message.Message) error {
	request, err := buildSummaryRequest(ctx, input, toCompress)
	if err != nil {
		return err
	}
	response, err := input.Model.Call(ctx, request)
	if err != nil {
		return err
	}
	summaryText, err := summaryTextFromResponse(response, input.Config)
	if err != nil {
		return err
	}
	if input.Offloader != nil {
		path, err := input.Offloader.OffloadContext(ctx, input.State.SessionID, toCompress)
		if err != nil {
			return err
		}
		summaryText += "\n<system-reminder>The compressed context is offloaded to '" + path + "', you can refer to it when needed.</system-reminder>"
	}
	input.State.Summary.Text = summaryText
	input.State.Summary.Blocks = nil
	input.State.Context = toReserve
	return nil
}

func (a *Agent) newContextStrategyInput() *ContextStrategyInput {
	return &ContextStrategyInput{
		Agent:        a,
		State:        a.state,
		Model:        a.model,
		ToolProvider: a.toolkit,
		Offloader:    a.offloader,
		Config:       a.contextConfig,
		activeGroups: a.activeGroups(),
		systemPrompt: a.buildSystemPrompt,
		toolSchemas: func() ([]ToolSchema, error) {
			return a.toolkit.ToolSchemas(a.activeGroups()...)
		},
	}
}

func splitContextForSummary(ctx context.Context, model ChatModel, contextMessages []*message.Message, tools []ToolSchema, reserveLimit int) ([]*message.Message, []*message.Message, error) {
	if len(contextMessages) <= 1 {
		return nil, cloneMessages(contextMessages), nil
	}
	reserveStart := len(contextMessages) - 1
	used := 0
	for index := len(contextMessages) - 1; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		msg := contextMessages[index]
		if msg == nil {
			reserveStart = index
			continue
		}
		count, err := model.CountTokens(CallRequest{Messages: []*message.Message{msg.Clone()}, Tools: tools})
		if err != nil {
			return nil, nil, err
		}
		if index == len(contextMessages)-1 {
			used += count
			reserveStart = index
			continue
		}
		if reserveLimit > 0 && used+count <= reserveLimit {
			used += count
			reserveStart = index
			continue
		}
		break
	}
	return cloneMessages(contextMessages[:reserveStart]), cloneMessages(contextMessages[reserveStart:]), nil
}

func buildSummaryRequest(ctx context.Context, input *ContextStrategyInput, toCompress []*message.Message) (CallRequest, error) {
	systemPrompt, err := input.SystemPrompt(ctx)
	if err != nil {
		return CallRequest{}, err
	}
	systemMsg, err := message.NewSystemMessage("system", systemPrompt)
	if err != nil {
		return CallRequest{}, err
	}
	messages := []*message.Message{systemMsg}
	if summary := summaryMessageFromState(input.State); summary != nil {
		messages = append(messages, summary)
	}
	messages = append(messages, cloneMessages(toCompress)...)
	prompt := input.Config.CompressionPrompt + "\n\nCall generate_structured_output with a JSON object matching this schema:\n" + marshalSchema(input.Config.SummarySchema)
	compressionMsg, err := message.NewUserMessage("user", prompt)
	if err != nil {
		return CallRequest{}, err
	}
	messages = append(messages, compressionMsg)
	return CallRequest{
		Messages: messages,
		Tools:    []ToolSchema{summaryToolSchema(input.Config.SummarySchema)},
		Metadata: map[string]any{"agentscope.context_strategy": "summary"},
	}, nil
}

func summaryToolSchema(schema map[string]any) ToolSchema {
	return ToolSchema{
		Type: "function",
		Function: FunctionSchema{
			Name:        "generate_structured_output",
			Description: "Generate the structured context summary requested by the user.",
			Parameters:  types.JSONSchema(utils.CloneAnyMap(schema)),
		},
	}
}

func summaryTextFromResponse(response *ChatResponse, config ContextConfig) (string, error) {
	if response == nil {
		return "", fmt.Errorf("agentscope: summary compression returned nil response")
	}
	text := response.GetTextContent("")
	if text == nil || strings.TrimSpace(*text) == "" {
		return "", fmt.Errorf("agentscope: summary compression returned empty response")
	}
	raw := stripJSONFence(strings.TrimSpace(*text))
	values := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return *text, nil
	}
	if len(values) == 0 {
		return *text, nil
	}
	return applySummaryTemplate(config.SummaryTemplate, values), nil
}

func applySummaryTemplate(template string, values map[string]any) string {
	out := template
	for key, value := range values {
		out = strings.ReplaceAll(out, "{"+key+"}", fmt.Sprint(value))
	}
	return out
}

func stripJSONFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

func marshalSchema(schema map[string]any) string {
	data, err := json.Marshal(schema)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func summaryMessageFromState(state *AgentState) *message.Message {
	if state == nil {
		return nil
	}
	switch {
	case state.Summary.Text != "":
		msg, err := message.NewUserMessage("user", state.Summary.Text)
		if err != nil {
			return nil
		}
		return msg
	case len(state.Summary.Blocks) > 0:
		msg, err := message.NewUserMessage("user", state.Summary.Blocks)
		if err != nil {
			return nil
		}
		return msg
	default:
		return nil
	}
}

func cloneMessages(messages []*message.Message) []*message.Message {
	if messages == nil {
		return nil
	}
	out := make([]*message.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, msg.Clone())
	}
	return out
}
