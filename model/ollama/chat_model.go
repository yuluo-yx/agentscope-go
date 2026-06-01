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

// Package ollama provides a chat model backed by the official Ollama Go API client.
package ollama

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	ollamaapi "github.com/ollama/ollama/api"

	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/types"
)

const (
	providerName       = "ollama"
	defaultContextSize = 32768
)

// Credential stores Ollama connection settings.
type Credential struct {
	Host string
}

// CredentialOption configures Ollama credentials.
type CredentialOption func(*Credential)

// WithHost sets the Ollama host URL.
func WithHost(host string) CredentialOption {
	return func(credential *Credential) {
		credential.Host = strings.TrimRight(strings.TrimSpace(host), "/")
	}
}

// NewCredential creates Ollama credentials.
func NewCredential(opts ...CredentialOption) Credential {
	credential := Credential{}
	for _, opt := range opts {
		opt(&credential)
	}
	return credential
}

// ChatParameters stores Ollama generation parameters.
type ChatParameters struct {
	MaxTokens      *int
	ThinkingEnable bool
	Temperature    *float64
}

// Clone returns a copy of parameters.
func (p ChatParameters) Clone() ChatParameters {
	cp := p
	if p.MaxTokens != nil {
		value := *p.MaxTokens
		cp.MaxTokens = &value
	}
	if p.Temperature != nil {
		value := *p.Temperature
		cp.Temperature = &value
	}
	return cp
}

// Validate validates parameter ranges.
func (p ChatParameters) Validate() error {
	if p.MaxTokens != nil && *p.MaxTokens <= 0 {
		return fmt.Errorf("ollama: max tokens must be positive")
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		return fmt.Errorf("ollama: temperature must be between 0 and 2")
	}
	return nil
}

// ChatModel is an Ollama ChatModel implementation.
type ChatModel struct {
	credential  Credential
	model       string
	parameters  ChatParameters
	stream      bool
	contextSize int
	client      *ollamaapi.Client
}

// ChatModelOption configures an Ollama ChatModel.
type ChatModelOption func(*chatModelOptions)

type chatModelOptions struct {
	parameters  ChatParameters
	stream      bool
	contextSize int
	client      *ollamaapi.Client
}

// WithChatParameters sets generation parameters.
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

// NewChatModel creates an Ollama chat model.
func NewChatModel(credential Credential, model string, opts ...ChatModelOption) (*ChatModel, error) {
	options := chatModelOptions{
		stream:      true,
		contextSize: defaultContextSize,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if model == "" {
		return nil, fmt.Errorf("ollama: model is empty")
	}
	if err := options.parameters.Validate(); err != nil {
		return nil, err
	}
	client := options.client
	if client == nil {
		var err error
		client, err = newClient(credential)
		if err != nil {
			return nil, err
		}
	}
	return &ChatModel{
		credential:  credential,
		model:       model,
		parameters:  options.parameters.Clone(),
		stream:      options.stream,
		contextSize: options.contextSize,
		client:      client,
	}, nil
}

// Name returns the provider and model name.
func (m *ChatModel) Name() string {
	if m == nil {
		return providerName + ":<nil>"
	}
	return providerName + ":" + m.model
}

// Call runs a non-streaming Ollama chat call.
func (m *ChatModel) Call(ctx context.Context, request asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("ollama: nil chat model")
	}
	params, err := m.buildRequest(request, false)
	if err != nil {
		return nil, err
	}
	var last ollamaapi.ChatResponse
	if err := m.client.Chat(ctx, &params, func(response ollamaapi.ChatResponse) error {
		last = response
		return nil
	}); err != nil {
		return nil, normalizeError(err)
	}
	return ollamaResponse(last, true), nil
}

// Stream runs a streaming Ollama chat call.
func (m *ChatModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("ollama: nil chat model")
	}
	params, err := m.buildRequest(request, true)
	if err != nil {
		return nil, err
	}
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		acc := &streamAccumulator{}
		if err := m.client.Chat(ctx, &params, func(response ollamaapi.ChatResponse) error {
			acc.add(response)
			if len(responseContent(response.Message)) > 0 {
				if !sendResponse(ctx, out, ollamaResponse(response, false)) {
					return ctx.Err()
				}
			}
			return nil
		}); err != nil {
			sendResponse(ctx, out, streamErrorResponse(normalizeError(err)))
			return
		}
		sendResponse(ctx, out, acc.final())
	}()
	return out, nil
}

// CountTokens returns an approximate token count.
func (m *ChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *ChatModel) buildRequest(request asmodel.CallRequest, stream bool) (ollamaapi.ChatRequest, error) {
	messages, err := formatMessages(request.Messages)
	if err != nil {
		return ollamaapi.ChatRequest{}, err
	}
	tools, err := formatTools(request.Tools, request.ToolChoice)
	if err != nil {
		return ollamaapi.ChatRequest{}, err
	}
	params := ollamaapi.ChatRequest{
		Model:    m.model,
		Messages: messages,
		Stream:   &stream,
		Options:  map[string]any{},
		Tools:    tools,
	}
	if m.parameters.MaxTokens != nil {
		params.Options["num_predict"] = *m.parameters.MaxTokens
	}
	if m.parameters.Temperature != nil {
		params.Options["temperature"] = *m.parameters.Temperature
	}
	if len(params.Options) == 0 {
		params.Options = nil
	}
	if m.parameters.ThinkingEnable {
		params.Think = &ollamaapi.ThinkValue{Value: true}
	}
	return params, nil
}

func newClient(credential Credential) (*ollamaapi.Client, error) {
	if credential.Host == "" {
		return ollamaapi.ClientFromEnvironment()
	}
	parsed, err := url.Parse(credential.Host)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("ollama: invalid host %q", credential.Host)
	}
	return ollamaapi.NewClient(parsed, http.DefaultClient), nil
}

func formatMessages(messages []*message.Message) ([]ollamaapi.Message, error) {
	formatted := make([]ollamaapi.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		role := string(msg.Role)
		contentParts := []string{}
		images := []ollamaapi.ImageData{}
		toolCalls := []ollamaapi.ToolCall{}
		for _, block := range msg.Content {
			switch typed := block.(type) {
			case *message.TextBlock:
				contentParts = append(contentParts, typed.Text)
			case *message.ThinkingBlock:
				contentParts = append(contentParts, typed.Thinking)
			case *message.DataBlock:
				image, ok, err := imageData(typed)
				if err != nil {
					return nil, err
				}
				if ok {
					images = append(images, image)
				}
			case *message.ToolCallBlock:
				call, err := formatToolCall(typed)
				if err != nil {
					return nil, err
				}
				toolCalls = append(toolCalls, call)
			case *message.ToolResultBlock:
				formatted = append(formatted, ollamaapi.Message{
					Role:       "tool",
					Content:    toolResultText(typed.Output),
					ToolName:   typed.Name,
					ToolCallID: typed.ID,
				})
			}
		}
		if len(contentParts) == 0 && len(images) == 0 && len(toolCalls) == 0 {
			continue
		}
		formatted = append(formatted, ollamaapi.Message{
			Role:      role,
			Content:   strings.Join(contentParts, ""),
			Images:    images,
			ToolCalls: toolCalls,
		})
	}
	return formatted, nil
}

func imageData(block *message.DataBlock) (ollamaapi.ImageData, bool, error) {
	if block == nil || block.Source == nil {
		return nil, false, nil
	}
	source, ok := block.Source.(*message.Base64Source)
	if !ok {
		return nil, false, nil
	}
	if !strings.HasPrefix(source.MediaType, "image/") {
		return nil, false, nil
	}
	data, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		return nil, false, err
	}
	return ollamaapi.ImageData(data), true, nil
}

func formatToolCall(block *message.ToolCallBlock) (ollamaapi.ToolCall, error) {
	arguments := ollamaapi.NewToolCallFunctionArguments()
	if strings.TrimSpace(block.Input) != "" {
		var raw map[string]any
		if err := json.Unmarshal([]byte(block.Input), &raw); err != nil {
			return ollamaapi.ToolCall{}, err
		}
		keys := make([]string, 0, len(raw))
		for key := range raw {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			arguments.Set(key, raw[key])
		}
	}
	return ollamaapi.ToolCall{
		ID: block.ID,
		Function: ollamaapi.ToolCallFunction{
			Name:      block.Name,
			Arguments: arguments,
		},
	}, nil
}

func formatTools(tools []asmodel.ToolSchema, choice *types.ToolChoice) (ollamaapi.Tools, error) {
	available := make([]string, 0, len(tools))
	for _, tool := range tools {
		available = append(available, tool.Function.Name)
	}
	if err := choice.Validate(available); err != nil {
		return nil, err
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
	if len(filtered) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	var formatted ollamaapi.Tools
	if err := json.Unmarshal(data, &formatted); err != nil {
		return nil, err
	}
	return formatted, nil
}

func ollamaResponse(response ollamaapi.ChatResponse, isLast bool) *asmodel.ChatResponse {
	opts := []asmodel.ChatResponseOption{asmodel.WithChatResponseUsage(chatUsage(response))}
	if !response.CreatedAt.IsZero() {
		opts = append(opts, asmodel.WithChatResponseCreatedAt(response.CreatedAt.Format(time.RFC3339Nano)))
	}
	return asmodel.NewChatResponse(responseContent(response.Message), isLast, opts...)
}

func responseContent(msg ollamaapi.Message) message.ContentBlockList {
	content := message.ContentBlockList{}
	if msg.Thinking != "" {
		content = append(content, message.NewThinkingBlock(msg.Thinking))
	}
	if msg.Content != "" {
		content = append(content, message.NewTextBlock(msg.Content))
	}
	for _, call := range msg.ToolCalls {
		arguments, _ := json.Marshal(call.Function.Arguments)
		content = append(content, message.NewToolCallBlock(call.ID, call.Function.Name, string(arguments)))
	}
	return content
}

func chatUsage(response ollamaapi.ChatResponse) *asmodel.ChatUsage {
	if response.PromptEvalCount == 0 && response.EvalCount == 0 && response.TotalDuration == 0 {
		return nil
	}
	return &asmodel.ChatUsage{
		InputTokens:  response.PromptEvalCount,
		OutputTokens: response.EvalCount,
		Time:         response.TotalDuration,
		Type:         asmodel.UsageTypeChat,
	}
}

type streamAccumulator struct {
	text  string
	usage *asmodel.ChatUsage
}

func (a *streamAccumulator) add(response ollamaapi.ChatResponse) {
	a.text += response.Message.Content
	if response.Done {
		a.usage = chatUsage(response)
	}
}

func (a *streamAccumulator) final() *asmodel.ChatResponse {
	content := message.ContentBlockList{}
	if a.text != "" {
		content = append(content, message.NewTextBlock(a.text))
	}
	return asmodel.NewChatResponse(content, true, asmodel.WithChatResponseUsage(a.usage))
}

func sendResponse(ctx context.Context, out chan<- asmodel.ChatResponse, response *asmodel.ChatResponse) bool {
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

func toolResultText(output message.ToolResultOutput) string {
	if output.Raw != "" {
		return output.Raw
	}
	text := output.Blocks.GetTextContent("")
	if text == nil {
		return ""
	}
	return *text
}

func normalizeError(err error) error {
	var statusErr ollamaapi.StatusError
	if errors.As(err, &statusErr) {
		return asmodel.NormalizeError(providerName, err, asmodel.WithStatusCode(statusErr.StatusCode))
	}
	var authErr ollamaapi.AuthorizationError
	if errors.As(err, &authErr) {
		return asmodel.NormalizeError(providerName, err, asmodel.WithStatusCode(authErr.StatusCode))
	}
	return asmodel.NormalizeError(providerName, err)
}

var _ asmodel.ChatModel = (*ChatModel)(nil)
