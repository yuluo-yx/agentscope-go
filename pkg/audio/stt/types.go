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

package stt

import (
	"context"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
)

// UsageType represents the speech recognition usage record type.
type UsageType string

const (
	// UsageTypeSTT is the usage type for speech-to-text model calls.
	UsageTypeSTT UsageType = "stt"
)

// ResponseKind represents the STT response type.
type ResponseKind string

const (
	// ResponseType is the fixed type for STT responses.
	ResponseType ResponseKind = "stt"
)

// Usage records token counts, audio duration, and elapsed time for one STT call.
type Usage struct {
	InputTokens   int            `json:"input_tokens"`
	OutputTokens  int            `json:"output_tokens"`
	AudioDuration time.Duration  `json:"audio_duration"`
	Time          time.Duration  `json:"time"`
	Type          UsageType      `json:"type"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// Clone returns a deep copy of usage information.
func (u *Usage) Clone() *Usage {
	if u == nil {
		return nil
	}
	cp := *u
	if cp.Type == "" {
		cp.Type = UsageTypeSTT
	}
	cp.Metadata = utils.CloneAnyMap(u.Metadata)
	return &cp
}

// Segment is one recognized transcript span.
type Segment struct {
	Text  string        `json:"text"`
	Start time.Duration `json:"start"`
	End   time.Duration `json:"end"`
}

// Request is the unified input for one STT model call.
type Request struct {
	Audio      *message.DataBlock `json:"audio"`
	Parameters map[string]any     `json:"parameters,omitempty"`
	Metadata   map[string]any     `json:"metadata,omitempty"`
}

// Clone returns a deep copy of the request.
func (r Request) Clone() Request {
	cp := r
	cp.Audio = cloneDataBlock(r.Audio)
	cp.Parameters = utils.CloneAnyMap(r.Parameters)
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	return cp
}

// SessionRequest configures one realtime STT session.
type SessionRequest struct {
	Parameters map[string]any `json:"parameters,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Clone returns a deep copy of the realtime session request.
func (r SessionRequest) Clone() SessionRequest {
	cp := r
	cp.Parameters = utils.CloneAnyMap(r.Parameters)
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	return cp
}

// Response is a speech recognition response carrying text and optional segments.
type Response struct {
	Text      string         `json:"text,omitempty"`
	Segments  []Segment      `json:"segments,omitempty"`
	Language  string         `json:"language,omitempty"`
	IsLast    bool           `json:"is_last"`
	Error     error          `json:"-"`
	ID        string         `json:"id"`
	CreatedAt string         `json:"created_at"`
	Type      ResponseKind   `json:"type"`
	Usage     *Usage         `json:"usage,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// ResponseOption configures optional STT response fields.
type ResponseOption func(*Response)

// WithResponseID sets the STT response ID.
func WithResponseID(id string) ResponseOption {
	return func(resp *Response) {
		resp.ID = id
	}
}

// WithResponseCreatedAt sets the STT response creation time.
func WithResponseCreatedAt(createdAt string) ResponseOption {
	return func(resp *Response) {
		resp.CreatedAt = createdAt
	}
}

// WithResponseSegments sets recognized transcript segments.
func WithResponseSegments(segments []Segment) ResponseOption {
	return func(resp *Response) {
		resp.Segments = append([]Segment(nil), segments...)
	}
}

// WithResponseLanguage sets the recognized language code.
func WithResponseLanguage(language string) ResponseOption {
	return func(resp *Response) {
		resp.Language = language
	}
}

// WithResponseUsage sets STT response usage.
func WithResponseUsage(usage *Usage) ResponseOption {
	return func(resp *Response) {
		resp.Usage = usage.Clone()
	}
}

// WithResponseMetadata sets STT response metadata.
func WithResponseMetadata(metadata map[string]any) ResponseOption {
	return func(resp *Response) {
		resp.Metadata = utils.CloneAnyMap(metadata)
	}
}

// WithResponseError sets an asynchronous stream error carried by a terminal chunk.
func WithResponseError(err error) ResponseOption {
	return func(resp *Response) {
		resp.Error = err
	}
}

// NewResponse creates an STT response with default ID, time, and type.
func NewResponse(text string, isLast bool, opts ...ResponseOption) *Response {
	resp := &Response{
		Text:      text,
		IsLast:    isLast,
		ID:        utils.NewID(),
		CreatedAt: nowRFC3339Nano(),
		Type:      ResponseType,
		Metadata:  map[string]any{},
	}
	for _, opt := range opts {
		opt(resp)
	}
	if resp.ID == "" {
		resp.ID = utils.NewID()
	}
	if resp.CreatedAt == "" {
		resp.CreatedAt = nowRFC3339Nano()
	}
	if resp.Type == "" {
		resp.Type = ResponseType
	}
	if resp.Metadata == nil {
		resp.Metadata = map[string]any{}
	}
	if resp.Usage != nil && resp.Usage.Type == "" {
		resp.Usage.Type = UsageTypeSTT
	}
	return resp
}

// Clone returns a deep copy of the STT response.
func (r *Response) Clone() *Response {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Segments = append([]Segment(nil), r.Segments...)
	cp.Usage = r.Usage.Clone()
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	return &cp
}

// Model is the core interface implemented by STT providers.
type Model interface {
	// Name returns a provider-qualified model name for logs and diagnostics.
	Name() string
	// Realtime reports whether the model can create streaming sessions.
	Realtime() bool
	// Recognize converts a request to recognized text chunks.
	Recognize(context.Context, Request) (<-chan Response, error)
	// NewSession creates a realtime recognition session. Batch models return an unsupported error.
	NewSession(context.Context, SessionRequest) (Session, error)
}

// Session represents one realtime STT conversation.
type Session interface {
	// ID returns the provider session ID when the provider has sent one.
	ID() string
	// Responses streams partial, final, and terminal error responses until the session ends.
	Responses() <-chan Response
	// Push appends one audio chunk to the realtime input buffer.
	Push(context.Context, *message.DataBlock) error
	// Commit manually commits the current input buffer when the provider is in manual mode.
	Commit(context.Context) error
	// Finish gracefully ends the session and lets the provider flush final responses.
	Finish(context.Context) error
	// Close releases transport resources. It is safe to call multiple times.
	Close(context.Context) error
}

func cloneDataBlock(block *message.DataBlock) *message.DataBlock {
	if block == nil {
		return nil
	}
	cloned, ok := block.Clone().(*message.DataBlock)
	if !ok {
		return nil
	}
	return cloned
}

func nowRFC3339Nano() string {
	return time.Now().Format(time.RFC3339Nano)
}
