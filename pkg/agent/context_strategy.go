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
	"sort"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
)

const (
	DefaultContextWarningThreshold  = 20000
	DefaultContextCompactThreshold  = 13000
	DefaultContextBlockingThreshold = 3000

	defaultToolResultContextStrategyPriority = 10
	defaultThresholdContextStrategyPriority  = 20
	defaultSummaryContextStrategyPriority    = 30
)

// ContextStrategy applies one context compression or offload step.
type ContextStrategy interface {
	// ContextStrategyName returns a stable strategy name for diagnostics.
	ContextStrategyName() string
	// ApplyContextStrategy mutates the provided Agent state when compression or offload is needed.
	ApplyContextStrategy(context.Context, *ContextStrategyInput) error
}

// ContextStrategyPrioritizer is optionally implemented by strategies that need deterministic ordering.
// Lower priority values run before higher values. Strategies without this interface use priority 0.
type ContextStrategyPrioritizer interface {
	ContextStrategyPriority() int
}

// ContextStrategyShortCircuiter is optionally implemented by strategies that can stop the strategy chain.
// ShouldShortCircuit is evaluated only after ApplyContextStrategy returns nil.
type ContextStrategyShortCircuiter interface {
	ShouldShortCircuit(*ContextStrategyInput) bool
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
			messages = append(messages, cloneMessageForModelInput(msg))
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
		NewThresholdContextStrategy(),
		NewSummaryContextStrategy(),
	}
}

func orderContextStrategies(strategies []ContextStrategy) []ContextStrategy {
	ordered := make([]ContextStrategy, 0, len(strategies))
	for _, strategy := range strategies {
		if strategy != nil {
			ordered = append(ordered, strategy)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return contextStrategyPriority(ordered[i]) < contextStrategyPriority(ordered[j])
	})
	return ordered
}

func contextStrategyPriority(strategy ContextStrategy) int {
	if typed, ok := strategy.(ContextStrategyPrioritizer); ok {
		return typed.ContextStrategyPriority()
	}
	return 0
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

// ContextStrategyPriority returns the default order of the built-in tool-result strategy.
func (ToolResultContextStrategy) ContextStrategyPriority() int {
	return defaultToolResultContextStrategyPriority
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

// ContextStrategyPriority returns the default order of the built-in summary strategy.
func (SummaryContextStrategy) ContextStrategyPriority() int {
	return defaultSummaryContextStrategyPriority
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
	_, err = compactContextWithSummary(ctx, input)
	return err
}

// ThresholdContextStrategy tracks context pressure and progressively warns, compacts, or blocks.
type ThresholdContextStrategy struct {
	WarningThreshold  int
	CompactThreshold  int
	BlockingThreshold int
}

// NewThresholdContextStrategy creates the default threshold-based context strategy.
func NewThresholdContextStrategy() ContextStrategy {
	return ThresholdContextStrategy{}
}

// ContextStrategyName returns the strategy name.
func (ThresholdContextStrategy) ContextStrategyName() string {
	return "threshold"
}

// ContextStrategyPriority returns the default order of the built-in threshold strategy.
func (ThresholdContextStrategy) ContextStrategyPriority() int {
	return defaultThresholdContextStrategyPriority
}

// ApplyContextStrategy applies warning, auto-compact, and blocking thresholds.
func (s ThresholdContextStrategy) ApplyContextStrategy(ctx context.Context, input *ContextStrategyInput) error {
	if !thresholdPreconditionsMet(input) {
		return nil
	}
	strategy, usingDefaults := s.normalized()
	if usingDefaults && input.Config.MaxTokens <= strategy.WarningThreshold {
		return nil
	}
	if err := strategy.validate(); err != nil {
		return err
	}

	usedTokens, remainingTokens, err := currentContextTokens(ctx, input)
	if err != nil {
		return err
	}
	strategy.updateStatus(input, statusLevelForRemaining(remainingTokens, strategy), usedTokens, remainingTokens, "")
	if remainingTokens > strategy.CompactThreshold {
		return nil
	}

	compacted, err := compactContextWithSummary(ctx, input)
	if err != nil {
		return err
	}
	if compacted {
		usedTokens, remainingTokens, err = currentContextTokens(ctx, input)
		if err != nil {
			return err
		}
	}
	if remainingTokens <= strategy.BlockingThreshold {
		message := fmt.Sprintf("context window has %d tokens remaining, below blocking threshold %d", remainingTokens, strategy.BlockingThreshold)
		strategy.updateStatus(input, ContextStatusBlocking, usedTokens, remainingTokens, message)
		return &ContextWindowError{
			Strategy:          strategy.ContextStrategyName(),
			MaxTokens:         input.Config.MaxTokens,
			UsedTokens:        usedTokens,
			RemainingTokens:   remainingTokens,
			BlockingThreshold: strategy.BlockingThreshold,
		}
	}
	if compacted {
		level := statusLevelForRemaining(remainingTokens, strategy)
		if level == ContextStatusWarning {
			level = ContextStatusCompact
		}
		strategy.updateStatus(input, level, usedTokens, remainingTokens, "")
	}
	return nil
}

func (s ThresholdContextStrategy) normalized() (ThresholdContextStrategy, bool) {
	usingDefaults := s.WarningThreshold == 0 && s.CompactThreshold == 0 && s.BlockingThreshold == 0
	if s.WarningThreshold <= 0 {
		s.WarningThreshold = DefaultContextWarningThreshold
	}
	if s.CompactThreshold <= 0 {
		s.CompactThreshold = DefaultContextCompactThreshold
	}
	if s.BlockingThreshold <= 0 {
		s.BlockingThreshold = DefaultContextBlockingThreshold
	}
	return s, usingDefaults
}

func (s ThresholdContextStrategy) validate() error {
	if s.WarningThreshold <= 0 || s.CompactThreshold <= 0 || s.BlockingThreshold <= 0 {
		return fmt.Errorf("agentscope: context thresholds must be positive")
	}
	if s.WarningThreshold <= s.CompactThreshold {
		return fmt.Errorf("agentscope: warning threshold must be greater than compact threshold")
	}
	if s.CompactThreshold <= s.BlockingThreshold {
		return fmt.Errorf("agentscope: compact threshold must be greater than blocking threshold")
	}
	return nil
}

func thresholdPreconditionsMet(input *ContextStrategyInput) bool {
	return input != nil && input.State != nil && input.Model != nil && input.Config.MaxTokens > 0
}

func currentContextTokens(ctx context.Context, input *ContextStrategyInput) (int, int, error) {
	currentRequest, err := input.CurrentModelRequest(ctx)
	if err != nil {
		return 0, 0, err
	}
	usedTokens, err := input.Model.CountTokens(currentRequest)
	if err != nil {
		return 0, 0, err
	}
	return usedTokens, input.Config.MaxTokens - usedTokens, nil
}

func statusLevelForRemaining(remainingTokens int, strategy ThresholdContextStrategy) ContextStatusLevel {
	switch {
	case remainingTokens <= strategy.BlockingThreshold:
		return ContextStatusCompact
	case remainingTokens <= strategy.CompactThreshold:
		return ContextStatusCompact
	case remainingTokens <= strategy.WarningThreshold:
		return ContextStatusWarning
	default:
		return ContextStatusNormal
	}
}

func (s ThresholdContextStrategy) updateStatus(input *ContextStrategyInput, level ContextStatusLevel, usedTokens, remainingTokens int, message string) {
	if input == nil || input.State == nil {
		return
	}
	input.State.ContextStatus = &ContextStatus{
		Level:             level,
		Strategy:          s.ContextStrategyName(),
		MaxTokens:         input.Config.MaxTokens,
		UsedTokens:        usedTokens,
		RemainingTokens:   remainingTokens,
		WarningThreshold:  s.WarningThreshold,
		CompactThreshold:  s.CompactThreshold,
		BlockingThreshold: s.BlockingThreshold,
		Message:           message,
	}
}

// ContextWindowError reports that the context is too full to send safely.
type ContextWindowError struct {
	Strategy          string
	MaxTokens         int
	UsedTokens        int
	RemainingTokens   int
	BlockingThreshold int
}

func (e *ContextWindowError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("agentscope: context window blocked by %s strategy: used=%d max=%d remaining=%d blocking_threshold=%d",
		e.Strategy,
		e.UsedTokens,
		e.MaxTokens,
		e.RemainingTokens,
		e.BlockingThreshold,
	)
}

func compactContextWithSummary(ctx context.Context, input *ContextStrategyInput) (bool, error) {
	if !summaryPreconditionsMet(input) {
		return false, nil
	}
	tools, err := input.ToolSchemas()
	if err != nil {
		return false, err
	}
	reserveLimit := int(float64(input.Config.MaxTokens) * input.Config.ReserveRatio)
	toCompress, toReserve, err := splitContextForSummary(ctx, input.Model, input.State.Context, tools, reserveLimit)
	if err != nil {
		return false, err
	}
	if len(toCompress) == 0 {
		return false, nil
	}
	return true, executeSummary(ctx, input, toCompress, toReserve)
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
	cleanUnreservedReadCache(input.State, toReserve)
	input.State.Context = toReserve
	return nil
}

func (a *Agent) newContextStrategyInput() *ContextStrategyInput {
	toolProvider := a.effectiveToolProvider()
	return &ContextStrategyInput{
		Agent:        a,
		State:        a.state,
		Model:        a.model,
		ToolProvider: toolProvider,
		Offloader:    a.offloader,
		Config:       a.contextConfig,
		activeGroups: a.activeGroups(),
		systemPrompt: a.buildSystemPrompt,
		toolSchemas: func() ([]ToolSchema, error) {
			return toolProvider.ToolSchemas(a.activeGroups()...)
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
	if unresolvedStart := firstUnresolvedToolMessageIndex(contextMessages); unresolvedStart >= 0 && reserveStart > unresolvedStart {
		reserveStart = unresolvedStart
	}
	// Adjust the boundary so tool call and result pairs are not split across
	// the compressed and reserved parts. Moving the boundary can pull another
	// tool call into the compressed part while leaving its result reserved,
	// so repeat until it is stable.
	for {
		unpairedStart := firstUnpairedToolResultIndex(contextMessages, reserveStart)
		if unpairedStart < 0 {
			break
		}
		reserveStart = unpairedStart
	}
	return cloneMessages(contextMessages[:reserveStart]), cloneMessages(contextMessages[reserveStart:]), nil
}

// firstUnpairedToolResultIndex returns the index of the earliest reserved
// message holding a tool result whose matching tool call is not reserved,
// or -1 when every reserved tool result is paired.
func firstUnpairedToolResultIndex(messages []*message.Message, reserveStart int) int {
	reservedCallIDs := map[string]struct{}{}
	for _, msg := range messages[reserveStart:] {
		if msg == nil {
			continue
		}
		for _, block := range msg.GetContentBlocks("tool_call") {
			if toolCall, ok := block.(*message.ToolCallBlock); ok {
				reservedCallIDs[toolCall.ID] = struct{}{}
			}
		}
	}
	resultIndex := -1
	for index := reserveStart; index < len(messages); index++ {
		msg := messages[index]
		if msg == nil {
			continue
		}
		for _, block := range msg.GetContentBlocks("tool_result") {
			result, ok := block.(*message.ToolResultBlock)
			if !ok {
				continue
			}
			if _, paired := reservedCallIDs[result.ID]; !paired && resultIndex < 0 {
				resultIndex = index
			}
		}
	}
	return resultIndex
}

func firstUnresolvedToolMessageIndex(messages []*message.Message) int {
	for index, msg := range messages {
		if hasUnresolvedToolWork(msg) {
			return index
		}
	}
	return -1
}

func hasUnresolvedToolWork(msg *message.Message) bool {
	if msg == nil {
		return false
	}
	resultIDs := map[string]message.ToolResultState{}
	for _, block := range msg.GetContentBlocks("tool_result") {
		result, ok := block.(*message.ToolResultBlock)
		if ok {
			resultIDs[result.ID] = result.State
		}
	}
	for _, block := range msg.GetContentBlocks("tool_call") {
		toolCall, ok := block.(*message.ToolCallBlock)
		if !ok {
			continue
		}
		if toolCall.State == message.ToolCallFinished {
			continue
		}
		resultState, hasResult := resultIDs[toolCall.ID]
		if !hasResult || resultState == message.ToolResultRunning || resultState == message.ToolResultInterrupted {
			return true
		}
	}
	for _, state := range resultIDs {
		if state == message.ToolResultRunning || state == message.ToolResultInterrupted {
			return true
		}
	}
	return false
}

func cleanUnreservedReadCache(state *AgentState, reserved []*message.Message) {
	if state == nil || state.ToolContext == nil {
		return
	}
	state.ToolContext.CleanFileCache(readFilePathsFromMessages(reserved)...)
}

func readFilePathsFromMessages(messages []*message.Message) []string {
	seen := map[string]struct{}{}
	var paths []string
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		for _, block := range msg.GetContentBlocks("tool_call") {
			toolCall, ok := block.(*message.ToolCallBlock)
			if !ok || toolCall.Name != "Read" {
				continue
			}
			input := map[string]any{}
			if err := json.Unmarshal([]byte(toolCall.Input), &input); err != nil {
				continue
			}
			filePath, ok := input["file_path"].(string)
			if !ok || filePath == "" {
				continue
			}
			if _, exists := seen[filePath]; exists {
				continue
			}
			seen[filePath] = struct{}{}
			paths = append(paths, filePath)
		}
	}
	return paths
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
	if text != nil && strings.TrimSpace(*text) != "" {
		return summaryTextFromRaw(strings.TrimSpace(*text), config), nil
	}
	for _, block := range response.GetContentBlocks("tool_call") {
		toolCall, ok := block.(*message.ToolCallBlock)
		if !ok || toolCall == nil || toolCall.Name != "generate_structured_output" || strings.TrimSpace(toolCall.Input) == "" {
			continue
		}
		return summaryTextFromRaw(strings.TrimSpace(toolCall.Input), config), nil
	}
	return "", fmt.Errorf("agentscope: summary compression returned empty response")
}

func summaryTextFromRaw(raw string, config ContextConfig) string {
	raw = stripJSONFence(raw)
	values := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return raw
	}
	if len(values) == 0 {
		return raw
	}
	return applySummaryTemplate(config.SummaryTemplate, values)
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
		out = append(out, cloneMessageForModelInput(msg))
	}
	return out
}
