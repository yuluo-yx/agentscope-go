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
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/tts"
	"github.com/yuluo-yx/agentscope-go/utils"
)

const (
	providerName       = "dashscope"
	defaultBaseURL     = "https://dashscope.aliyuncs.com"
	generationPath     = "/api/v1/services/aigc/multimodal-generation/generation"
	defaultVoice       = "Cherry"
	defaultAudioFormat = "pcm"
	defaultSampleRate  = 24000
	defaultChannels    = 1
	defaultSampleBits  = 16
	outputMediaType    = "audio/wav"
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
	credential := Credential{APIKey: apiKey, BaseURL: defaultBaseURL}
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

// Parameters configures DashScope TTS generation.
type Parameters struct {
	Voice         string
	AudioFormat   string
	SampleRate    int
	Channels      int
	BitsPerSample int
	Extra         map[string]any
}

// Clone returns a deep copy of parameters.
func (p Parameters) Clone() Parameters {
	cp := p
	cp.Extra = utils.CloneAnyMap(p.Extra)
	return cp
}

// ModelOption configures a DashScope TTS model.
type ModelOption func(*modelOptions)

type modelOptions struct {
	parameters Parameters
	stream     bool
	httpClient *http.Client
}

// WithParameters sets default DashScope TTS parameters.
func WithParameters(parameters Parameters) ModelOption {
	return func(options *modelOptions) {
		options.parameters = mergeParameterDefaults(parameters)
	}
}

// WithStream sets whether Synthesize emits streaming chunks or one aggregated WAV response.
func WithStream(stream bool) ModelOption {
	return func(options *modelOptions) {
		options.stream = stream
	}
}

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(client *http.Client) ModelOption {
	return func(options *modelOptions) {
		options.httpClient = client
	}
}

// Model is a native DashScope text-to-speech model.
type Model struct {
	credential Credential
	model      string
	parameters Parameters
	stream     bool
	httpClient *http.Client
}

// NewModel creates a DashScope TTS model.
func NewModel(credential Credential, model string, opts ...ModelOption) (*Model, error) {
	options := modelOptions{
		parameters: defaultParameters(),
		stream:     true,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := validateCredential(credential); err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("dashscope tts: model is empty")
	}
	if options.httpClient == nil {
		options.httpClient = http.DefaultClient
	}
	return &Model{
		credential: credential,
		model:      model,
		parameters: mergeParameterDefaults(options.parameters),
		stream:     options.stream,
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

// Realtime reports that this HTTP provider is batch/streaming, not bidirectional realtime.
func (m *Model) Realtime() bool {
	_ = m
	return false
}

// Connect is a no-op for the native HTTP provider.
func (m *Model) Connect(context.Context) error {
	_ = m
	return nil
}

// Close is a no-op for the native HTTP provider.
func (m *Model) Close(context.Context) error {
	_ = m
	return nil
}

// Push is unsupported for this non-realtime HTTP provider.
func (m *Model) Push(context.Context, string) (*tts.Response, error) {
	_ = m
	return nil, fmt.Errorf("dashscope tts: realtime push is not supported")
}

// Synthesize calls DashScope native multimodal generation and returns audio chunks.
func (m *Model) Synthesize(ctx context.Context, request tts.Request) (<-chan tts.Response, error) {
	if m == nil {
		return nil, fmt.Errorf("dashscope tts: nil model")
	}
	if strings.TrimSpace(request.Text) == "" {
		out := make(chan tts.Response, 1)
		out <- *tts.NewResponse(nil, true)
		close(out)
		return out, nil
	}

	start := time.Now()
	rawChunks, err := m.call(ctx, request)
	if err != nil {
		return nil, err
	}
	responses, err := m.responsesFromChunks(rawChunks, time.Since(start))
	if err != nil {
		return nil, err
	}
	out := make(chan tts.Response, len(responses))
	for _, response := range responses {
		out <- response
	}
	close(out)
	return out, nil
}

func (m *Model) call(ctx context.Context, request tts.Request) ([]dashScopeChunk, error) {
	body := map[string]any{
		"model":      m.model,
		"input":      map[string]any{"text": request.Text},
		"parameters": m.parametersForRequest(request.Parameters),
		"stream":     m.stream,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.credential.BaseURL+generationPath, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	req.Header.Set("Authorization", "Bearer "+m.credential.APIKey)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, asmodel.NormalizeError(providerName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, providerError(resp)
	}
	chunks, err := parseDashScopeChunks(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, asmodel.NormalizeError(providerName, err, asmodel.WithStatusCode(resp.StatusCode))
	}
	return chunks, nil
}

func (m *Model) parametersForRequest(parameters map[string]any) map[string]any {
	out := m.parameters.toMap()
	for key, value := range parameters {
		out[key] = utils.CloneAny(value)
	}
	return out
}

func (m *Model) responsesFromChunks(chunks []dashScopeChunk, elapsed time.Duration) ([]tts.Response, error) {
	usage := aggregateUsage(chunks, elapsed)
	metadata := aggregateMetadata(chunks)
	if len(chunks) == 0 {
		return []tts.Response{*tts.NewResponse(nil, true, tts.WithResponseUsage(usage), tts.WithResponseMetadata(metadata))}, nil
	}
	if !m.stream {
		pcm, err := aggregatePCM(chunks)
		if err != nil {
			return nil, err
		}
		audio := tts.WrapPCMAsWAV(pcm, m.parameters.SampleRate, m.parameters.Channels, m.parameters.BitsPerSample)
		return []tts.Response{*tts.NewResponse(
			tts.NewAudioBlock(audio, outputMediaType),
			true,
			tts.WithResponseUsage(usage),
			tts.WithResponseMetadata(metadata),
		)}, nil
	}

	responses := make([]tts.Response, 0, len(chunks))
	for index, chunk := range chunks {
		pcm, err := base64.StdEncoding.DecodeString(chunk.Output.Audio.Data)
		if err != nil {
			return nil, fmt.Errorf("dashscope tts: decode audio chunk: %w", err)
		}
		if index == 0 {
			header := tts.StreamingWAVHeader(m.parameters.SampleRate, m.parameters.Channels, m.parameters.BitsPerSample)
			audio := make([]byte, 0, len(header)+len(pcm))
			audio = append(audio, header...)
			audio = append(audio, pcm...)
			pcm = audio
		}
		isLast := index == len(chunks)-1
		opts := []tts.ResponseOption{tts.WithResponseMetadata(metadata)}
		if isLast {
			opts = append(opts, tts.WithResponseUsage(usage))
		}
		responses = append(responses, *tts.NewResponse(tts.NewAudioBlock(pcm, outputMediaType), isLast, opts...))
	}
	return responses, nil
}

func validateCredential(credential Credential) error {
	if credential.APIKey == "" {
		return fmt.Errorf("dashscope tts: API key is empty")
	}
	if strings.TrimSpace(credential.BaseURL) == "" {
		return fmt.Errorf("dashscope tts: base URL is empty")
	}
	return nil
}

func defaultParameters() Parameters {
	return Parameters{
		Voice:         defaultVoice,
		AudioFormat:   defaultAudioFormat,
		SampleRate:    defaultSampleRate,
		Channels:      defaultChannels,
		BitsPerSample: defaultSampleBits,
	}
}

func mergeParameterDefaults(parameters Parameters) Parameters {
	defaults := defaultParameters()
	out := parameters.Clone()
	if out.Voice == "" {
		out.Voice = defaults.Voice
	}
	if out.AudioFormat == "" {
		out.AudioFormat = defaults.AudioFormat
	}
	if out.SampleRate <= 0 {
		out.SampleRate = defaults.SampleRate
	}
	if out.Channels <= 0 {
		out.Channels = defaults.Channels
	}
	if out.BitsPerSample <= 0 {
		out.BitsPerSample = defaults.BitsPerSample
	}
	return out
}

func (p Parameters) toMap() map[string]any {
	merged := mergeParameterDefaults(p)
	out := map[string]any{
		"voice":        merged.Voice,
		"audio_format": merged.AudioFormat,
		"sample_rate":  merged.SampleRate,
	}
	for key, value := range merged.Extra {
		if _, exists := out[key]; exists {
			continue
		}
		out[key] = utils.CloneAny(value)
	}
	return out
}

type dashScopeChunk struct {
	RequestID string         `json:"request_id"`
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Output    dashScopeAudio `json:"output"`
	Usage     map[string]int `json:"usage"`
}

type dashScopeAudio struct {
	Audio struct {
		Data string `json:"data"`
	} `json:"audio"`
}

func parseDashScopeChunks(body io.Reader, contentType string) ([]dashScopeChunk, error) {
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		return parseSSEChunks(body)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var chunk dashScopeChunk
	if err := json.Unmarshal(data, &chunk); err == nil {
		return []dashScopeChunk{chunk}, nil
	}
	return parseJSONLines(data)
}

func parseSSEChunks(body io.Reader) ([]dashScopeChunk, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024), 10*1024*1024)
	chunks := []dashScopeChunk{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		chunk, err := decodeChunk([]byte(payload))
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

func parseJSONLines(data []byte) ([]dashScopeChunk, error) {
	chunks := []dashScopeChunk{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		chunk, err := decodeChunk(line)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

func decodeChunk(data []byte) (dashScopeChunk, error) {
	var chunk dashScopeChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return chunk, err
	}
	return chunk, nil
}

func aggregatePCM(chunks []dashScopeChunk) ([]byte, error) {
	var out []byte
	for _, chunk := range chunks {
		if chunk.Output.Audio.Data == "" {
			continue
		}
		pcm, err := base64.StdEncoding.DecodeString(chunk.Output.Audio.Data)
		if err != nil {
			return nil, fmt.Errorf("dashscope tts: decode audio chunk: %w", err)
		}
		out = append(out, pcm...)
	}
	return out, nil
}

func aggregateUsage(chunks []dashScopeChunk, elapsed time.Duration) *tts.Usage {
	usage := &tts.Usage{Time: elapsed, Type: tts.UsageTypeTTS}
	for _, chunk := range chunks {
		for key, value := range chunk.Usage {
			switch key {
			case "input_tokens", "prompt_tokens":
				usage.InputTokens += value
			case "output_tokens", "completion_tokens":
				usage.OutputTokens += value
			}
		}
	}
	return usage
}

func aggregateMetadata(chunks []dashScopeChunk) map[string]any {
	metadata := map[string]any{"provider": providerName}
	for _, chunk := range chunks {
		if chunk.RequestID != "" {
			metadata["request_id"] = chunk.RequestID
		}
	}
	return metadata
}

func providerError(resp *http.Response) error {
	var raw dashScopeChunk
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

var _ tts.Model = (*Model)(nil)
