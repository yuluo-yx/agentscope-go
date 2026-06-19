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
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/yuluo-yx/agentscope-go/audio/stt"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/utils"
)

const (
	realtimePath                = "/api-ws/v1/realtime"
	defaultRealtimeAudioFormat  = "pcm"
	defaultRealtimeSampleRate   = 16000
	defaultRealtimeVADThreshold = 0.0
	defaultRealtimeVADSilence   = 400 * time.Millisecond
	defaultConnectTimeout       = 5 * time.Second
)

// RealtimeMode selects how a realtime ASR session decides utterance boundaries.
type RealtimeMode string

const (
	// RealtimeModeVAD lets DashScope server-side VAD commit utterances automatically.
	RealtimeModeVAD RealtimeMode = "vad"
	// RealtimeModeManual requires callers to commit each utterance explicitly.
	RealtimeModeManual RealtimeMode = "manual"
)

// RealtimeParameters configures Qwen-ASR realtime recognition sessions.
type RealtimeParameters struct {
	Language           string
	InputAudioFormat   string
	SampleRate         int
	Mode               RealtimeMode
	VADThreshold       float64
	VADSilenceDuration time.Duration
	Extra              map[string]any
}

// Clone returns a deep copy of realtime parameters.
func (p RealtimeParameters) Clone() RealtimeParameters {
	cp := p
	cp.Extra = utils.CloneAnyMap(p.Extra)
	return cp
}

// RealtimeModelOption configures a DashScope realtime STT model.
type RealtimeModelOption func(*realtimeModelOptions)

type realtimeModelOptions struct {
	parameters     RealtimeParameters
	endpoint       string
	dialer         *websocket.Dialer
	connectTimeout time.Duration
	workspace      string
	dataInspection string
}

// WithRealtimeParameters sets default Qwen-ASR realtime parameters.
func WithRealtimeParameters(parameters RealtimeParameters) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		options.parameters = mergeRealtimeParameterDefaults(parameters)
	}
}

// WithRealtimeEndpoint overrides the realtime WebSocket endpoint, mainly for tests or private gateways.
func WithRealtimeEndpoint(endpoint string) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		options.endpoint = strings.TrimSpace(endpoint)
	}
}

// WithRealtimeDialer sets the WebSocket dialer.
func WithRealtimeDialer(dialer *websocket.Dialer) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		options.dialer = dialer
	}
}

// WithRealtimeConnectTimeout sets the connection timeout.
func WithRealtimeConnectTimeout(timeout time.Duration) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		if timeout > 0 {
			options.connectTimeout = timeout
		}
	}
}

// WithRealtimeWorkspace sets the optional DashScope workspace header.
func WithRealtimeWorkspace(workspace string) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		options.workspace = strings.TrimSpace(workspace)
	}
}

// WithRealtimeDataInspection sets the optional DashScope data-inspection header.
func WithRealtimeDataInspection(dataInspection string) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		options.dataInspection = strings.TrimSpace(dataInspection)
	}
}

// RealtimeModel implements DashScope Qwen-ASR Realtime over WebSocket.
type RealtimeModel struct {
	credential     Credential
	model          string
	parameters     RealtimeParameters
	endpoint       string
	dialer         *websocket.Dialer
	connectTimeout time.Duration
	workspace      string
	dataInspection string
}

// NewRealtimeModel creates a DashScope realtime STT model.
func NewRealtimeModel(credential Credential, model string, opts ...RealtimeModelOption) (*RealtimeModel, error) {
	options := realtimeModelOptions{
		parameters:     defaultRealtimeParameters(),
		dialer:         websocket.DefaultDialer,
		connectTimeout: defaultConnectTimeout,
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := validateCredential(credential); err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("dashscope realtime stt: model is empty")
	}
	if options.dialer == nil {
		options.dialer = websocket.DefaultDialer
	}
	options.parameters = mergeRealtimeParameterDefaults(options.parameters)
	if err := validateRealtimeParameters(options.parameters); err != nil {
		return nil, err
	}
	return &RealtimeModel{
		credential:     credential,
		model:          strings.TrimSpace(model),
		parameters:     options.parameters,
		endpoint:       options.endpoint,
		dialer:         options.dialer,
		connectTimeout: options.connectTimeout,
		workspace:      options.workspace,
		dataInspection: options.dataInspection,
	}, nil
}

// Name returns the provider-qualified model name.
func (m *RealtimeModel) Name() string {
	if m == nil {
		return providerName + ":<nil>"
	}
	return providerName + ":" + m.model
}

// Realtime reports that this model can create streaming recognition sessions.
func (m *RealtimeModel) Realtime() bool {
	_ = m
	return true
}

// Recognize performs one-shot realtime recognition by opening a session, pushing audio, and finishing it.
func (m *RealtimeModel) Recognize(ctx context.Context, request stt.Request) (<-chan stt.Response, error) {
	if m == nil {
		return nil, fmt.Errorf("dashscope realtime stt: nil model")
	}
	if request.Audio == nil {
		return nil, fmt.Errorf("dashscope realtime stt: audio is required")
	}
	session, err := m.NewSession(ctx, stt.SessionRequest{
		Parameters: request.Parameters,
		Metadata:   request.Metadata,
	})
	if err != nil {
		return nil, err
	}
	if err := session.Push(ctx, request.Audio); err != nil {
		_ = session.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	if realtimeSession, ok := session.(*realtimeSession); ok && realtimeSession.mode == RealtimeModeManual {
		if err := session.Commit(ctx); err != nil {
			_ = session.Close(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	if err := session.Finish(ctx); err != nil {
		_ = session.Close(context.WithoutCancel(ctx))
		return nil, err
	}

	out := make(chan stt.Response, 16)
	go func() {
		defer close(out)
		defer session.Close(context.WithoutCancel(ctx))
		for response := range session.Responses() {
			select {
			case <-ctx.Done():
				out <- *stt.NewResponse("", true, stt.WithResponseError(ctx.Err()))
				return
			case out <- response:
			}
		}
	}()
	return out, nil
}

// NewSession opens a Qwen-ASR realtime recognition session.
func (m *RealtimeModel) NewSession(ctx context.Context, request stt.SessionRequest) (stt.Session, error) {
	if m == nil {
		return nil, fmt.Errorf("dashscope realtime stt: nil model")
	}
	parameters := realtimeParametersForSession(m.parameters, request)
	if err := validateRealtimeParameters(parameters); err != nil {
		return nil, err
	}
	endpoint, err := m.realtimeURL()
	if err != nil {
		return nil, err
	}
	dialCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok && m.connectTimeout > 0 {
		dialCtx, cancel = context.WithTimeout(ctx, m.connectTimeout)
	}
	defer cancel()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+m.credential.APIKey)
	header.Set("User-Agent", "agentscope-go")
	if m.workspace != "" {
		header.Set("X-DashScope-WorkSpace", m.workspace)
	}
	if m.dataInspection != "" {
		header.Set("X-DashScope-DataInspection", m.dataInspection)
	}
	conn, resp, err := m.dialer.DialContext(dialCtx, endpoint, header)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		if resp != nil && resp.StatusCode >= http.StatusBadRequest {
			return nil, providerError(resp)
		}
		return nil, asmodel.NormalizeError(providerName, err)
	}

	session := newRealtimeSession(conn, parameters, request.Metadata)
	go session.readLoop()
	if err := session.update(ctx); err != nil {
		_ = session.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return session, nil
}

func (m *RealtimeModel) realtimeURL() (string, error) {
	raw := strings.TrimSpace(m.endpoint)
	if raw == "" {
		raw = strings.TrimRight(m.credential.BaseURL, "/") + realtimePath
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("dashscope realtime stt: invalid websocket endpoint %q: %w", raw, err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("dashscope realtime stt: unsupported websocket endpoint scheme %q", parsed.Scheme)
	}
	query := parsed.Query()
	query.Set("model", m.model)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

type realtimeSession struct {
	connMu     sync.Mutex
	conn       *websocket.Conn
	parameters RealtimeParameters
	mode       RealtimeMode
	metadata   map[string]any

	stateMu   sync.RWMutex
	sessionID string

	emitMu    sync.Mutex
	responses chan stt.Response
	done      chan struct{}
	closeOnce sync.Once
}

func newRealtimeSession(conn *websocket.Conn, parameters RealtimeParameters, metadata map[string]any) *realtimeSession {
	return &realtimeSession{
		conn:       conn,
		parameters: parameters,
		mode:       parameters.Mode,
		metadata:   utils.CloneAnyMap(metadata),
		responses:  make(chan stt.Response, 16),
		done:       make(chan struct{}),
	}
}

func (s *realtimeSession) ID() string {
	if s == nil {
		return ""
	}
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.sessionID
}

func (s *realtimeSession) Responses() <-chan stt.Response {
	if s == nil {
		ch := make(chan stt.Response)
		close(ch)
		return ch
	}
	return s.responses
}

func (s *realtimeSession) Push(ctx context.Context, audio *message.DataBlock) error {
	if s == nil {
		return fmt.Errorf("dashscope realtime stt: nil session")
	}
	data, err := realtimeAudioBase64(audio)
	if err != nil {
		return err
	}
	return s.sendEvent(ctx, map[string]any{
		"event_id": newRealtimeEventID(),
		"type":     "input_audio_buffer.append",
		"audio":    data,
	})
}

func (s *realtimeSession) Commit(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("dashscope realtime stt: nil session")
	}
	return s.sendEvent(ctx, map[string]any{
		"event_id": newRealtimeEventID(),
		"type":     "input_audio_buffer.commit",
	})
}

func (s *realtimeSession) Finish(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("dashscope realtime stt: nil session")
	}
	return s.sendEvent(ctx, map[string]any{
		"event_id": newRealtimeEventID(),
		"type":     "session.finish",
	})
}

func (s *realtimeSession) Close(ctx context.Context) error {
	_ = ctx
	if s == nil {
		return nil
	}
	s.closeWithError(nil)
	return nil
}

func (s *realtimeSession) update(ctx context.Context) error {
	return s.sendEvent(ctx, map[string]any{
		"event_id": newRealtimeEventID(),
		"type":     "session.update",
		"session":  s.sessionPayload(),
	})
}

func (s *realtimeSession) sessionPayload() map[string]any {
	session := map[string]any{
		"input_audio_format": s.parameters.InputAudioFormat,
		"sample_rate":        s.parameters.SampleRate,
	}
	if s.parameters.Language != "" {
		session["input_audio_transcription"] = map[string]any{
			"language": s.parameters.Language,
		}
	}
	if s.parameters.Mode == RealtimeModeManual {
		session["turn_detection"] = nil
	} else {
		session["turn_detection"] = map[string]any{
			"type":                "server_vad",
			"threshold":           s.parameters.VADThreshold,
			"silence_duration_ms": int(s.parameters.VADSilenceDuration / time.Millisecond),
		}
	}
	for key, value := range s.parameters.Extra {
		if _, exists := session[key]; exists {
			continue
		}
		session[key] = utils.CloneAny(value)
	}
	return session
}

func (s *realtimeSession) sendEvent(ctx context.Context, payload map[string]any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn == nil {
		return fmt.Errorf("dashscope realtime stt: websocket is not connected")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = s.conn.SetWriteDeadline(deadline)
		defer func() { _ = s.conn.SetWriteDeadline(time.Time{}) }()
	}
	if err := s.conn.WriteJSON(payload); err != nil {
		return asmodel.NormalizeError(providerName, err)
	}
	return nil
}

func (s *realtimeSession) readLoop() {
	for {
		s.connMu.Lock()
		conn := s.conn
		s.connMu.Unlock()
		if conn == nil {
			return
		}
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if isClosedWebSocketError(err) {
				s.closeWithError(nil)
				return
			}
			s.closeWithError(asmodel.NormalizeError(providerName, err))
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		if err := s.handleEvent(data); err != nil {
			s.closeWithError(err)
			return
		}
	}
}

func (s *realtimeSession) handleEvent(data []byte) error {
	var event realtimeSTTEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("dashscope realtime stt: decode event: %w", err)
	}

	switch event.Type {
	case "session.created", "session.updated":
		if event.Session.ID != "" {
			s.stateMu.Lock()
			s.sessionID = event.Session.ID
			s.stateMu.Unlock()
		}
	case "conversation.item.input_audio_transcription.text":
		text := event.Text + event.Stash
		s.emit(*stt.NewResponse(
			text,
			false,
			stt.WithResponseLanguage(event.Language),
			stt.WithResponseMetadata(s.eventMetadata(event)),
		))
	case "conversation.item.input_audio_transcription.completed":
		s.emit(*stt.NewResponse(
			event.Transcript,
			true,
			stt.WithResponseLanguage(event.Language),
			stt.WithResponseMetadata(s.eventMetadata(event)),
		))
	case "conversation.item.input_audio_transcription.failed":
		s.emit(*stt.NewResponse(
			"",
			true,
			stt.WithResponseError(providerSTTRealtimeEventError(event)),
			stt.WithResponseMetadata(s.eventMetadata(event)),
		))
	case "error":
		s.closeWithError(providerSTTRealtimeEventError(event))
	case "session.finished":
		s.closeWithError(nil)
	}
	return nil
}

func (s *realtimeSession) emit(response stt.Response) {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()

	select {
	case <-s.done:
		return
	default:
	}
	select {
	case <-s.done:
	case s.responses <- response:
	}
}

func (s *realtimeSession) closeWithError(err error) {
	s.closeOnce.Do(func() {
		if err != nil {
			s.emit(*stt.NewResponse("", true, stt.WithResponseError(err), stt.WithResponseMetadata(s.baseMetadata())))
		}

		s.emitMu.Lock()
		defer s.emitMu.Unlock()
		close(s.done)
		s.connMu.Lock()
		conn := s.conn
		s.conn = nil
		s.connMu.Unlock()
		if conn != nil {
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(time.Second),
			)
			_ = conn.Close()
		}
		close(s.responses)
	})
}

func (s *realtimeSession) baseMetadata() map[string]any {
	metadata := utils.CloneAnyMap(s.metadata)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["provider"] = providerName
	if id := s.ID(); id != "" {
		metadata["session_id"] = id
	}
	return metadata
}

func (s *realtimeSession) eventMetadata(event realtimeSTTEvent) map[string]any {
	metadata := s.baseMetadata()
	metadata["event_type"] = event.Type
	if event.EventID != "" {
		metadata["event_id"] = event.EventID
	}
	if event.ItemID != "" {
		metadata["item_id"] = event.ItemID
	}
	metadata["content_index"] = event.ContentIndex
	if event.Emotion != "" {
		metadata["emotion"] = event.Emotion
	}
	if event.Text != "" {
		metadata["confirmed_text"] = event.Text
	}
	if event.Stash != "" {
		metadata["stash"] = event.Stash
	}
	if event.Error.Param != "" {
		metadata["provider_error_param"] = event.Error.Param
	}
	return metadata
}

func realtimeAudioBase64(audio *message.DataBlock) (string, error) {
	if audio == nil || audio.Source == nil {
		return "", fmt.Errorf("dashscope realtime stt: audio is required")
	}
	source, ok := audio.Source.(*message.Base64Source)
	if !ok {
		return "", fmt.Errorf("dashscope realtime stt: realtime sessions require base64 audio")
	}
	if strings.TrimSpace(source.Data) == "" {
		return "", fmt.Errorf("dashscope realtime stt: audio data is empty")
	}
	if _, err := base64.StdEncoding.DecodeString(source.Data); err != nil {
		return "", fmt.Errorf("dashscope realtime stt: invalid base64 audio: %w", err)
	}
	return source.Data, nil
}

func defaultRealtimeParameters() RealtimeParameters {
	return RealtimeParameters{
		InputAudioFormat:   defaultRealtimeAudioFormat,
		SampleRate:         defaultRealtimeSampleRate,
		Mode:               RealtimeModeVAD,
		VADThreshold:       defaultRealtimeVADThreshold,
		VADSilenceDuration: defaultRealtimeVADSilence,
	}
}

func mergeRealtimeParameterDefaults(parameters RealtimeParameters) RealtimeParameters {
	defaults := defaultRealtimeParameters()
	merged := parameters.Clone()
	if strings.TrimSpace(merged.InputAudioFormat) == "" {
		merged.InputAudioFormat = defaults.InputAudioFormat
	}
	merged.InputAudioFormat = strings.TrimSpace(merged.InputAudioFormat)
	merged.Language = strings.TrimSpace(merged.Language)
	if merged.SampleRate <= 0 {
		merged.SampleRate = defaults.SampleRate
	}
	if merged.Mode == "" {
		merged.Mode = defaults.Mode
	}
	if merged.VADSilenceDuration <= 0 {
		merged.VADSilenceDuration = defaults.VADSilenceDuration
	}
	return merged
}

func realtimeParametersForSession(base RealtimeParameters, request stt.SessionRequest) RealtimeParameters {
	parameters := base.Clone()
	for key, value := range request.Parameters {
		switch key {
		case "language":
			if language, ok := stringRealtimeParameter(value); ok {
				parameters.Language = language
			}
		case "input_audio_format":
			if format, ok := stringRealtimeParameter(value); ok {
				parameters.InputAudioFormat = format
			}
		case "sample_rate":
			if sampleRate, ok := intRealtimeParameter(value); ok {
				parameters.SampleRate = sampleRate
			}
		case "mode":
			if mode, ok := stringRealtimeParameter(value); ok {
				parameters.Mode = RealtimeMode(mode)
			}
		case "vad_threshold":
			if threshold, ok := floatRealtimeParameter(value); ok {
				parameters.VADThreshold = threshold
			}
		case "silence_duration_ms":
			if silenceMS, ok := intRealtimeParameter(value); ok {
				parameters.VADSilenceDuration = time.Duration(silenceMS) * time.Millisecond
			}
		default:
			if parameters.Extra == nil {
				parameters.Extra = map[string]any{}
			}
			parameters.Extra[key] = utils.CloneAny(value)
		}
	}
	return mergeRealtimeParameterDefaults(parameters)
}

func validateRealtimeParameters(parameters RealtimeParameters) error {
	switch parameters.InputAudioFormat {
	case "pcm", "opus":
	default:
		return fmt.Errorf("dashscope realtime stt: unsupported input audio format %q", parameters.InputAudioFormat)
	}
	switch parameters.SampleRate {
	case 16000, 8000:
	default:
		return fmt.Errorf("dashscope realtime stt: unsupported sample rate %d", parameters.SampleRate)
	}
	switch parameters.Mode {
	case RealtimeModeVAD, RealtimeModeManual:
	default:
		return fmt.Errorf("dashscope realtime stt: unsupported realtime mode %q", parameters.Mode)
	}
	if parameters.Mode == RealtimeModeVAD {
		if parameters.VADThreshold < -1 || parameters.VADThreshold > 1 {
			return fmt.Errorf("dashscope realtime stt: vad threshold must be between -1 and 1")
		}
		silenceMS := int(parameters.VADSilenceDuration / time.Millisecond)
		if silenceMS < 200 || silenceMS > 6000 {
			return fmt.Errorf("dashscope realtime stt: vad silence duration must be between 200ms and 6000ms")
		}
	}
	return nil
}

func stringRealtimeParameter(value any) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(text), true
}

func intRealtimeParameter(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		i, err := typed.Int64()
		if err == nil {
			return int(i), true
		}
		f, err := typed.Float64()
		return int(f), err == nil
	default:
		return 0, false
	}
}

func floatRealtimeParameter(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		f, err := typed.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func newRealtimeEventID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "event_" + hex.EncodeToString(b[:])
}

func providerSTTRealtimeEventError(event realtimeSTTEvent) error {
	message := event.Message
	code := event.Code
	if message == "" {
		message = event.Error.Message
	}
	if code == "" {
		code = event.Error.Code
	}
	if message == "" {
		message = "dashscope realtime stt error"
	}
	return &asmodel.ProviderError{
		Provider: providerName,
		Code:     code,
		Message:  message,
		Err:      errors.New(message),
	}
}

func isClosedWebSocketError(err error) bool {
	if err == nil {
		return false
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "use of closed network connection") ||
		strings.Contains(message, "websocket: close")
}

type realtimeSTTEvent struct {
	Type         string `json:"type"`
	EventID      string `json:"event_id"`
	ItemID       string `json:"item_id"`
	ContentIndex int    `json:"content_index"`
	Language     string `json:"language"`
	Emotion      string `json:"emotion"`
	Text         string `json:"text"`
	Stash        string `json:"stash"`
	Transcript   string `json:"transcript"`
	Code         string `json:"code"`
	Message      string `json:"message"`
	Param        string `json:"param"`
	Session      struct {
		ID string `json:"id"`
	} `json:"session"`
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Param   string `json:"param"`
		EventID string `json:"event_id"`
	} `json:"error"`
}

var (
	_ stt.Model   = (*RealtimeModel)(nil)
	_ stt.Session = (*realtimeSession)(nil)
)
