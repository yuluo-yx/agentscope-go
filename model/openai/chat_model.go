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

package openai

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"

	agenterrors "github.com/yuluo-yx/agentscope-go/errors"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/types"
)

// ChatModel is a Chat Completions provider based on openai-go.
type ChatModel struct {
	providerName string
	credential   Credential
	model        string
	parameters   ChatParameters
	stream       bool
	contextSize  int
	client       sdk.Client
}

// ChatModelOption configures an OpenAI ChatModel.
type ChatModelOption func(*chatModelOptions)

type chatModelOptions struct {
	providerName string
	parameters   ChatParameters
	stream       bool
	contextSize  int
	maxRetries   int
}

// WithProviderName sets the logical provider name for OpenAI-compatible wrappers.
func WithProviderName(providerName string) ChatModelOption {
	return func(options *chatModelOptions) {
		options.providerName = providerName
	}
}

// WithChatParameters sets Chat Completions generation parameters.
func WithChatParameters(parameters ChatParameters) ChatModelOption {
	return func(options *chatModelOptions) {
		options.parameters = parameters.Clone()
	}
}

// WithStream records a compatibility stream preference.
// Call and Stream still choose the request transport explicitly.
func WithStream(stream bool) ChatModelOption {
	return func(options *chatModelOptions) {
		options.stream = stream
	}
}

// WithContextSize sets model context length for upper-layer compression.
func WithContextSize(contextSize int) ChatModelOption {
	return func(options *chatModelOptions) {
		options.contextSize = contextSize
	}
}

// WithMaxRetries sets the SDK max retry count.
func WithMaxRetries(maxRetries int) ChatModelOption {
	return func(options *chatModelOptions) {
		options.maxRetries = maxRetries
	}
}

// NewChatModel creates an OpenAI Chat Completions model.
func NewChatModel(credential Credential, model string, opts ...ChatModelOption) (*ChatModel, error) {
	options := chatModelOptions{
		providerName: "openai",
		stream:       true,
		contextSize:  128000,
		maxRetries:   3,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := credential.Validate(); err != nil {
		return nil, agenterrors.NewDeveloperError("invalid OpenAI credential", agenterrors.WithErrorCause(err))
	}
	if model == "" {
		return nil, agenterrors.NewDeveloperError("invalid OpenAI model", agenterrors.WithErrorCause(fmt.Errorf("model is empty")))
	}
	if options.providerName == "" {
		options.providerName = "openai"
	}
	if err := options.parameters.Validate(); err != nil {
		return nil, agenterrors.NewDeveloperError("invalid OpenAI chat parameters", agenterrors.WithErrorCause(err))
	}
	if options.maxRetries <= 0 {
		return nil, agenterrors.NewDeveloperError("invalid OpenAI max retries", agenterrors.WithErrorCause(fmt.Errorf("max retries must be positive")))
	}
	clientOptions := []option.RequestOption{
		option.WithAPIKey(credential.APIKey),
		option.WithMaxRetries(options.maxRetries),
	}
	if credential.BaseURL != "" {
		clientOptions = append(clientOptions, option.WithBaseURL(credential.BaseURL))
	}
	if credential.Organization != "" {
		clientOptions = append(clientOptions, option.WithOrganization(credential.Organization))
	}
	return &ChatModel{
		providerName: options.providerName,
		credential:   credential,
		model:        model,
		parameters:   options.parameters.Clone(),
		stream:       options.stream,
		contextSize:  options.contextSize,
		client:       sdk.NewClient(clientOptions...),
	}, nil
}

// Name returns the provider and model name.
func (m *ChatModel) Name() string {
	if m == nil {
		return "openai:<nil>"
	}
	providerName := m.providerName
	if providerName == "" {
		providerName = "openai"
	}
	return providerName + ":" + m.model
}

// Call runs a non-streaming Chat Completions call.
func (m *ChatModel) Call(ctx context.Context, request asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	params, err := m.buildParams(request)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := m.client.Chat.Completions.New(ctx, params, option.WithJSONSet("stream", false))
	if err != nil {
		return nil, normalizeError(m.providerName, err)
	}
	return parseCompletion(resp, time.Since(start)), nil
}

// Stream runs a streaming Chat Completions call.
func (m *ChatModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	params, err := m.buildParams(request)
	if err != nil {
		return nil, err
	}
	params.StreamOptions = sdk.ChatCompletionStreamOptionsParam{IncludeUsage: sdk.Bool(true)}
	stream := m.client.Chat.Completions.NewStreaming(ctx, params)
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		start := time.Now()
		parseStream(ctx, stream, m.providerName, start, out)
	}()
	return out, nil
}

// CountTokens returns an approximate token count.
func (m *ChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *ChatModel) buildParams(request asmodel.CallRequest) (sdk.ChatCompletionNewParams, error) {
	if m == nil {
		return sdk.ChatCompletionNewParams{}, agenterrors.NewDeveloperError("nil OpenAI chat model")
	}
	messages, err := formatMessages(request.Messages)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, agenterrors.NewDeveloperError("failed to format OpenAI messages", agenterrors.WithErrorCause(err))
	}
	tools, toolChoice, err := formatTools(request.Tools, request.ToolChoice)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, agenterrors.NewDeveloperError("failed to format OpenAI tools", agenterrors.WithErrorCause(err))
	}
	params := sdk.ChatCompletionNewParams{
		Messages: messages,
		Model:    m.model,
	}
	if len(tools) > 0 {
		params.Tools = tools
	}
	if toolChoice != nil {
		params.ToolChoice = *toolChoice
	}
	if m.parameters.MaxTokens != nil {
		params.MaxTokens = sdk.Int(*m.parameters.MaxTokens)
	}
	if m.parameters.Temperature != nil {
		params.Temperature = sdk.Float(*m.parameters.Temperature)
	}
	if m.parameters.TopP != nil {
		params.TopP = sdk.Float(*m.parameters.TopP)
	}
	if m.parameters.ThinkingEnable && m.parameters.ReasoningEffort != "" && m.parameters.ReasoningEffort != "none" && m.parameters.ReasoningEffort != "minimal" && m.parameters.ReasoningEffort != "xhigh" {
		params.ReasoningEffort = shared.ReasoningEffort(m.parameters.ReasoningEffort)
	}
	if m.parameters.ParallelToolCalls != nil {
		params.ParallelToolCalls = sdk.Bool(*m.parameters.ParallelToolCalls)
	}
	return params, nil
}

func formatTools(tools []asmodel.ToolSchema, choice *types.ToolChoice) ([]sdk.ChatCompletionToolParam, *sdk.ChatCompletionToolChoiceOptionUnionParam, error) {
	available := make([]string, 0, len(tools))
	for _, tool := range tools {
		available = append(available, tool.Function.Name)
	}
	if err := choice.Validate(available); err != nil {
		return nil, nil, err
	}
	filtered := tools
	if choice != nil && len(choice.Tools) > 0 {
		allowed := make(map[string]struct{}, len(choice.Tools))
		for _, name := range choice.Tools {
			allowed[name] = struct{}{}
		}
		filtered = nil
		for _, tool := range tools {
			if _, ok := allowed[tool.Function.Name]; ok {
				filtered = append(filtered, tool)
			}
		}
	}
	formatted := make([]sdk.ChatCompletionToolParam, 0, len(filtered))
	for _, tool := range filtered {
		formatted = append(formatted, sdk.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        tool.Function.Name,
				Description: sdk.String(tool.Function.Description),
				Parameters:  shared.FunctionParameters(tool.Function.Parameters),
			},
		})
	}
	if choice == nil {
		return formatted, nil, nil
	}
	switch choice.Mode {
	case string(types.ToolChoiceAuto), string(types.ToolChoiceNone), string(types.ToolChoiceRequired):
		toolChoice := sdk.ChatCompletionToolChoiceOptionUnionParam{OfAuto: sdk.String(choice.Mode)}
		return formatted, &toolChoice, nil
	default:
		toolChoice := sdk.ChatCompletionToolChoiceOptionParamOfChatCompletionNamedToolChoice(sdk.ChatCompletionNamedToolChoiceFunctionParam{Name: choice.Mode})
		return formatted, &toolChoice, nil
	}
}

func parseCompletion(resp *sdk.ChatCompletion, elapsed time.Duration) *asmodel.ChatResponse {
	if resp == nil {
		return asmodel.NewChatResponse(nil, true)
	}
	content := message.ContentBlockList{}
	if len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		if msg.Content != "" {
			content = append(content, message.NewTextBlock(msg.Content))
		}
		for _, call := range msg.ToolCalls {
			content = append(content, message.NewToolCallBlock(call.ID, call.Function.Name, call.Function.Arguments))
		}
	}
	opts := []asmodel.ChatResponseOption{
		asmodel.WithChatResponseID(resp.ID),
		asmodel.WithChatResponseUsage(chatUsage(resp.Usage, elapsed)),
	}
	return asmodel.NewChatResponse(content, true, opts...)
}

type accumulatedToolCall struct {
	id    string
	name  string
	input string
}

type streamAccumulator struct {
	start      time.Time
	responseID string
	usage      *asmodel.ChatUsage
	textID     string
	text       string
	toolCalls  map[int64]*accumulatedToolCall
	toolOrder  []int64
}

func newStreamAccumulator(start time.Time) *streamAccumulator {
	return &streamAccumulator{
		start:     start,
		toolCalls: make(map[int64]*accumulatedToolCall),
	}
}

func parseStream(ctx context.Context, stream interface {
	Next() bool
	Current() sdk.ChatCompletionChunk
	Err() error
	Close() error
}, providerName string, start time.Time, out chan<- asmodel.ChatResponse,
) {
	defer stream.Close()
	acc := newStreamAccumulator(start)
	for stream.Next() {
		if !sendStreamResponse(ctx, out, acc.consume(stream.Current())) {
			return
		}
	}
	if err := stream.Err(); err != nil {
		sendStreamResponse(ctx, out, streamErrorResponse(normalizeError(providerName, err)))
		return
	}
	sendStreamResponse(ctx, out, acc.finalResponse())
}

func (acc *streamAccumulator) consume(chunk sdk.ChatCompletionChunk) *asmodel.ChatResponse {
	acc.captureMetadata(chunk)
	if len(chunk.Choices) == 0 {
		return nil
	}
	content := acc.consumeDelta(chunk.Choices[0].Delta)
	if len(content) == 0 {
		return nil
	}
	return asmodel.NewChatResponse(content, false, asmodel.WithChatResponseID(acc.responseID), asmodel.WithChatResponseUsage(acc.usage))
}

func (acc *streamAccumulator) captureMetadata(chunk sdk.ChatCompletionChunk) {
	if chunk.ID != "" {
		acc.responseID = chunk.ID
	}
	if chunk.Usage.JSON.PromptTokens.Valid() || chunk.Usage.JSON.CompletionTokens.Valid() {
		acc.usage = chatUsage(chunk.Usage, time.Since(acc.start))
	}
}

func (acc *streamAccumulator) consumeDelta(delta sdk.ChatCompletionChunkChoiceDelta) message.ContentBlockList {
	content := message.ContentBlockList{}
	if delta.Content != "" {
		content = append(content, acc.consumeTextDelta(delta.Content))
	}
	for _, toolCall := range delta.ToolCalls {
		content = append(content, acc.consumeToolCallDelta(toolCall))
	}
	return content
}

func (acc *streamAccumulator) consumeTextDelta(text string) *message.TextBlock {
	if acc.textID == "" {
		acc.textID = newResponseBlockID()
	}
	acc.text += text
	return message.NewTextBlock(text, message.WithBlockID(acc.textID))
}

func (acc *streamAccumulator) consumeToolCallDelta(toolCall sdk.ChatCompletionChunkChoiceDeltaToolCall) *message.ToolCallBlock {
	call, ok := acc.toolCalls[toolCall.Index]
	if !ok {
		call = &accumulatedToolCall{id: toolCall.ID, name: toolCall.Function.Name}
		acc.toolCalls[toolCall.Index] = call
		acc.toolOrder = append(acc.toolOrder, toolCall.Index)
	}
	if toolCall.ID != "" {
		call.id = toolCall.ID
	}
	if toolCall.Function.Name != "" {
		call.name = toolCall.Function.Name
	}
	call.input += toolCall.Function.Arguments
	return message.NewToolCallBlock(call.id, call.name, toolCall.Function.Arguments)
}

func (acc *streamAccumulator) finalResponse() *asmodel.ChatResponse {
	finalContent := message.ContentBlockList{}
	if acc.text != "" {
		finalContent = append(finalContent, message.NewTextBlock(acc.text, message.WithBlockID(acc.textID)))
	}
	for _, index := range acc.toolOrder {
		call := acc.toolCalls[index]
		finalContent = append(finalContent, message.NewToolCallBlock(call.id, call.name, call.input))
	}
	return asmodel.NewChatResponse(finalContent, true, asmodel.WithChatResponseID(acc.responseID), asmodel.WithChatResponseUsage(acc.usage))
}

func sendStreamResponse(ctx context.Context, out chan<- asmodel.ChatResponse, response *asmodel.ChatResponse) bool {
	if response == nil {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case out <- *response:
		return true
	}
}

func streamErrorResponse(err error) *asmodel.ChatResponse {
	return asmodel.NewChatResponse(nil, true, asmodel.WithChatResponseError(err))
}

func chatUsage(usage sdk.CompletionUsage, elapsed time.Duration) *asmodel.ChatUsage {
	if !usage.JSON.PromptTokens.Valid() && !usage.JSON.CompletionTokens.Valid() {
		return nil
	}
	return &asmodel.ChatUsage{
		InputTokens:      int(usage.PromptTokens),
		OutputTokens:     int(usage.CompletionTokens),
		Time:             elapsed,
		CacheInputTokens: int(usage.PromptTokensDetails.CachedTokens),
		Type:             asmodel.UsageTypeChat,
	}
}

func normalizeError(providerName string, err error) error {
	if providerName == "" {
		providerName = "openai"
	}
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		return asmodel.NormalizeError(providerName, err, asmodel.WithStatusCode(apiErr.StatusCode), asmodel.WithErrorCode(apiErr.Code))
	}
	return asmodel.NormalizeError(providerName, err)
}

func newResponseBlockID() string {
	return fmt.Sprintf("openai-block-%d", time.Now().UnixNano())
}

var _ asmodel.ChatModel = (*ChatModel)(nil)
