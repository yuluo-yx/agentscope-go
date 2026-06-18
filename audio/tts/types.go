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

package tts

import (
	"context"
	"time"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// UsageType represents the model usage record type.
type UsageType string

const (
	// UsageTypeTTS is the usage type for text-to-speech model calls.
	UsageTypeTTS UsageType = "tts"
)

// ResponseKind represents the TTS response type.
type ResponseKind string

const (
	// ResponseType is the fixed type for TTS responses.
	ResponseType ResponseKind = "tts"
)

// Usage records token counts and duration for one TTS model call.
type Usage struct {
	InputTokens  int            `json:"input_tokens"`
	OutputTokens int            `json:"output_tokens"`
	Time         time.Duration  `json:"time"`
	Type         UsageType      `json:"type"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Clone returns a deep copy of usage information.
func (u *Usage) Clone() *Usage {
	if u == nil {
		return nil
	}
	cp := *u
	if cp.Type == "" {
		cp.Type = UsageTypeTTS
	}
	cp.Metadata = utils.CloneAnyMap(u.Metadata)
	return &cp
}

// Request is the unified input for one TTS model call.
type Request struct {
	Text       string         `json:"text"`
	Parameters map[string]any `json:"parameters,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Clone returns a deep copy of the request.
func (r Request) Clone() Request {
	cp := r
	cp.Parameters = utils.CloneAnyMap(r.Parameters)
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	return cp
}

// Response is a TTS model response carrying one audio data block.
type Response struct {
	Content   *message.DataBlock `json:"content,omitempty"`
	IsLast    bool               `json:"is_last"`
	Error     error              `json:"-"`
	ID        string             `json:"id"`
	CreatedAt string             `json:"created_at"`
	Type      ResponseKind       `json:"type"`
	Usage     *Usage             `json:"usage,omitempty"`
	Metadata  map[string]any     `json:"metadata,omitempty"`
}

// ResponseOption configures optional TTS response fields.
type ResponseOption func(*Response)

// WithResponseID sets the TTS response ID.
func WithResponseID(id string) ResponseOption {
	return func(resp *Response) {
		resp.ID = id
	}
}

// WithResponseCreatedAt sets the TTS response creation time.
func WithResponseCreatedAt(createdAt string) ResponseOption {
	return func(resp *Response) {
		resp.CreatedAt = createdAt
	}
}

// WithResponseUsage sets TTS response usage.
func WithResponseUsage(usage *Usage) ResponseOption {
	return func(resp *Response) {
		resp.Usage = usage.Clone()
	}
}

// WithResponseMetadata sets TTS response metadata.
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

// NewResponse creates a TTS response with default ID, time, and type.
func NewResponse(content *message.DataBlock, isLast bool, opts ...ResponseOption) *Response {
	resp := &Response{
		Content:   cloneDataBlock(content),
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
		resp.Usage.Type = UsageTypeTTS
	}
	return resp
}

// Clone returns a deep copy of the TTS response.
func (r *Response) Clone() *Response {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Content = cloneDataBlock(r.Content)
	cp.Usage = r.Usage.Clone()
	cp.Metadata = utils.CloneAnyMap(r.Metadata)
	return &cp
}

// Model is the core interface implemented by TTS providers.
type Model interface {
	// Name returns a provider-qualified model name for logs and diagnostics.
	Name() string
	// Realtime reports whether the model accepts incremental text via Push.
	Realtime() bool
	// Connect opens provider state for realtime models. Batch models may no-op.
	Connect(context.Context) error
	// Close releases provider state for realtime models. Batch models may no-op.
	Close(context.Context) error
	// Push sends incremental text to realtime models and may return one audio chunk.
	Push(context.Context, string) (*Response, error)
	// Synthesize converts a request to audio chunks. Realtime models use an empty request as a flush signal.
	Synthesize(context.Context, Request) (<-chan Response, error)
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
