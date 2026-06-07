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

package openairesponse

import (
	"bufio"
	"bytes"
	"context"
	"embed"
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
	responseProviderName = "openai_response"
	defaultResponsesURL  = "https://api.openai.com/v1"
)

//go:embed models/*.yaml
var responseModelFS embed.FS

type (
	// Credential configures OpenAI Responses authentication and endpoint settings.
	Credential = openai.Credential
	// CredentialOption customizes OpenAI Responses credentials.
	CredentialOption = openai.CredentialOption
)

// NewCredential creates OpenAI Responses credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	return openai.NewCredential(apiKey, opts...)
}

// WithBaseURL overrides the OpenAI Responses API endpoint.
func WithBaseURL(baseURL string) CredentialOption { return openai.WithBaseURL(baseURL) }

// WithOrganization sets the OpenAI organization ID.
func WithOrganization(organization string) CredentialOption {
	return openai.WithOrganization(organization)
}

// ResponseParameters stores OpenAI Responses API generation parameters.
type ResponseParameters struct {
	MaxTokens         *int64
	ThinkingEnable    bool
	ReasoningEffort   string
	Temperature       *float64
	Store             *bool
	ParallelToolCalls *bool
}

// Clone returns a parameter copy.
func (p ResponseParameters) Clone() ResponseParameters {
	cp := p
	if p.MaxTokens != nil {
		value := *p.MaxTokens
		cp.MaxTokens = &value
	}
	if p.Temperature != nil {
		value := *p.Temperature
		cp.Temperature = &value
	}
	if p.Store != nil {
		value := *p.Store
		cp.Store = &value
	}
	if p.ParallelToolCalls != nil {
		value := *p.ParallelToolCalls
		cp.ParallelToolCalls = &value
	}
	return cp
}

// Validate validates Responses API parameter ranges.
func (p ResponseParameters) Validate() error {
	if p.MaxTokens != nil && *p.MaxTokens <= 0 {
		return fmt.Errorf("openai responses: max tokens must be positive")
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		return fmt.Errorf("openai responses: temperature must be between 0 and 2")
	}
	switch p.ReasoningEffort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh":
		return nil
	default:
		return fmt.Errorf("openai responses: unsupported reasoning effort %q", p.ReasoningEffort)
	}
}

// ResponseModel is a ChatModel backed by OpenAI's Responses API.
type ResponseModel struct {
	credential  Credential
	model       string
	parameters  ResponseParameters
	contextSize int
	httpClient  *http.Client
}

// ResponseModelOption configures a ResponseModel.
type ResponseModelOption func(*responseModelOptions)

type responseModelOptions struct {
	parameters  ResponseParameters
	contextSize int
	httpClient  *http.Client
}

// WithResponseParameters sets default Responses API parameters.
func WithResponseParameters(parameters ResponseParameters) ResponseModelOption {
	return func(options *responseModelOptions) {
		options.parameters = parameters.Clone()
	}
}

// WithResponseContextSize sets model context length for upper-layer compression.
func WithResponseContextSize(contextSize int) ResponseModelOption {
	return func(options *responseModelOptions) {
		options.contextSize = contextSize
	}
}

// WithResponseHTTPClient overrides the HTTP client used by the Responses API provider.
func WithResponseHTTPClient(client *http.Client) ResponseModelOption {
	return func(options *responseModelOptions) {
		options.httpClient = client
	}
}

// NewResponseModel creates an OpenAI Responses API model.
func NewResponseModel(credential Credential, model string, opts ...ResponseModelOption) (*ResponseModel, error) {
	options := responseModelOptions{
		contextSize: 200000,
		httpClient:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := credential.Validate(); err != nil {
		return nil, fmt.Errorf("invalid OpenAI Responses credential: %w", err)
	}
	if model == "" {
		return nil, fmt.Errorf("openai responses: model is empty")
	}
	if err := options.parameters.Validate(); err != nil {
		return nil, err
	}
	return &ResponseModel{
		credential:  credential,
		model:       model,
		parameters:  options.parameters.Clone(),
		contextSize: options.contextSize,
		httpClient:  options.httpClient,
	}, nil
}

// ListModels returns embedded OpenAI Responses model cards.
func ListModels() ([]asmodel.ModelCard, error) {
	return asmodel.LoadModelCardsFSWithDefaults(responseModelFS, "models", asmodel.NewModelCardDefaults(responseProviderName, asmodel.ModelCapabilities{
		asmodel.ModelCapabilityTools:            true,
		asmodel.ModelCapabilityStructuredOutput: true,
		asmodel.ModelCapabilityEmbedding:        false,
		asmodel.ModelCapabilityGeneration:       true,
	}, map[string]any{"api": "responses"}))
}

// Name returns the provider and model name.
func (m *ResponseModel) Name() string {
	if m == nil {
		return responseProviderName + ":<nil>"
	}
	return responseProviderName + ":" + m.model
}

// Call runs a non-streaming Responses API call.
func (m *ResponseModel) Call(ctx context.Context, request asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	body, err := m.buildBody(request, false)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := m.post(ctx, body) //nolint:bodyclose
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, responseError(resp)
	}
	var payload responsePayloadJSON
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, normalizeResponseError(err)
	}
	return parseResponsePayload(payload, time.Since(start)), nil
}

// Stream runs a streaming Responses API call.
func (m *ResponseModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	body, err := m.buildBody(request, true)
	if err != nil {
		return nil, err
	}
	//nolint:bodyclose
	resp, err := m.post(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		defer resp.Body.Close()
		return nil, responseError(resp)
	}
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		parseResponseStream(ctx, resp.Body, time.Now(), out)
	}()
	return out, nil
}

// CountTokens returns an approximate token count.
func (m *ResponseModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

// GenerateStructured requests JSON-schema constrained output through the Responses API.
func (m *ResponseModel) GenerateStructured(ctx context.Context, request asmodel.StructuredOutputRequest) (*asmodel.StructuredResponse, error) {
	body, err := m.buildBody(request.CallRequest, false)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = "structured_output"
	}
	if len(request.Schema) == 0 {
		return nil, fmt.Errorf("openai responses: structured output schema is required")
	}
	body["text"] = map[string]any{
		"format": map[string]any{
			"type":   "json_schema",
			"name":   name,
			"schema": request.Schema,
			"strict": request.Strict,
		},
	}
	start := time.Now()
	resp, err := m.post(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, responseError(resp)
	}
	var payload responsePayloadJSON
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, normalizeResponseError(err)
	}
	chatResp := parseResponsePayload(payload, time.Since(start))
	text := chatResp.GetTextContent("")
	if text == nil || strings.TrimSpace(*text) == "" {
		return nil, fmt.Errorf("openai responses: structured output response did not contain text JSON")
	}
	var structured map[string]any
	if err := json.Unmarshal([]byte(*text), &structured); err != nil {
		return nil, normalizeResponseError(err)
	}
	return &asmodel.StructuredResponse{
		Content:   structured,
		ID:        chatResp.ID,
		CreatedAt: chatResp.CreatedAt,
		Type:      asmodel.StructuredResponseType,
		Usage:     chatResp.Usage.Clone(),
		Metadata: map[string]any{
			"provider":    responseProviderName,
			"schema_name": name,
		},
	}, nil
}

// ValidateParameters rejects Chat Completions-only fields before they reach the Responses API.
func (m *ResponseModel) ValidateParameters(parameters map[string]any) error {
	if _, ok := parameters["audio"]; ok {
		return fmt.Errorf("openai responses: Chat Completions audio parameters are not supported by the Responses API")
	}
	if _, ok := parameters["modalities"]; ok {
		return fmt.Errorf("openai responses: Chat Completions audio modalities are not supported by the Responses API")
	}
	return nil
}

func (m *ResponseModel) buildBody(request asmodel.CallRequest, stream bool) (map[string]any, error) {
	if m == nil {
		return nil, fmt.Errorf("openai responses: nil model")
	}
	if err := m.ValidateParameters(request.Parameters); err != nil {
		return nil, err
	}
	input, err := formatResponseInput(request.Messages)
	if err != nil {
		return nil, err
	}
	tools, toolChoice, err := formatResponseTools(request.Tools, request.ToolChoice)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":  m.model,
		"input":  input,
		"stream": stream,
	}
	if m.parameters.MaxTokens != nil {
		body["max_output_tokens"] = *m.parameters.MaxTokens
	}
	if m.parameters.Temperature != nil {
		body["temperature"] = *m.parameters.Temperature
	}
	if m.parameters.Store != nil {
		body["store"] = *m.parameters.Store
	}
	if m.parameters.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *m.parameters.ParallelToolCalls
	}
	if m.parameters.ThinkingEnable && m.parameters.ReasoningEffort != "" && m.parameters.ReasoningEffort != "none" {
		body["reasoning"] = map[string]any{"effort": m.parameters.ReasoningEffort}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if toolChoice != nil {
		body["tool_choice"] = toolChoice
	}
	return body, nil
}

func (m *ResponseModel) post(ctx context.Context, body map[string]any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, responsesBaseURL(m.credential)+"/responses", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.credential.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if m.credential.Organization != "" {
		req.Header.Set("OpenAI-Organization", m.credential.Organization)
	}
	client := m.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, normalizeResponseError(err)
	}
	return resp, nil
}

func responsesBaseURL(credential Credential) string {
	if credential.BaseURL != "" {
		return strings.TrimRight(credential.BaseURL, "/")
	}
	return defaultResponsesURL
}

func formatResponseInput(messages []*message.Message) ([]any, error) {
	input := make([]any, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		content, items, err := formatResponseContent(msg.Content)
		if err != nil {
			return nil, err
		}
		role := string(msg.Role)
		input = append(input, map[string]any{"role": role, "content": content})
		input = append(input, items...)
	}
	return input, nil
}

func formatResponseContent(blocks message.ContentBlockList) ([]any, []any, error) {
	content := make([]any, 0, len(blocks))
	var items []any
	for _, block := range blocks {
		switch typed := block.(type) {
		case *message.TextBlock:
			content = append(content, map[string]any{"type": "input_text", "text": typed.Text})
		case *message.HintBlock:
			hintContent, err := hintResponseContent(typed)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, hintContent...)
		case *message.DataBlock:
			part, err := responseDataPart(typed)
			if err != nil {
				return nil, nil, err
			}
			if part != nil {
				content = append(content, part)
			}
		case *message.ToolResultBlock:
			items = append(items, map[string]any{"type": "function_call_output", "call_id": typed.ID, "output": toolResultText(typed.Output)})
		case *message.ToolCallBlock:
			callID, _ := typed.Extra["call_id"].(string)
			if callID == "" {
				callID = typed.ID
			}
			items = append(items, map[string]any{"type": "function_call", "id": typed.ID, "call_id": callID, "name": typed.Name, "arguments": typed.Input})
		case *message.ThinkingBlock:
			continue
		default:
			return nil, nil, fmt.Errorf("openai responses: unsupported content block %T", block)
		}
	}
	return content, items, nil
}

func hintResponseContent(block *message.HintBlock) ([]any, error) {
	if block.Blocks == nil {
		return []any{map[string]any{"type": "input_text", "text": block.Hint}}, nil
	}
	content := make([]any, 0, len(block.Blocks))
	for _, nested := range block.Blocks {
		switch typed := nested.(type) {
		case *message.TextBlock:
			content = append(content, map[string]any{"type": "input_text", "text": typed.Text})
		case *message.DataBlock:
			part, err := responseDataPart(typed)
			if err != nil {
				return nil, err
			}
			if part != nil {
				content = append(content, part)
			}
		default:
			return nil, fmt.Errorf("openai responses: unsupported hint content block %T", nested)
		}
	}
	return content, nil
}

func responseDataPart(block *message.DataBlock) (map[string]any, error) {
	if block == nil || block.Source == nil {
		return nil, nil
	}
	switch source := block.Source.(type) {
	case *message.URLSource:
		switch {
		case strings.HasPrefix(source.MediaType, "image/"):
			return map[string]any{"type": "input_image", "image_url": source.URL}, nil
		case strings.HasPrefix(source.MediaType, "video/"):
			return nil, &asmodel.CapabilityError{Model: "openai responses", Capability: asmodel.ModelCapabilityVideo}
		case strings.HasPrefix(source.MediaType, "audio/"):
			return nil, &asmodel.CapabilityError{Model: "openai responses", Capability: asmodel.ModelCapabilityAudio}
		default:
			return nil, fmt.Errorf("openai responses: unsupported URL media type %q", source.MediaType)
		}
	case *message.Base64Source:
		switch {
		case strings.HasPrefix(source.MediaType, "image/"):
			return map[string]any{"type": "input_image", "image_url": fmt.Sprintf("data:%s;base64,%s", source.MediaType, source.Data)}, nil
		case strings.HasPrefix(source.MediaType, "video/"):
			return nil, &asmodel.CapabilityError{Model: "openai responses", Capability: asmodel.ModelCapabilityVideo}
		case strings.HasPrefix(source.MediaType, "audio/"):
			return nil, &asmodel.CapabilityError{Model: "openai responses", Capability: asmodel.ModelCapabilityAudio}
		default:
			return nil, fmt.Errorf("openai responses: unsupported base64 media type %q", source.MediaType)
		}
	default:
		return nil, fmt.Errorf("openai responses: unsupported data source %T", block.Source)
	}
}

func formatResponseTools(tools []asmodel.ToolSchema, choice *types.ToolChoice) ([]any, any, error) {
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
	formatted := make([]any, 0, len(filtered))
	for _, tool := range filtered {
		formatted = append(formatted, map[string]any{
			"type":        "function",
			"name":        tool.Function.Name,
			"description": tool.Function.Description,
			"parameters":  tool.Function.Parameters,
		})
	}
	if choice == nil {
		return formatted, nil, nil
	}
	switch choice.Mode {
	case string(types.ToolChoiceAuto), string(types.ToolChoiceNone), string(types.ToolChoiceRequired):
		return formatted, choice.Mode, nil
	default:
		return formatted, map[string]any{"type": "function", "name": choice.Mode}, nil
	}
}

type responsePayloadJSON struct {
	ID     string               `json:"id"`
	Output []responseOutputItem `json:"output"`
	Usage  responseUsage        `json:"usage"`
}

type responseOutputItem struct {
	ID        string                `json:"id"`
	Type      string                `json:"type"`
	CallID    string                `json:"call_id"`
	Name      string                `json:"name"`
	Arguments string                `json:"arguments"`
	Content   []responseContentPart `json:"content"`
	Summary   []responseSummaryPart `json:"summary"`
}

type responseContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseSummaryPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type accumulatedToolCall struct {
	id    string
	name  string
	input string
}

func parseResponsePayload(payload responsePayloadJSON, elapsed time.Duration) *asmodel.ChatResponse {
	content := message.ContentBlockList{}
	for _, item := range payload.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					content = append(content, message.NewTextBlock(part.Text))
				}
			}
		case "function_call":
			content = append(content, message.NewToolCallBlock(item.ID, item.Name, item.Arguments, message.WithToolCallExtra("call_id", item.CallID)))
		case "reasoning":
			for _, summary := range item.Summary {
				if summary.Text != "" {
					content = append(content, message.NewThinkingBlock(summary.Text))
				}
			}
		}
	}
	usage := &asmodel.ChatUsage{
		InputTokens:  payload.Usage.InputTokens,
		OutputTokens: payload.Usage.OutputTokens,
		Time:         elapsed,
		Type:         asmodel.UsageTypeChat,
	}
	return asmodel.NewChatResponse(content, true, asmodel.WithChatResponseID(payload.ID), asmodel.WithChatResponseUsage(usage))
}

func parseResponseStream(ctx context.Context, reader io.Reader, start time.Time, out chan<- asmodel.ChatResponse) {
	acc := newResponseStreamAccumulator(start)
	scanner := bufio.NewScanner(reader)
	var data strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
		if strings.TrimSpace(line) == "" && data.Len() > 0 {
			if !sendStreamResponse(ctx, out, acc.consumeEvent([]byte(data.String()))) {
				return
			}
			data.Reset()
		}
	}
	if err := scanner.Err(); err != nil {
		sendStreamResponse(ctx, out, streamErrorResponse(normalizeResponseError(err)))
	}
}

type responseStreamAccumulator struct {
	start     time.Time
	textID    string
	text      string
	toolCalls map[string]*accumulatedToolCall
	toolOrder []string
}

func newResponseStreamAccumulator(start time.Time) *responseStreamAccumulator {
	return &responseStreamAccumulator{start: start, toolCalls: map[string]*accumulatedToolCall{}}
}

func (acc *responseStreamAccumulator) consumeEvent(data []byte) *asmodel.ChatResponse {
	var event struct {
		Type     string              `json:"type"`
		Delta    string              `json:"delta"`
		ItemID   string              `json:"item_id"`
		Item     responseOutputItem  `json:"item"`
		Response responsePayloadJSON `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return streamErrorResponse(normalizeResponseError(err))
	}
	switch event.Type {
	case "response.output_text.delta":
		if acc.textID == "" {
			acc.textID = newResponseBlockID()
		}
		acc.text += event.Delta
		return asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock(event.Delta, message.WithBlockID(acc.textID))}, false)
	case "response.output_item.added":
		if event.Item.Type != "function_call" {
			return nil
		}
		acc.toolCalls[event.Item.ID] = &accumulatedToolCall{id: event.Item.ID, name: event.Item.Name}
		acc.toolOrder = append(acc.toolOrder, event.Item.ID)
		return asmodel.NewChatResponse(message.ContentBlockList{
			message.NewToolCallBlock(event.Item.ID, event.Item.Name, "", message.WithToolCallExtra("call_id", event.Item.CallID)),
		}, false)
	case "response.function_call_arguments.delta":
		call := acc.toolCalls[event.ItemID]
		if call == nil {
			call = &accumulatedToolCall{id: event.ItemID}
			acc.toolCalls[event.ItemID] = call
			acc.toolOrder = append(acc.toolOrder, event.ItemID)
		}
		call.input += event.Delta
		return asmodel.NewChatResponse(message.ContentBlockList{message.NewToolCallBlock(call.id, call.name, event.Delta)}, false)
	case "response.completed":
		return parseResponsePayload(event.Response, time.Since(acc.start))
	default:
		return nil
	}
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

func newResponseBlockID() string {
	return fmt.Sprintf("openai-response-block-%d", time.Now().UnixNano())
}

func responseError(resp *http.Response) error {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	message := payload.Error.Message
	if message == "" {
		message = resp.Status
	}
	return &asmodel.ProviderError{
		Provider:   responseProviderName,
		Code:       payload.Error.Code,
		StatusCode: resp.StatusCode,
		Message:    message,
		Err:        fmt.Errorf("%s", message),
	}
}

func normalizeResponseError(err error) error {
	return asmodel.NormalizeError(responseProviderName, err)
}

var _ asmodel.ChatModel = (*ResponseModel)(nil)
