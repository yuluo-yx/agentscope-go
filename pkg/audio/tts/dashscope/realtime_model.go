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

	"github.com/yuluo-yx/agentscope-go/pkg/audio/tts"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

const (
	realtimePath          = "/api-ws/v1/realtime"
	defaultConnectTimeout = 5 * time.Second
)

// RealtimeModelOption configures a DashScope bidirectional realtime TTS model.
type RealtimeModelOption func(*realtimeModelOptions)

type realtimeModelOptions struct {
	parameters      Parameters
	stream          bool
	coldStartLength int
	coldStartWords  int
	endpoint        string
	dialer          *websocket.Dialer
	connectTimeout  time.Duration
}

// WithRealtimeParameters sets default realtime TTS parameters.
func WithRealtimeParameters(parameters Parameters) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		options.parameters = mergeParameterDefaults(parameters)
	}
}

// WithRealtimeStream sets whether Synthesize returns incremental audio chunks.
func WithRealtimeStream(stream bool) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		options.stream = stream
	}
}

// WithRealtimeColdStartLength sets the minimum buffered character count before the first text append.
func WithRealtimeColdStartLength(length int) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		if length > 0 {
			options.coldStartLength = length
		}
	}
}

// WithRealtimeColdStartWords sets the minimum buffered word count before the first text append.
func WithRealtimeColdStartWords(words int) RealtimeModelOption {
	return func(options *realtimeModelOptions) {
		if words > 0 {
			options.coldStartWords = words
		}
	}
}

// WithRealtimeEndpoint overrides the realtime TTS WebSocket endpoint, mainly for tests or private gateways.
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

// RealtimeModel implements DashScope Qwen TTS Realtime over WebSocket.
type RealtimeModel struct {
	credential Credential
	model      string
	parameters Parameters

	stream          bool
	coldStartLength int
	coldStartWords  int
	endpoint        string
	dialer          *websocket.Dialer
	connectTimeout  time.Duration

	connMu    sync.Mutex
	conn      *websocket.Conn
	connected bool

	textMu          sync.Mutex
	coldStartBuffer string
	coldStartDone   bool
	accumulatedText string

	audioMu       sync.Mutex
	audioSignal   chan struct{}
	audioPCM      []byte
	consumed      int
	finished      bool
	readErr       error
	sessionID     string
	lastResponse  string
	closeNotified bool
}

// NewRealtimeModel creates a DashScope realtime TTS model.
func NewRealtimeModel(credential Credential, model string, opts ...RealtimeModelOption) (*RealtimeModel, error) {
	options := realtimeModelOptions{
		parameters:     defaultParameters(),
		stream:         true,
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
		return nil, fmt.Errorf("dashscope realtime tts: model is empty")
	}
	if options.dialer == nil {
		options.dialer = websocket.DefaultDialer
	}
	return &RealtimeModel{
		credential:      credential,
		model:           strings.TrimSpace(model),
		parameters:      mergeParameterDefaults(options.parameters),
		stream:          options.stream,
		coldStartLength: options.coldStartLength,
		coldStartWords:  options.coldStartWords,
		endpoint:        options.endpoint,
		dialer:          options.dialer,
		connectTimeout:  options.connectTimeout,
		audioSignal:     make(chan struct{}, 1),
	}, nil
}

// Name returns the provider-qualified model name.
func (m *RealtimeModel) Name() string {
	if m == nil {
		return providerName + ":<nil>"
	}
	return providerName + ":" + m.model
}

// Realtime reports that this model accepts incremental text input.
func (m *RealtimeModel) Realtime() bool {
	_ = m
	return true
}

// Connect opens the DashScope realtime WebSocket and sends session.update like the Python implementation.
func (m *RealtimeModel) Connect(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("dashscope realtime tts: nil model")
	}
	m.connMu.Lock()
	if m.connected {
		m.connMu.Unlock()
		return nil
	}
	m.connMu.Unlock()

	endpoint, err := m.realtimeURL()
	if err != nil {
		return err
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
	conn, resp, err := m.dialer.DialContext(dialCtx, endpoint, header)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		if resp != nil && resp.StatusCode >= http.StatusBadRequest {
			return providerError(resp)
		}
		return asmodel.NormalizeError(providerName, err)
	}

	m.connMu.Lock()
	m.conn = conn
	m.connected = true
	m.connMu.Unlock()

	m.resetAudio()
	go m.readLoop(conn)
	if err := m.updateSession(ctx); err != nil {
		_ = m.Close(context.WithoutCancel(ctx))
		return err
	}
	return nil
}

// Close closes the realtime WebSocket connection.
func (m *RealtimeModel) Close(ctx context.Context) error {
	_ = ctx
	if m == nil {
		return nil
	}
	m.connMu.Lock()
	conn := m.conn
	m.conn = nil
	wasConnected := m.connected
	m.connected = false
	m.connMu.Unlock()
	if conn != nil {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		_ = conn.Close()
	}
	if wasConnected {
		m.markFinished(nil)
	}
	return nil
}

// Push sends incremental text and returns the currently available audio delta, if any.
func (m *RealtimeModel) Push(ctx context.Context, text string) (*tts.Response, error) {
	if m == nil {
		return nil, fmt.Errorf("dashscope realtime tts: nil model")
	}
	if text == "" {
		return tts.NewResponse(nil, false, tts.WithResponseMetadata(m.metadata())), nil
	}

	sendText := m.bufferRealtimeText(text)
	if sendText != "" {
		if err := m.appendText(ctx, sendText); err != nil {
			if isClosedWebSocketError(err) {
				return tts.NewResponse(nil, false, tts.WithResponseMetadata(m.metadata())), nil
			}
			return nil, err
		}
	}
	return m.takeAudioResponse(false), nil
}

// Synthesize commits and finishes the current utterance; an empty request flushes previously pushed text.
func (m *RealtimeModel) Synthesize(ctx context.Context, request tts.Request) (<-chan tts.Response, error) {
	if m == nil {
		return nil, fmt.Errorf("dashscope realtime tts: nil model")
	}
	unsent := m.finalizeRealtimeText(request.Text)
	if unsent != "" {
		if err := m.appendText(ctx, unsent); err != nil {
			return nil, err
		}
	}
	if err := m.sendRealtimeEvent(ctx, map[string]any{
		"event_id": newRealtimeEventID(),
		"type":     "input_text_buffer.commit",
	}); err != nil {
		return nil, err
	}
	if err := m.sendRealtimeEvent(ctx, map[string]any{
		"event_id": newRealtimeEventID(),
		"type":     "session.finish",
	}); err != nil {
		return nil, err
	}
	m.resetTextState()

	out := make(chan tts.Response, 4)
	if m.stream {
		go m.streamRealtimeResponses(ctx, out)
		return out, nil
	}
	go m.collectRealtimeResponse(ctx, out)
	return out, nil
}

func (m *RealtimeModel) realtimeURL() (string, error) {
	raw := strings.TrimSpace(m.endpoint)
	if raw == "" {
		raw = strings.TrimRight(m.credential.BaseURL, "/") + realtimePath
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("dashscope realtime tts: invalid websocket endpoint %q: %w", raw, err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("dashscope realtime tts: unsupported websocket endpoint scheme %q", parsed.Scheme)
	}
	query := parsed.Query()
	query.Set("model", m.model)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (m *RealtimeModel) updateSession(ctx context.Context) error {
	session := map[string]any{
		"voice":           m.parameters.Voice,
		"mode":            "server_commit",
		"response_format": m.parameters.AudioFormat,
		"sample_rate":     m.parameters.SampleRate,
	}
	for key, value := range m.parameters.Extra {
		if _, exists := session[key]; exists {
			continue
		}
		session[key] = utils.CloneAny(value)
	}
	return m.sendRealtimeEvent(ctx, map[string]any{
		"event_id": newRealtimeEventID(),
		"type":     "session.update",
		"session":  session,
	})
}

func (m *RealtimeModel) appendText(ctx context.Context, text string) error {
	return m.sendRealtimeEvent(ctx, map[string]any{
		"event_id": newRealtimeEventID(),
		"type":     "input_text_buffer.append",
		"text":     text,
	})
}

func (m *RealtimeModel) sendRealtimeEvent(ctx context.Context, payload map[string]any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	m.connMu.Lock()
	defer m.connMu.Unlock()
	if m.conn == nil || !m.connected {
		return fmt.Errorf("dashscope realtime tts: websocket is not connected")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = m.conn.SetWriteDeadline(deadline)
		defer func() { _ = m.conn.SetWriteDeadline(time.Time{}) }()
	}
	if err := m.conn.WriteJSON(payload); err != nil {
		return asmodel.NormalizeError(providerName, err)
	}
	return nil
}

func (m *RealtimeModel) bufferRealtimeText(text string) string {
	m.textMu.Lock()
	defer m.textMu.Unlock()

	m.accumulatedText += text
	if !m.coldStartDone {
		m.coldStartBuffer += text
		if !m.coldStartReadyLocked() {
			return ""
		}
		sendText := m.coldStartBuffer
		m.coldStartBuffer = ""
		m.coldStartDone = true
		return sendText
	}
	return text
}

func (m *RealtimeModel) finalizeRealtimeText(text string) string {
	m.textMu.Lock()
	defer m.textMu.Unlock()

	unsent := m.coldStartBuffer
	if text != "" {
		m.accumulatedText += text
		unsent += text
	}
	m.coldStartBuffer = ""
	return unsent
}

func (m *RealtimeModel) resetTextState() {
	m.textMu.Lock()
	defer m.textMu.Unlock()
	m.coldStartBuffer = ""
	m.coldStartDone = false
	m.accumulatedText = ""
}

func (m *RealtimeModel) coldStartReadyLocked() bool {
	if m.coldStartLength > 0 && len(m.coldStartBuffer) < m.coldStartLength {
		return false
	}
	if m.coldStartWords > 0 && len(strings.Fields(m.coldStartBuffer)) < m.coldStartWords {
		return false
	}
	return true
}

func (m *RealtimeModel) readLoop(conn *websocket.Conn) {
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			m.handleRealtimeReadError(err)
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		if err := m.handleRealtimeEvent(data); err != nil {
			m.markFinished(err)
			return
		}
	}
}

func (m *RealtimeModel) handleRealtimeEvent(data []byte) error {
	var event realtimeEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("dashscope realtime tts: decode event: %w", err)
	}

	m.audioMu.Lock()
	defer m.audioMu.Unlock()

	changed := false
	switch event.Type {
	case "session.created":
		m.audioPCM = nil
		m.consumed = 0
		m.finished = false
		m.readErr = nil
		m.closeNotified = false
		m.sessionID = event.Session.ID
		changed = true
	case "response.created":
		m.lastResponse = event.Response.ID
	case "response.audio.delta":
		if event.Delta == "" {
			break
		}
		audio, err := base64.StdEncoding.DecodeString(event.Delta)
		if err != nil {
			m.readErr = fmt.Errorf("dashscope realtime tts: decode audio delta: %w", err)
			m.finished = true
		} else {
			m.audioPCM = append(m.audioPCM, audio...)
		}
		changed = true
	case "session.finished":
		m.finished = true
		changed = true
	case "error":
		m.readErr = providerRealtimeEventError(event)
		m.finished = true
		changed = true
	}
	if changed {
		m.notifyAudioChangeLocked()
	}
	return nil
}

func (m *RealtimeModel) handleRealtimeReadError(err error) {
	if isClosedWebSocketError(err) {
		m.markFinished(nil)
		return
	}
	m.markFinished(asmodel.NormalizeError(providerName, err))
}

func (m *RealtimeModel) markFinished(err error) {
	m.audioMu.Lock()
	defer m.audioMu.Unlock()
	if err != nil {
		m.readErr = err
	}
	m.finished = true
	m.closeNotified = true
	m.notifyAudioChangeLocked()
}

func (m *RealtimeModel) resetAudio() {
	m.audioMu.Lock()
	defer m.audioMu.Unlock()
	m.audioPCM = nil
	m.consumed = 0
	m.finished = false
	m.readErr = nil
	m.sessionID = ""
	m.lastResponse = ""
	m.closeNotified = false
	for {
		select {
		case <-m.audioSignal:
		default:
			return
		}
	}
}

func (m *RealtimeModel) streamRealtimeResponses(ctx context.Context, out chan<- tts.Response) {
	defer close(out)
	for {
		response, done := m.waitAndTakeRealtimeResponse(ctx)
		out <- *response
		if done {
			m.resetAudio()
			return
		}
	}
}

func (m *RealtimeModel) collectRealtimeResponse(ctx context.Context, out chan<- tts.Response) {
	defer close(out)
	if err := m.waitRealtimeFinished(ctx); err != nil {
		out <- *tts.NewResponse(nil, true, tts.WithResponseError(err), tts.WithResponseMetadata(m.metadata()))
		return
	}
	out <- *m.takeAudioResponse(true)
	m.resetAudio()
}

func (m *RealtimeModel) waitAndTakeRealtimeResponse(ctx context.Context) (*tts.Response, bool) {
	for {
		m.audioMu.Lock()
		hasDelta := len(m.audioPCM) > m.consumed
		done := m.finished || m.readErr != nil
		if hasDelta || done {
			response := m.takeAudioResponseLocked(done)
			m.audioMu.Unlock()
			return response, done
		}
		signal := m.audioSignal
		m.audioMu.Unlock()

		select {
		case <-ctx.Done():
			return tts.NewResponse(nil, true, tts.WithResponseError(ctx.Err()), tts.WithResponseMetadata(m.metadata())), true
		case <-signal:
		}
	}
}

func (m *RealtimeModel) waitRealtimeFinished(ctx context.Context) error {
	for {
		m.audioMu.Lock()
		done := m.finished
		err := m.readErr
		signal := m.audioSignal
		m.audioMu.Unlock()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-signal:
		}
	}
}

func (m *RealtimeModel) takeAudioResponse(isLast bool) *tts.Response {
	m.audioMu.Lock()
	defer m.audioMu.Unlock()
	return m.takeAudioResponseLocked(isLast)
}

func (m *RealtimeModel) takeAudioResponseLocked(isLast bool) *tts.Response {
	metadata := m.metadataLocked()
	if m.readErr != nil && len(m.audioPCM) <= m.consumed {
		return tts.NewResponse(nil, true, tts.WithResponseError(m.readErr), tts.WithResponseMetadata(metadata))
	}
	audio := m.takeAudioDeltaLocked()
	if len(audio) == 0 {
		return tts.NewResponse(nil, isLast, tts.WithResponseMetadata(metadata))
	}
	opts := []tts.ResponseOption{tts.WithResponseMetadata(metadata)}
	if m.readErr != nil {
		opts = append(opts, tts.WithResponseError(m.readErr))
	}
	return tts.NewResponse(tts.NewAudioBlock(audio, outputMediaType), isLast, opts...)
}

func (m *RealtimeModel) takeAudioDeltaLocked() []byte {
	if len(m.audioPCM) <= m.consumed {
		return nil
	}
	audio := append([]byte(nil), m.audioPCM[m.consumed:]...)
	if m.consumed == 0 {
		header := tts.StreamingWAVHeader(defaultSampleRate, defaultChannels, defaultSampleBits)
		withHeader := make([]byte, 0, len(header)+len(audio))
		withHeader = append(withHeader, header...)
		withHeader = append(withHeader, audio...)
		audio = withHeader
	}
	m.consumed = len(m.audioPCM)
	return audio
}

func (m *RealtimeModel) metadata() map[string]any {
	m.audioMu.Lock()
	defer m.audioMu.Unlock()
	return m.metadataLocked()
}

func (m *RealtimeModel) metadataLocked() map[string]any {
	metadata := map[string]any{"provider": providerName}
	if m.sessionID != "" {
		metadata["session_id"] = m.sessionID
	}
	if m.lastResponse != "" {
		metadata["response_id"] = m.lastResponse
	}
	return metadata
}

func (m *RealtimeModel) notifyAudioChangeLocked() {
	select {
	case m.audioSignal <- struct{}{}:
	default:
	}
}

func newRealtimeEventID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "event_" + hex.EncodeToString(b[:])
}

func providerRealtimeEventError(event realtimeEvent) error {
	message := event.Message
	code := event.Code
	if message == "" {
		message = event.Error.Message
	}
	if code == "" {
		code = event.Error.Code
	}
	if message == "" {
		message = "dashscope realtime tts error"
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

type realtimeEvent struct {
	Type    string `json:"type"`
	Delta   string `json:"delta"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Session struct {
		ID string `json:"id"`
	} `json:"session"`
	Response struct {
		ID string `json:"id"`
	} `json:"response"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

var _ tts.Model = (*RealtimeModel)(nil)
