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

package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	agenterrors "github.com/yuluo-yx/agentscope-go/errors"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

// ChatModel is a Messages API provider based on anthropic-sdk-go.
type ChatModel struct {
	credential  Credential
	model       string
	parameters  ChatParameters
	stream      bool
	contextSize int
	client      sdk.Client
}

// ChatModelOption configures an Anthropic ChatModel.
type ChatModelOption func(*chatModelOptions)

type chatModelOptions struct {
	parameters  ChatParameters
	stream      bool
	contextSize int
	maxRetries  int
}

// WithChatParameters sets Anthropic Messages API generation parameters.
func WithChatParameters(parameters ChatParameters) ChatModelOption {
	return func(options *chatModelOptions) {
		options.parameters = parameters.Clone()
	}
}

// WithStream sets whether streaming is used by default.
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

// NewChatModel creates an Anthropic Messages model.
func NewChatModel(credential Credential, model string, opts ...ChatModelOption) (*ChatModel, error) {
	options := chatModelOptions{
		stream:      true,
		contextSize: 200000,
		maxRetries:  3,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := credential.Validate(); err != nil {
		return nil, agenterrors.NewDeveloperError("invalid Anthropic credential", agenterrors.WithErrorCause(err))
	}
	if model == "" {
		return nil, agenterrors.NewDeveloperError("invalid Anthropic model", agenterrors.WithErrorCause(fmt.Errorf("model is empty")))
	}
	if err := options.parameters.Validate(); err != nil {
		return nil, agenterrors.NewDeveloperError("invalid Anthropic chat parameters", agenterrors.WithErrorCause(err))
	}
	if options.maxRetries <= 0 {
		return nil, agenterrors.NewDeveloperError("invalid Anthropic max retries", agenterrors.WithErrorCause(fmt.Errorf("max retries must be positive")))
	}
	clientOptions := []option.RequestOption{
		option.WithAPIKey(credential.APIKey),
		option.WithMaxRetries(options.maxRetries),
	}
	if credential.BaseURL != "" {
		clientOptions = append(clientOptions, option.WithBaseURL(credential.BaseURL))
	}
	return &ChatModel{
		credential:  credential,
		model:       model,
		parameters:  options.parameters.Clone(),
		stream:      options.stream,
		contextSize: options.contextSize,
		client:      sdk.NewClient(clientOptions...),
	}, nil
}

// Name returns the provider and model name.
func (m *ChatModel) Name() string {
	if m == nil {
		return "anthropic:<nil>"
	}
	return "anthropic:" + m.model
}

// Call runs a non-streaming Anthropic Messages call.
func (m *ChatModel) Call(ctx context.Context, request asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	params, err := m.buildParams(request)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := m.client.Messages.New(ctx, params)
	if err != nil {
		return nil, normalizeError(err)
	}
	return parseMessage(resp, time.Since(start)), nil
}

// Stream runs a streaming Anthropic Messages call.
func (m *ChatModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	params, err := m.buildParams(request)
	if err != nil {
		return nil, err
	}
	stream := m.client.Messages.NewStreaming(ctx, params)
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		start := time.Now()
		parseStream(ctx, stream, start, out)
	}()
	return out, nil
}

// CountTokens returns an approximate token count.
func (m *ChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *ChatModel) buildParams(request asmodel.CallRequest) (sdk.MessageNewParams, error) {
	if m == nil {
		return sdk.MessageNewParams{}, agenterrors.NewDeveloperError("nil Anthropic chat model")
	}
	system, messages, err := formatMessages(request.Messages)
	if err != nil {
		return sdk.MessageNewParams{}, agenterrors.NewDeveloperError("failed to format Anthropic messages", agenterrors.WithErrorCause(err))
	}
	tools, toolChoice, err := formatTools(request.Tools, request.ToolChoice)
	if err != nil {
		return sdk.MessageNewParams{}, agenterrors.NewDeveloperError("failed to format Anthropic tools", agenterrors.WithErrorCause(err))
	}
	params := sdk.MessageNewParams{
		MaxTokens: resolvedMaxTokens(m.parameters),
		Messages:  messages,
		Model:     m.model,
		System:    system,
		Tools:     tools,
	}
	if toolChoice != nil {
		params.ToolChoice = *toolChoice
	}
	if m.parameters.Temperature != nil {
		params.Temperature = sdk.Float(*m.parameters.Temperature)
	}
	if m.parameters.TopP != nil {
		params.TopP = sdk.Float(*m.parameters.TopP)
	}
	if m.parameters.TopK != nil {
		params.TopK = sdk.Int(*m.parameters.TopK)
	}
	if m.parameters.ThinkingBudgetTokens != nil {
		params.Thinking = sdk.ThinkingConfigParamOfEnabled(*m.parameters.ThinkingBudgetTokens)
		if m.parameters.ThinkingDisplay != "" {
			params.Thinking.OfEnabled.Display = sdk.ThinkingConfigEnabledDisplay(m.parameters.ThinkingDisplay)
		}
	}
	return params, nil
}

func resolvedMaxTokens(parameters ChatParameters) int64 {
	if parameters.MaxTokens != nil {
		return *parameters.MaxTokens
	}
	return defaultMaxTokens
}

func parseMessage(resp *sdk.Message, elapsed time.Duration) *asmodel.ChatResponse {
	if resp == nil {
		return asmodel.NewChatResponse(nil, true)
	}
	return asmodel.NewChatResponse(
		parseContent(resp.Content),
		true,
		asmodel.WithChatResponseID(resp.ID),
		asmodel.WithChatResponseUsage(usageFromMessage(resp.Usage, elapsed)),
	)
}

func parseContent(blocks []sdk.ContentBlockUnion) message.ContentBlockList {
	content := message.ContentBlockList{}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			content = append(content, message.NewTextBlock(block.Text))
		case "thinking":
			content = append(content, message.NewThinkingBlock(block.Thinking, message.WithExtra("signature", block.Signature)))
		case "tool_use":
			content = append(content, message.NewToolCallBlock(block.ID, block.Name, compactJSON(block.Input)))
		}
	}
	return content
}

type streamBlock struct {
	blockType string
	id        string
	name      string
	blockID   string
	text      string
	thinking  string
	signature string
	input     string
}

type streamAccumulator struct {
	start      time.Time
	responseID string
	usage      *asmodel.ChatUsage
	blocks     map[int64]*streamBlock
	order      []int64
}

func newStreamAccumulator(start time.Time) *streamAccumulator {
	return &streamAccumulator{
		start:  start,
		blocks: make(map[int64]*streamBlock),
	}
}

func parseStream(ctx context.Context, stream interface {
	Next() bool
	Current() sdk.MessageStreamEventUnion
	Err() error
	Close() error
}, start time.Time, out chan<- asmodel.ChatResponse,
) {
	defer stream.Close()
	acc := newStreamAccumulator(start)
	for stream.Next() {
		if !sendStreamResponse(ctx, out, acc.consume(stream.Current())) {
			return
		}
	}
	sendStreamResponse(ctx, out, acc.finalResponse())
}

func (acc *streamAccumulator) consume(event sdk.MessageStreamEventUnion) *asmodel.ChatResponse {
	switch event.Type {
	case "message_start":
		acc.captureMessageStart(event.Message)
	case "message_delta":
		acc.usage = usageFromDelta(event.Usage, time.Since(acc.start))
	case "content_block_start":
		acc.startBlock(event.Index, event.ContentBlock)
	case "content_block_delta":
		return acc.consumeDelta(event.Index, event.Delta)
	}
	return nil
}

func (acc *streamAccumulator) captureMessageStart(msg sdk.Message) {
	if msg.ID != "" {
		acc.responseID = msg.ID
	}
	if usage := usageFromMessage(msg.Usage, time.Since(acc.start)); usage != nil {
		acc.usage = usage
	}
}

func (acc *streamAccumulator) startBlock(index int64, block sdk.ContentBlockStartEventContentBlockUnion) {
	if _, ok := acc.blocks[index]; ok {
		return
	}
	acc.blocks[index] = &streamBlock{
		blockType: block.Type,
		id:        block.ID,
		name:      block.Name,
		blockID:   newResponseBlockID(),
		text:      block.Text,
		thinking:  block.Thinking,
		signature: block.Signature,
	}
	acc.order = append(acc.order, index)
}

func (acc *streamAccumulator) consumeDelta(index int64, delta sdk.MessageStreamEventUnionDelta) *asmodel.ChatResponse {
	block := acc.ensureBlock(index, delta.Type)
	switch delta.Type {
	case "text_delta":
		block.text += delta.Text
		return acc.deltaResponse(message.ContentBlockList{message.NewTextBlock(delta.Text, message.WithBlockID(block.blockID))})
	case "thinking_delta":
		block.thinking += delta.Thinking
		return acc.deltaResponse(message.ContentBlockList{message.NewThinkingBlock(delta.Thinking, message.WithThinkingBlockID(block.blockID))})
	case "signature_delta":
		block.signature += delta.Signature
	case "input_json_delta":
		block.input += delta.PartialJSON
		return acc.deltaResponse(message.ContentBlockList{message.NewToolCallBlock(block.id, block.name, delta.PartialJSON)})
	}
	return nil
}

func (acc *streamAccumulator) ensureBlock(index int64, deltaType string) *streamBlock {
	if block, ok := acc.blocks[index]; ok {
		return block
	}
	block := &streamBlock{blockID: newResponseBlockID(), blockType: blockTypeFromDelta(deltaType)}
	acc.blocks[index] = block
	acc.order = append(acc.order, index)
	return block
}

func (acc *streamAccumulator) deltaResponse(content message.ContentBlockList) *asmodel.ChatResponse {
	return asmodel.NewChatResponse(content, false, asmodel.WithChatResponseID(acc.responseID), asmodel.WithChatResponseUsage(acc.usage))
}

func (acc *streamAccumulator) finalResponse() *asmodel.ChatResponse {
	content := message.ContentBlockList{}
	for _, index := range acc.order {
		if block := acc.finalBlock(index); block != nil {
			content = append(content, block)
		}
	}
	return asmodel.NewChatResponse(content, true, asmodel.WithChatResponseID(acc.responseID), asmodel.WithChatResponseUsage(acc.usage))
}

func (acc *streamAccumulator) finalBlock(index int64) message.ContentBlock {
	block := acc.blocks[index]
	switch block.blockType {
	case "text":
		if block.text != "" {
			return message.NewTextBlock(block.text, message.WithBlockID(block.blockID))
		}
	case "thinking":
		if block.thinking != "" {
			return message.NewThinkingBlock(block.thinking, message.WithThinkingBlockID(block.blockID), message.WithExtra("signature", block.signature))
		}
	case "tool_use":
		return message.NewToolCallBlock(block.id, block.name, block.input)
	}
	return nil
}

func blockTypeFromDelta(deltaType string) string {
	switch deltaType {
	case "text_delta":
		return "text"
	case "thinking_delta", "signature_delta":
		return "thinking"
	case "input_json_delta":
		return "tool_use"
	default:
		return ""
	}
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

func usageFromMessage(usage sdk.Usage, elapsed time.Duration) *asmodel.ChatUsage {
	if !usage.JSON.InputTokens.Valid() && !usage.JSON.OutputTokens.Valid() {
		return nil
	}
	return &asmodel.ChatUsage{
		InputTokens:              int(usage.InputTokens),
		OutputTokens:             int(usage.OutputTokens),
		Time:                     elapsed,
		CacheCreationInputTokens: int(usage.CacheCreationInputTokens),
		CacheInputTokens:         int(usage.CacheReadInputTokens),
		Type:                     asmodel.UsageTypeChat,
	}
}

func usageFromDelta(usage sdk.MessageDeltaUsage, elapsed time.Duration) *asmodel.ChatUsage {
	if !usage.JSON.InputTokens.Valid() && !usage.JSON.OutputTokens.Valid() {
		return nil
	}
	return &asmodel.ChatUsage{
		InputTokens:              int(usage.InputTokens),
		OutputTokens:             int(usage.OutputTokens),
		Time:                     elapsed,
		CacheCreationInputTokens: int(usage.CacheCreationInputTokens),
		CacheInputTokens:         int(usage.CacheReadInputTokens),
		Type:                     asmodel.UsageTypeChat,
	}
}

func normalizeError(err error) error {
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		return asmodel.NormalizeError("anthropic", err, asmodel.WithStatusCode(apiErr.StatusCode), asmodel.WithErrorCode(string(apiErr.Type())))
	}
	return asmodel.NormalizeError("anthropic", err)
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return string(raw)
	}
	return out.String()
}

func newResponseBlockID() string {
	return fmt.Sprintf("anthropic-block-%d", time.Now().UnixNano())
}

var _ asmodel.ChatModel = (*ChatModel)(nil)
