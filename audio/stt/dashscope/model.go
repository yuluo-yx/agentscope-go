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

package dashscope

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/audio/stt"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/utils"
)

const (
	providerName    = "dashscope"
	defaultBaseURL  = "https://dashscope.aliyuncs.com"
	transcribePath  = "/api/v1/services/audio/asr/transcription"
	defaultLanguage = "auto"
)

// Credential configures the DashScope API key and endpoint.
type Credential struct {
	APIKey  string
	BaseURL string
}

// CredentialOption configures DashScope credentials.
type CredentialOption func(*Credential)

// NewCredential creates DashScope credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	credential := Credential{APIKey: strings.TrimSpace(apiKey), BaseURL: defaultBaseURL}
	for _, opt := range opts {
		opt(&credential)
	}
	credential.BaseURL = strings.TrimRight(strings.TrimSpace(credential.BaseURL), "/")
	return credential
}

// WithBaseURL overrides the DashScope endpoint.
func WithBaseURL(baseURL string) CredentialOption {
	return func(credential *Credential) {
		credential.BaseURL = baseURL
	}
}

// Parameters configures DashScope STT recognition.
type Parameters struct {
	Language   string
	SampleRate int
	Extra      map[string]any
}

// Clone returns a deep copy of parameters.
func (p Parameters) Clone() Parameters {
	cp := p
	cp.Extra = utils.CloneAnyMap(p.Extra)
	return cp
}

// ModelOption configures a DashScope STT model.
type ModelOption func(*modelOptions)

type modelOptions struct {
	parameters Parameters
	httpClient *http.Client
}

// WithParameters sets default DashScope STT parameters.
func WithParameters(parameters Parameters) ModelOption {
	return func(options *modelOptions) {
		options.parameters = mergeParameterDefaults(parameters)
	}
}

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(client *http.Client) ModelOption {
	return func(options *modelOptions) {
		options.httpClient = client
	}
}

// Model is a native DashScope speech-to-text model.
type Model struct {
	credential Credential
	model      string
	parameters Parameters
	httpClient *http.Client
}

// NewModel creates a DashScope STT model.
func NewModel(credential Credential, model string, opts ...ModelOption) (*Model, error) {
	options := modelOptions{
		parameters: defaultParameters(),
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := validateCredential(credential); err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("dashscope stt: model is empty")
	}
	if options.httpClient == nil {
		options.httpClient = http.DefaultClient
	}
	return &Model{
		credential: credential,
		model:      strings.TrimSpace(model),
		parameters: mergeParameterDefaults(options.parameters),
		httpClient: options.httpClient,
	}, nil
}

// Name returns the provider-qualified model name.
func (m *Model) Name() string {
	if m == nil {
		return providerName + ":<nil>"
	}
	return providerName + ":" + m.model
}

// Realtime reports that this HTTP provider is batch recognition, not bidirectional realtime.
func (m *Model) Realtime() bool {
	_ = m
	return false
}

// Recognize calls DashScope native speech recognition and returns text chunks.
func (m *Model) Recognize(ctx context.Context, request stt.Request) (<-chan stt.Response, error) {
	if m == nil {
		return nil, fmt.Errorf("dashscope stt: nil model")
	}
	if request.Audio == nil {
		return nil, fmt.Errorf("dashscope stt: audio is required")
	}
	start := time.Now()
	raw, err := m.call(ctx, request)
	if err != nil {
		return nil, err
	}
	response := responseFromResult(raw, time.Since(start))
	out := make(chan stt.Response, 1)
	out <- *response
	close(out)
	return out, nil
}

// NewSession reports that the native HTTP provider does not support realtime sessions.
func (m *Model) NewSession(context.Context, stt.SessionRequest) (stt.Session, error) {
	_ = m
	return nil, fmt.Errorf("dashscope stt: realtime session is not supported")
}

func (m *Model) call(ctx context.Context, request stt.Request) (dashScopeResult, error) {
	audio, err := audioPayload(request.Audio)
	if err != nil {
		return dashScopeResult{}, err
	}
	body := map[string]any{
		"model":      m.model,
		"input":      map[string]any{"audio": audio},
		"parameters": m.parametersForRequest(request.Parameters),
	}
	data, err := json.Marshal(body)
	if err != nil {
		return dashScopeResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.credential.BaseURL+transcribePath, bytes.NewReader(data))
	if err != nil {
		return dashScopeResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.credential.APIKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return dashScopeResult{}, asmodel.NormalizeError(providerName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return dashScopeResult{}, providerError(resp)
	}
	var result dashScopeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return dashScopeResult{}, asmodel.NormalizeError(providerName, err, asmodel.WithStatusCode(resp.StatusCode))
	}
	return result, nil
}

func audioPayload(block *message.DataBlock) (map[string]any, error) {
	if block == nil || block.Source == nil {
		return nil, fmt.Errorf("dashscope stt: audio source is required")
	}
	switch source := block.Source.(type) {
	case *message.Base64Source:
		if source.Data == "" {
			return nil, fmt.Errorf("dashscope stt: base64 audio data is required")
		}
		return map[string]any{
			"data":       source.Data,
			"media_type": source.MediaType,
		}, nil
	case *message.URLSource:
		if source.URL == "" {
			return nil, fmt.Errorf("dashscope stt: audio url is required")
		}
		return map[string]any{
			"url":        source.URL,
			"media_type": source.MediaType,
		}, nil
	default:
		return nil, fmt.Errorf("dashscope stt: unsupported audio source %T", block.Source)
	}
}

func (m *Model) parametersForRequest(parameters map[string]any) map[string]any {
	out := m.parameters.toMap()
	for key, value := range parameters {
		out[key] = utils.CloneAny(value)
	}
	return out
}

func responseFromResult(result dashScopeResult, elapsed time.Duration) *stt.Response {
	metadata := map[string]any{"provider": providerName}
	if result.RequestID != "" {
		metadata["request_id"] = result.RequestID
	}
	usage := &stt.Usage{
		InputTokens:   result.Usage.InputTokens,
		OutputTokens:  result.Usage.OutputTokens,
		AudioDuration: time.Duration(result.Usage.AudioDurationMS) * time.Millisecond,
		Time:          elapsed,
		Type:          stt.UsageTypeSTT,
	}
	return stt.NewResponse(
		result.Output.Text,
		true,
		stt.WithResponseLanguage(result.Output.Language),
		stt.WithResponseSegments(segmentsFromOutput(result.Output.Segments)),
		stt.WithResponseUsage(usage),
		stt.WithResponseMetadata(metadata),
	)
}

func segmentsFromOutput(raw []dashScopeSegment) []stt.Segment {
	if len(raw) == 0 {
		return nil
	}
	out := make([]stt.Segment, 0, len(raw))
	for _, segment := range raw {
		out = append(out, stt.Segment{
			Text:  segment.Text,
			Start: time.Duration(segment.StartTimeMS) * time.Millisecond,
			End:   time.Duration(segment.EndTimeMS) * time.Millisecond,
		})
	}
	return out
}

func validateCredential(credential Credential) error {
	if credential.APIKey == "" {
		return fmt.Errorf("dashscope stt: API key is empty")
	}
	if strings.TrimSpace(credential.BaseURL) == "" {
		return fmt.Errorf("dashscope stt: base URL is empty")
	}
	return nil
}

func defaultParameters() Parameters {
	return Parameters{Language: defaultLanguage}
}

func mergeParameterDefaults(parameters Parameters) Parameters {
	defaults := defaultParameters()
	out := parameters.Clone()
	if out.Language == "" {
		out.Language = defaults.Language
	}
	return out
}

func (p Parameters) toMap() map[string]any {
	merged := mergeParameterDefaults(p)
	out := map[string]any{
		"language": merged.Language,
	}
	if merged.SampleRate > 0 {
		out["sample_rate"] = merged.SampleRate
	}
	for key, value := range merged.Extra {
		if _, exists := out[key]; exists {
			continue
		}
		out[key] = utils.CloneAny(value)
	}
	return out
}

type dashScopeResult struct {
	RequestID string          `json:"request_id"`
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	Output    dashScopeOutput `json:"output"`
	Usage     dashScopeUsage  `json:"usage"`
}

type dashScopeOutput struct {
	Text     string             `json:"text"`
	Language string             `json:"language"`
	Segments []dashScopeSegment `json:"segments"`
}

type dashScopeSegment struct {
	Text        string `json:"text"`
	StartTimeMS int    `json:"start_time_ms"`
	EndTimeMS   int    `json:"end_time_ms"`
}

type dashScopeUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	AudioDurationMS int `json:"audio_duration_ms"`
}

func providerError(resp *http.Response) error {
	var raw dashScopeResult
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	message := raw.Message
	if message == "" {
		message = resp.Status
	}
	return &asmodel.ProviderError{
		Provider:   providerName,
		Code:       raw.Code,
		StatusCode: resp.StatusCode,
		Message:    message,
		Err:        errors.New(message),
	}
}

var _ stt.Model = (*Model)(nil)
