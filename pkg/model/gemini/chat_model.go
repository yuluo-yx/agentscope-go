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

// Package gemini provides a Google Gemini chat model backed by google.golang.org/genai.
package gemini

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

const providerName = "gemini"

//go:embed models/*.yaml
var modelFS embed.FS

// Credential configures Gemini authentication and endpoint settings.
type Credential struct {
	APIKey  string
	BaseURL string
}

// CredentialOption customizes Gemini credentials.
type CredentialOption func(*Credential)

// NewCredential creates Gemini credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	credential := Credential{APIKey: apiKey}
	for _, opt := range opts {
		opt(&credential)
	}
	credential.BaseURL = strings.TrimRight(strings.TrimSpace(credential.BaseURL), "/")
	return credential
}

// WithBaseURL overrides the Gemini API endpoint.
func WithBaseURL(baseURL string) CredentialOption {
	return func(credential *Credential) {
		credential.BaseURL = baseURL
	}
}

// ChatParameters stores Gemini generation parameters.
type ChatParameters struct {
	MaxTokens      *int32
	ThinkingEnable bool
	ThinkingBudget *int32
	Temperature    *float32
	TopP           *float32
}

// Clone returns a parameter copy.
func (p ChatParameters) Clone() ChatParameters {
	cp := p
	if p.MaxTokens != nil {
		value := *p.MaxTokens
		cp.MaxTokens = &value
	}
	if p.ThinkingBudget != nil {
		value := *p.ThinkingBudget
		cp.ThinkingBudget = &value
	}
	if p.Temperature != nil {
		value := *p.Temperature
		cp.Temperature = &value
	}
	if p.TopP != nil {
		value := *p.TopP
		cp.TopP = &value
	}
	return cp
}

// Validate validates Gemini parameter ranges.
func (p ChatParameters) Validate() error {
	if p.MaxTokens != nil && *p.MaxTokens <= 0 {
		return fmt.Errorf("gemini: max tokens must be positive")
	}
	if p.ThinkingBudget != nil && *p.ThinkingBudget <= 0 {
		return fmt.Errorf("gemini: thinking budget must be positive")
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		return fmt.Errorf("gemini: temperature must be between 0 and 2")
	}
	if p.TopP != nil && (*p.TopP <= 0 || *p.TopP > 1) {
		return fmt.Errorf("gemini: top_p must be > 0 and <= 1")
	}
	return nil
}

// ChatModel is a Gemini ChatModel implementation.
type ChatModel struct {
	credential  Credential
	model       string
	parameters  ChatParameters
	contextSize int
	client      *genai.Client
}

// ChatModelOption configures a Gemini ChatModel.
type ChatModelOption func(*chatModelOptions)

type chatModelOptions struct {
	parameters  ChatParameters
	contextSize int
	httpClient  *http.Client
}

// WithChatParameters sets default Gemini generation parameters.
func WithChatParameters(parameters ChatParameters) ChatModelOption {
	return func(options *chatModelOptions) {
		options.parameters = parameters.Clone()
	}
}

// WithContextSize sets model context length for upper-layer compression.
func WithContextSize(contextSize int) ChatModelOption {
	return func(options *chatModelOptions) {
		options.contextSize = contextSize
	}
}

// WithHTTPClient overrides the HTTP client used by google.golang.org/genai.
func WithHTTPClient(client *http.Client) ChatModelOption {
	return func(options *chatModelOptions) {
		options.httpClient = client
	}
}

// NewChatModel creates a Gemini chat model.
func NewChatModel(credential Credential, model string, opts ...ChatModelOption) (*ChatModel, error) {
	options := chatModelOptions{
		contextSize: 1048576,
		httpClient:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if strings.TrimSpace(credential.APIKey) == "" {
		return nil, fmt.Errorf("gemini: API key is empty")
	}
	if model == "" {
		return nil, fmt.Errorf("gemini: model is empty")
	}
	if err := options.parameters.Validate(); err != nil {
		return nil, err
	}
	config := &genai.ClientConfig{
		APIKey:     credential.APIKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: options.httpClient,
	}
	if credential.BaseURL != "" {
		config.HTTPOptions = genai.HTTPOptions{BaseURL: credential.BaseURL}
	}
	client, err := genai.NewClient(context.Background(), config)
	if err != nil {
		return nil, err
	}
	return &ChatModel{
		credential:  credential,
		model:       model,
		parameters:  options.parameters.Clone(),
		contextSize: options.contextSize,
		client:      client,
	}, nil
}

// ListModels returns embedded Gemini model cards.
func ListModels() ([]asmodel.ModelCard, error) {
	return asmodel.LoadModelCardsFSWithDefaults(modelFS, "models", asmodel.NewModelCardDefaults(providerName, asmodel.ModelCapabilities{
		asmodel.ModelCapabilityTools:            true,
		asmodel.ModelCapabilityStructuredOutput: true,
		asmodel.ModelCapabilityEmbedding:        false,
		asmodel.ModelCapabilityGeneration:       true,
	}, map[string]any{"api": "gemini_generate_content", "backend": "google.golang.org/genai"}))
}

// Name returns the provider-qualified model name.
func (m *ChatModel) Name() string {
	if m == nil {
		return providerName + ":<nil>"
	}
	return providerName + ":" + m.model
}

// Call sends a non-streaming Gemini request.
func (m *ChatModel) Call(ctx context.Context, request asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	contents, config, err := m.buildRequest(request)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := m.client.Models.GenerateContent(ctx, m.model, contents, config)
	if err != nil {
		return nil, normalizeError(ctx, err)
	}
	return parseGenerateContentResponse(resp, true, time.Since(start)), nil
}

// Stream sends a streaming Gemini request.
func (m *ChatModel) Stream(ctx context.Context, request asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	contents, config, err := m.buildRequest(request)
	if err != nil {
		return nil, err
	}
	out := make(chan asmodel.ChatResponse)
	go func() {
		defer close(out)
		start := time.Now()
		acc := newStreamAccumulator()
		for resp, err := range m.client.Models.GenerateContentStream(ctx, m.model, contents, config) {
			if err != nil {
				sendResponse(ctx, out, streamErrorResponse(normalizeError(ctx, err)))
				return
			}
			chunk := parseGenerateContentResponse(resp, false, time.Since(start))
			acc.add(chunk)
			if len(chunk.Content) > 0 && !sendResponse(ctx, out, chunk) {
				return
			}
		}
		sendResponse(ctx, out, acc.final(time.Since(start)))
	}()
	return out, nil
}

// CountTokens returns an approximate token count.
func (m *ChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *ChatModel) buildRequest(request asmodel.CallRequest) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	if m == nil || m.client == nil {
		return nil, nil, fmt.Errorf("gemini: nil chat model")
	}
	contents, system, err := formatMessages(request.Messages)
	if err != nil {
		return nil, nil, err
	}
	tools, toolConfig, err := formatTools(request.Tools, request.ToolChoice)
	if err != nil {
		return nil, nil, err
	}
	config := &genai.GenerateContentConfig{
		SystemInstruction: system,
		Tools:             tools,
		ToolConfig:        toolConfig,
	}
	if m.parameters.MaxTokens != nil {
		config.MaxOutputTokens = *m.parameters.MaxTokens
	}
	if m.parameters.Temperature != nil {
		config.Temperature = m.parameters.Temperature
	}
	if m.parameters.TopP != nil {
		config.TopP = m.parameters.TopP
	}
	if m.parameters.ThinkingEnable {
		config.ThinkingConfig = &genai.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: m.parameters.ThinkingBudget}
	} else {
		zero := int32(0)
		config.ThinkingConfig = &genai.ThinkingConfig{IncludeThoughts: false, ThinkingBudget: &zero}
	}
	return contents, config, nil
}

func formatMessages(messages []*message.Message) ([]*genai.Content, *genai.Content, error) {
	contents := make([]*genai.Content, 0, len(messages))
	var systemParts []*genai.Part
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		parts, err := formatContentParts(msg.Content)
		if err != nil {
			return nil, nil, err
		}
		switch msg.Role {
		case message.RoleSystem:
			systemParts = append(systemParts, parts...)
		case message.RoleUser:
			contents = append(contents, genai.NewContentFromParts(parts, genai.Role("user")))
		case message.RoleAssistant:
			contents = append(contents, genai.NewContentFromParts(parts, genai.Role("model")))
		default:
			return nil, nil, fmt.Errorf("gemini: unsupported message role %q", msg.Role)
		}
	}
	var system *genai.Content
	if len(systemParts) > 0 {
		system = genai.NewContentFromParts(systemParts, genai.Role("user"))
	}
	return contents, system, nil
}

func formatContentParts(blocks message.ContentBlockList) ([]*genai.Part, error) {
	parts := make([]*genai.Part, 0, len(blocks))
	for _, block := range blocks {
		switch typed := block.(type) {
		case *message.TextBlock:
			parts = append(parts, genai.NewPartFromText(typed.Text))
		case *message.HintBlock:
			hintParts, err := hintContentParts(typed)
			if err != nil {
				return nil, err
			}
			parts = append(parts, hintParts...)
		case *message.DataBlock:
			part, err := dataPart(typed)
			if err != nil {
				return nil, err
			}
			if part != nil {
				parts = append(parts, part)
			}
		case *message.ToolCallBlock:
			var args map[string]any
			if err := json.Unmarshal([]byte(typed.Input), &args); err != nil {
				args = map[string]any{"input": typed.Input}
			}
			parts = append(parts, genai.NewPartFromFunctionCall(typed.Name, args))
		case *message.ToolResultBlock:
			parts = append(parts, genai.NewPartFromFunctionResponse(typed.Name, map[string]any{"output": toolResultText(typed.Output)}))
		case *message.ThinkingBlock:
			continue
		default:
			return nil, fmt.Errorf("gemini: unsupported content block %T", block)
		}
	}
	return parts, nil
}

func hintContentParts(block *message.HintBlock) ([]*genai.Part, error) {
	if block.Blocks == nil {
		return []*genai.Part{genai.NewPartFromText(block.Hint)}, nil
	}
	parts := make([]*genai.Part, 0, len(block.Blocks))
	for _, nested := range block.Blocks {
		switch typed := nested.(type) {
		case *message.TextBlock:
			parts = append(parts, genai.NewPartFromText(typed.Text))
		case *message.DataBlock:
			part, err := dataPart(typed)
			if err != nil {
				return nil, err
			}
			if part != nil {
				parts = append(parts, part)
			}
		default:
			return nil, fmt.Errorf("gemini: unsupported hint content block %T", nested)
		}
	}
	return parts, nil
}

func dataPart(block *message.DataBlock) (*genai.Part, error) {
	if block == nil || block.Source == nil {
		return nil, nil
	}
	switch source := block.Source.(type) {
	case *message.Base64Source:
		if !supportedMediaType(source.MediaType) {
			return nil, &asmodel.CapabilityError{Model: "gemini", Capability: asmodel.ModelCapabilityGeneration}
		}
		data, err := base64.StdEncoding.DecodeString(source.Data)
		if err != nil {
			return nil, err
		}
		return genai.NewPartFromBytes(data, source.MediaType), nil
	case *message.URLSource:
		if !supportedMediaType(source.MediaType) {
			return nil, &asmodel.CapabilityError{Model: "gemini", Capability: asmodel.ModelCapabilityGeneration}
		}
		return genai.NewPartFromURI(source.URL, source.MediaType), nil
	default:
		return nil, fmt.Errorf("gemini: unsupported data source %T", block.Source)
	}
}

func supportedMediaType(mediaType string) bool {
	return strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "video/")
}

func formatTools(tools []asmodel.ToolSchema, choice *types.ToolChoice) ([]*genai.Tool, *genai.ToolConfig, error) {
	available := make([]string, 0, len(tools))
	for _, tool := range tools {
		available = append(available, tool.Function.Name)
	}
	if err := choice.Validate(available); err != nil {
		return nil, nil, err
	}
	if len(tools) == 0 {
		return nil, nil, nil
	}
	declarations := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		declarations = append(declarations, &genai.FunctionDeclaration{
			Name:                 tool.Function.Name,
			Description:          tool.Function.Description,
			ParametersJsonSchema: tool.Function.Parameters,
		})
	}
	formatted := []*genai.Tool{{FunctionDeclarations: declarations}}
	if choice == nil {
		return formatted, nil, nil
	}
	config := &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{}}
	switch choice.Mode {
	case string(types.ToolChoiceAuto):
		config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeAuto
	case string(types.ToolChoiceNone):
		config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeNone
	case string(types.ToolChoiceRequired):
		config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeAny
		config.FunctionCallingConfig.AllowedFunctionNames = available
	default:
		config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeAny
		config.FunctionCallingConfig.AllowedFunctionNames = []string{choice.Mode}
	}
	if len(choice.Tools) > 0 && choice.Mode == string(types.ToolChoiceRequired) {
		config.FunctionCallingConfig.AllowedFunctionNames = append([]string(nil), choice.Tools...)
	}
	return formatted, config, nil
}

func parseGenerateContentResponse(resp *genai.GenerateContentResponse, isLast bool, elapsed time.Duration) *asmodel.ChatResponse {
	if resp == nil {
		return asmodel.NewChatResponse(nil, isLast)
	}
	content := message.ContentBlockList{}
	if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
		for _, part := range resp.Candidates[0].Content.Parts {
			if part == nil {
				continue
			}
			switch {
			case part.Text != "":
				if part.Thought {
					content = append(content, message.NewThinkingBlock(part.Text))
				} else {
					content = append(content, message.NewTextBlock(part.Text))
				}
			case part.FunctionCall != nil:
				input, _ := json.Marshal(part.FunctionCall.Args)
				id := part.FunctionCall.ID
				if id == "" {
					id = "gemini-call-" + part.FunctionCall.Name
				}
				content = append(content, message.NewToolCallBlock(id, part.FunctionCall.Name, string(input)))
			case part.InlineData != nil && len(part.InlineData.Data) > 0:
				content = append(content, message.NewDataBlock(message.NewBase64Source(base64.StdEncoding.EncodeToString(part.InlineData.Data), part.InlineData.MIMEType)))
			}
		}
	}
	return asmodel.NewChatResponse(
		content,
		isLast,
		asmodel.WithChatResponseID(resp.ResponseID),
		asmodel.WithChatResponseUsage(usage(resp.UsageMetadata, elapsed)),
	)
}

func usage(usage *genai.GenerateContentResponseUsageMetadata, elapsed time.Duration) *asmodel.ChatUsage {
	if usage == nil {
		return nil
	}
	return &asmodel.ChatUsage{
		InputTokens:      int(usage.PromptTokenCount),
		OutputTokens:     int(usage.CandidatesTokenCount),
		CacheInputTokens: int(usage.CachedContentTokenCount),
		Time:             elapsed,
		Type:             asmodel.UsageTypeChat,
	}
}

type streamAccumulator struct {
	text      string
	toolCalls []*message.ToolCallBlock
	usage     *asmodel.ChatUsage
	id        string
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{}
}

func (acc *streamAccumulator) add(chunk *asmodel.ChatResponse) {
	if chunk == nil {
		return
	}
	if chunk.ID != "" {
		acc.id = chunk.ID
	}
	if chunk.Usage != nil {
		acc.usage = chunk.Usage.Clone()
	}
	for _, block := range chunk.Content {
		switch typed := block.(type) {
		case *message.TextBlock:
			acc.text += typed.Text
		case *message.ToolCallBlock:
			acc.toolCalls = append(acc.toolCalls, typed.Clone().(*message.ToolCallBlock))
		}
	}
}

func (acc *streamAccumulator) final(elapsed time.Duration) *asmodel.ChatResponse {
	content := message.ContentBlockList{}
	if acc.text != "" {
		content = append(content, message.NewTextBlock(acc.text))
	}
	for _, call := range acc.toolCalls {
		content = append(content, call.Clone())
	}
	usage := acc.usage
	if usage != nil {
		usage.Time = elapsed
	}
	return asmodel.NewChatResponse(content, true, asmodel.WithChatResponseID(acc.id), asmodel.WithChatResponseUsage(usage))
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
	if len(output.Blocks) == 0 {
		return ""
	}
	text := output.Blocks.GetTextContent("")
	if text == nil {
		return ""
	}
	return *text
}

func normalizeError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		return asmodel.NormalizeError(providerName, err, asmodel.WithStatusCode(apiErr.Code), asmodel.WithErrorCode(apiErr.Status))
	}
	return asmodel.NormalizeError(providerName, err)
}

var _ asmodel.ChatModel = (*ChatModel)(nil)
