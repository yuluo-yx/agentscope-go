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

// Package zhipu provides an OpenAI-compatible Zhipu AI chat model.
package zhipu

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/openai"
	"github.com/yuluo-yx/agentscope-go/types"
)

const (
	defaultBaseURL     = "https://open.bigmodel.cn/api/paas/v4"
	defaultContextSize = 131072
)

type (
	// Credential configures Zhipu AI authentication and endpoint settings.
	Credential = openai.Credential
	// CredentialOption customizes Zhipu AI credentials.
	CredentialOption = openai.CredentialOption
	// ChatParameters configures Zhipu AI chat completion parameters.
	ChatParameters = openai.ChatParameters
)

// ChatModelOption customizes a Zhipu AI ChatModel.
type ChatModelOption func(*chatModelOptions)

type chatModelOptions struct {
	parameters  ChatParameters
	stream      bool
	contextSize int
	maxRetries  int
	httpClient  *http.Client
}

// ChatModel implements Zhipu AI's OpenAI-compatible Chat Completions API.
type ChatModel struct {
	credential  Credential
	model       string
	parameters  ChatParameters
	stream      bool
	contextSize int
	maxRetries  int
	httpClient  *http.Client
}

// NewCredential creates Zhipu AI credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	options := append([]CredentialOption{openai.WithBaseURL(defaultBaseURL)}, opts...)
	return openai.NewCredential(apiKey, options...)
}

// WithBaseURL overrides the default Zhipu AI OpenAI-compatible endpoint.
func WithBaseURL(baseURL string) CredentialOption { return openai.WithBaseURL(baseURL) }

// WithChatParameters sets default Zhipu AI chat parameters.
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

// WithContextSize overrides the model context size used for validation.
func WithContextSize(contextSize int) ChatModelOption {
	return func(options *chatModelOptions) {
		options.contextSize = contextSize
	}
}

// WithMaxRetries sets the maximum retry count reserved for compatible callers.
func WithMaxRetries(maxRetries int) ChatModelOption {
	return func(options *chatModelOptions) {
		options.maxRetries = maxRetries
	}
}

// WithHTTPClient overrides the HTTP client used by the provider.
func WithHTTPClient(client *http.Client) ChatModelOption {
	return func(options *chatModelOptions) {
		options.httpClient = client
	}
}

// NewChatModel creates a Zhipu AI chat model.
func NewChatModel(credential Credential, model string, opts ...ChatModelOption) (*ChatModel, error) {
	options := chatModelOptions{
		stream:      true,
		contextSize: defaultContextSize,
		maxRetries:  3,
		httpClient:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := credential.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Zhipu credential: %w", err)
	}
	if model == "" {
		return nil, fmt.Errorf("invalid Zhipu model: model is empty")
	}
	if err := options.parameters.Validate(); err != nil {
		return nil, err
	}
	if options.maxRetries <= 0 {
		return nil, fmt.Errorf("zhipu: max retries must be positive")
	}
	if options.httpClient == nil {
		options.httpClient = http.DefaultClient
	}
	return &ChatModel{
		credential:  credential,
		model:       model,
		parameters:  options.parameters.Clone(),
		stream:      options.stream,
		contextSize: options.contextSize,
		maxRetries:  options.maxRetries,
		httpClient:  options.httpClient,
	}, nil
}

// Name returns the provider-qualified model name.
func (m *ChatModel) Name() string {
	if m == nil {
		return providerName + ":<nil>"
	}
	return providerName + ":" + m.model
}

// Call sends a non-streaming chat request.
func (m *ChatModel) Call(ctx context.Context, request asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	body, err := m.buildRequest(request, false)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := m.post(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, normalizeHTTPError(resp)
	}
	var payload zhipuCompletion
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.chatResponse(time.Since(start)), nil
}

// Stream sends a streaming chat request.
func (m *ChatModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	body, err := m.buildRequest(request, true)
	if err != nil {
		return nil, err
	}
	resp, err := m.post(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		defer resp.Body.Close()
		return nil, normalizeHTTPError(resp)
	}
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		parseZhipuStream(ctx, resp.Body, time.Now(), out)
	}()
	return out, nil
}

// CountTokens estimates token usage for a request.
func (m *ChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	if m == nil {
		return 0, fmt.Errorf("zhipu: nil chat model")
	}
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *ChatModel) buildRequest(request asmodel.CallRequest, stream bool) (map[string]any, error) {
	if m == nil {
		return nil, fmt.Errorf("zhipu: nil chat model")
	}
	messages, err := zhipuMessages(request.Messages)
	if err != nil {
		return nil, err
	}
	tools, toolChoice, err := zhipuTools(request.Tools, request.ToolChoice)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":    m.model,
		"messages": messages,
		"stream":   stream,
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if toolChoice != nil {
		body["tool_choice"] = toolChoice
	}
	if m.parameters.MaxTokens != nil {
		body["max_tokens"] = *m.parameters.MaxTokens
	}
	if m.parameters.Temperature != nil {
		body["temperature"] = *m.parameters.Temperature
	}
	if m.parameters.TopP != nil {
		body["top_p"] = *m.parameters.TopP
	}
	if m.parameters.ThinkingEnable {
		body["thinking"] = map[string]any{"type": "enabled"}
	}
	if m.parameters.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *m.parameters.ParallelToolCalls
	}
	if stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	return body, nil
}

func (m *ChatModel) post(ctx context.Context, body map[string]any) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < m.maxRetries; attempt++ {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(m.credential.BaseURL, "/")+"/chat/completions", bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+m.credential.APIKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := m.httpClient.Do(req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			lastErr = err
		} else if !isRetryableStatus(resp.StatusCode) || attempt == m.maxRetries-1 {
			return resp, nil
		} else {
			resp.Body.Close()
			lastErr = fmt.Errorf("zhipu: retryable status %d", resp.StatusCode)
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, asmodel.NormalizeError(providerName, lastErr)
}

func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func zhipuMessages(messages []*message.Message) ([]map[string]any, error) {
	formatted := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		parts, toolCalls, toolResults, err := splitZhipuContent(msg.Content)
		if err != nil {
			return nil, err
		}
		if len(toolResults) > 0 {
			if len(parts) > 0 || len(toolCalls) > 0 {
				formatted = append(formatted, zhipuAssistantMessage(strings.Join(parts, ""), toolCalls))
			}
			for _, result := range toolResults {
				formatted = append(formatted, map[string]any{
					"role":         "tool",
					"tool_call_id": result.ID,
					"content":      toolResultText(result.Output),
				})
			}
			continue
		}
		switch msg.Role {
		case message.RoleSystem, message.RoleUser:
			item := map[string]any{"role": string(msg.Role), "content": strings.Join(parts, "")}
			if msg.Name != "" {
				item["name"] = msg.Name
			}
			formatted = append(formatted, item)
		case message.RoleAssistant:
			formatted = append(formatted, zhipuAssistantMessage(strings.Join(parts, ""), toolCalls))
		default:
			return nil, fmt.Errorf("zhipu: unsupported message role %q", msg.Role)
		}
	}
	return formatted, nil
}

func splitZhipuContent(blocks message.ContentBlockList) ([]string, []map[string]any, []*message.ToolResultBlock, error) {
	texts := make([]string, 0, len(blocks))
	toolCalls := []map[string]any{}
	toolResults := []*message.ToolResultBlock{}
	for _, block := range blocks {
		switch typed := block.(type) {
		case *message.TextBlock:
			texts = append(texts, typed.Text)
		case *message.ThinkingBlock:
			continue
		case *message.ToolCallBlock:
			toolCalls = append(toolCalls, map[string]any{
				"id":   typed.ID,
				"type": "function",
				"function": map[string]any{
					"name":      typed.Name,
					"arguments": typed.Input,
				},
			})
		case *message.ToolResultBlock:
			toolResults = append(toolResults, typed)
		default:
			return nil, nil, nil, fmt.Errorf("zhipu: unsupported content block %T", block)
		}
	}
	return texts, toolCalls, toolResults, nil
}

func zhipuAssistantMessage(content string, toolCalls []map[string]any) map[string]any {
	msg := map[string]any{"role": "assistant", "content": content}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	return msg
}

func zhipuTools(tools []asmodel.ToolSchema, choice *types.ToolChoice) ([]map[string]any, any, error) {
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
	formatted := make([]map[string]any, 0, len(filtered))
	for _, tool := range filtered {
		formatted = append(formatted, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Function.Name,
				"description": tool.Function.Description,
				"parameters":  tool.Function.Parameters,
			},
		})
	}
	if choice == nil {
		return formatted, nil, nil
	}
	switch types.ToolChoiceMode(choice.Mode) {
	case types.ToolChoiceAuto, types.ToolChoiceNone, types.ToolChoiceRequired:
		return formatted, choice.Mode, nil
	default:
		return formatted, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": choice.Mode},
		}, nil
	}
}

type zhipuCompletion struct {
	ID      string         `json:"id"`
	Choices []zhipuChoice  `json:"choices"`
	Usage   zhipuUsageData `json:"usage"`
}

type zhipuChoice struct {
	Message zhipuMessage `json:"message"`
	Delta   zhipuMessage `json:"delta"`
}

type zhipuMessage struct {
	ReasoningContent string          `json:"reasoning_content"`
	Content          string          `json:"content"`
	ToolCalls        []zhipuToolCall `json:"tool_calls"`
}

type zhipuToolCall struct {
	Index    int               `json:"index"`
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Function zhipuToolFunction `json:"function"`
}

type zhipuToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type zhipuUsageData struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (payload zhipuCompletion) chatResponse(elapsed time.Duration) *asmodel.ChatResponse {
	content := message.ContentBlockList{}
	if len(payload.Choices) > 0 {
		content = appendZhipuContent(content, payload.Choices[0].Message)
	}
	return asmodel.NewChatResponse(content, true, asmodel.WithChatResponseID(payload.ID), asmodel.WithChatResponseUsage(payload.Usage.chatUsage(elapsed)))
}

func appendZhipuContent(content message.ContentBlockList, msg zhipuMessage) message.ContentBlockList {
	if msg.ReasoningContent != "" {
		content = append(content, message.NewThinkingBlock(msg.ReasoningContent))
	}
	if msg.Content != "" {
		content = append(content, message.NewTextBlock(msg.Content))
	}
	for _, call := range msg.ToolCalls {
		content = append(content, message.NewToolCallBlock(call.ID, call.Function.Name, call.Function.Arguments))
	}
	return content
}

func (usage zhipuUsageData) chatUsage(elapsed time.Duration) *asmodel.ChatUsage {
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
		return nil
	}
	return &asmodel.ChatUsage{
		InputTokens:      usage.PromptTokens,
		OutputTokens:     usage.CompletionTokens,
		CacheInputTokens: usage.PromptTokensDetails.CachedTokens,
		Time:             elapsed,
		Type:             asmodel.UsageTypeChat,
	}
}

type zhipuStreamAccumulator struct {
	start      time.Time
	responseID string
	usage      *asmodel.ChatUsage
	thinkingID string
	thinking   string
	textID     string
	text       string
	toolCalls  map[int]*accumulatedToolCall
	toolOrder  []int
}

type accumulatedToolCall struct {
	id    string
	name  string
	input string
}

func newZhipuStreamAccumulator(start time.Time) *zhipuStreamAccumulator {
	return &zhipuStreamAccumulator{
		start:     start,
		toolCalls: map[int]*accumulatedToolCall{},
	}
}

func parseZhipuStream(ctx context.Context, body io.Reader, start time.Time, out chan<- asmodel.ChatResponse) {
	acc := newZhipuStreamAccumulator(start)
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var chunk zhipuCompletion
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			sendStreamResponse(ctx, out, streamErrorResponse(err))
			return
		}
		if response := acc.consume(chunk); !sendStreamResponse(ctx, out, response) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		sendStreamResponse(ctx, out, streamErrorResponse(err))
		return
	}
	sendStreamResponse(ctx, out, acc.finalResponse())
}

func (acc *zhipuStreamAccumulator) consume(chunk zhipuCompletion) *asmodel.ChatResponse {
	if chunk.ID != "" {
		acc.responseID = chunk.ID
	}
	if usage := chunk.Usage.chatUsage(time.Since(acc.start)); usage != nil {
		acc.usage = usage
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	content := message.ContentBlockList{}
	delta := chunk.Choices[0].Delta
	if delta.ReasoningContent != "" {
		if acc.thinkingID == "" {
			acc.thinkingID = newZhipuBlockID()
		}
		acc.thinking += delta.ReasoningContent
		content = append(content, message.NewThinkingBlock(delta.ReasoningContent, message.WithThinkingBlockID(acc.thinkingID)))
	}
	if delta.Content != "" {
		if acc.textID == "" {
			acc.textID = newZhipuBlockID()
		}
		acc.text += delta.Content
		content = append(content, message.NewTextBlock(delta.Content, message.WithBlockID(acc.textID)))
	}
	for _, toolCall := range delta.ToolCalls {
		content = append(content, acc.consumeToolCall(toolCall))
	}
	if len(content) == 0 {
		return nil
	}
	return asmodel.NewChatResponse(content, false, asmodel.WithChatResponseID(acc.responseID), asmodel.WithChatResponseUsage(acc.usage))
}

func (acc *zhipuStreamAccumulator) consumeToolCall(toolCall zhipuToolCall) *message.ToolCallBlock {
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

func (acc *zhipuStreamAccumulator) finalResponse() *asmodel.ChatResponse {
	content := message.ContentBlockList{}
	if acc.thinking != "" {
		content = append(content, message.NewThinkingBlock(acc.thinking, message.WithThinkingBlockID(acc.thinkingID)))
	}
	if acc.text != "" {
		content = append(content, message.NewTextBlock(acc.text, message.WithBlockID(acc.textID)))
	}
	for _, index := range acc.toolOrder {
		call := acc.toolCalls[index]
		content = append(content, message.NewToolCallBlock(call.id, call.name, call.input))
	}
	return asmodel.NewChatResponse(content, true, asmodel.WithChatResponseID(acc.responseID), asmodel.WithChatResponseUsage(acc.usage))
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

func normalizeHTTPError(resp *http.Response) error {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	data, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(data, &payload)
	msg := payload.Error.Message
	if msg == "" {
		msg = strings.TrimSpace(string(data))
	}
	if msg == "" {
		msg = resp.Status
	}
	return asmodel.NormalizeError(providerName, errors.New(msg), asmodel.WithStatusCode(resp.StatusCode), asmodel.WithErrorCode(payload.Error.Code))
}

func toolResultText(output message.ToolResultOutput) string {
	if output.Raw != "" {
		return output.Raw
	}
	if len(output.Blocks) == 0 {
		return ""
	}
	text := output.Blocks.GetTextContent("")
	if text == nil {
		return ""
	}
	return *text
}

func newZhipuBlockID() string {
	return fmt.Sprintf("zhipu-block-%d", time.Now().UnixNano())
}

var _ asmodel.ChatModel = (*ChatModel)(nil)
