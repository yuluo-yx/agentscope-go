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

package model

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// ResponsePartKind represents an SDK response part type.
type ResponsePartKind string

const (
	// PartText represents a text part.
	PartText ResponsePartKind = "text"
	// PartThinking represents a thinking part.
	PartThinking ResponsePartKind = "thinking"
	// PartToolCall represents a tool call part.
	PartToolCall ResponsePartKind = "tool_call"
	// PartBase64Data represents a base64 data part.
	PartBase64Data ResponsePartKind = "base64_data"
	// PartURLData represents a URL data part.
	PartURLData ResponsePartKind = "url_data"
)

// ResponsePart is a normalized intermediate part from a provider SDK response.
type ResponsePart struct {
	Kind      ResponsePartKind
	ID        string
	Text      string
	Thinking  string
	ToolName  string
	ToolInput string
	Data      string
	URL       string
	MediaType string
	Name      string
}

// NormalizeOption configures response normalization.
type NormalizeOption func(*normalizeOptions)

type normalizeOptions struct {
	isLast   bool
	usage    *ChatUsage
	metadata map[string]any
}

// WithResponseUsage sets normalized response usage.
func WithResponseUsage(usage *ChatUsage) NormalizeOption {
	return func(opts *normalizeOptions) {
		opts.usage = usage.Clone()
	}
}

// WithResponseIsLast sets whether the normalized response is the final chunk.
func WithResponseIsLast(isLast bool) NormalizeOption {
	return func(opts *normalizeOptions) {
		opts.isLast = isLast
	}
}

// WithResponseMetadata sets normalized response metadata.
func WithResponseMetadata(metadata map[string]any) NormalizeOption {
	return func(opts *normalizeOptions) {
		opts.metadata = utils.CloneAnyMap(metadata)
	}
}

// NormalizeChatResponse converts SDK-independent parts into a ChatResponse.
func NormalizeChatResponse(parts []ResponsePart, opts ...NormalizeOption) (*ChatResponse, error) {
	options := normalizeOptions{isLast: true, metadata: map[string]any{}}
	for _, opt := range opts {
		opt(&options)
	}
	content := make(message.ContentBlockList, 0, len(parts))
	for _, part := range parts {
		block, err := normalizePart(part)
		if err != nil {
			return nil, err
		}
		content = append(content, block)
	}
	return NewChatResponse(
		content,
		options.isLast,
		WithChatResponseUsage(options.usage),
		WithChatResponseMetadata(options.metadata),
	), nil
}

// ProviderError is the common wrapper for provider SDK errors.
type ProviderError struct {
	Provider   string
	Code       string
	StatusCode int
	Message    string
	Err        error
}

// Error returns text containing provider, status code, and error code.
func (e *ProviderError) Error() string {
	if e == nil {
		return "<nil>"
	}
	prefix := e.Provider
	if prefix == "" {
		prefix = "provider"
	}
	switch {
	case e.StatusCode != 0 && e.Code != "":
		return fmt.Sprintf("%s: status %d code %s: %s", prefix, e.StatusCode, e.Code, e.Message)
	case e.StatusCode != 0:
		return fmt.Sprintf("%s: status %d: %s", prefix, e.StatusCode, e.Message)
	case e.Code != "":
		return fmt.Sprintf("%s: code %s: %s", prefix, e.Code, e.Message)
	default:
		return fmt.Sprintf("%s: %s", prefix, e.Message)
	}
}

// Unwrap returns the underlying provider error.
func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ErrorOption configures provider error normalization.
type ErrorOption func(*ProviderError)

// WithStatusCode sets the provider HTTP status code.
func WithStatusCode(statusCode int) ErrorOption {
	return func(err *ProviderError) {
		err.StatusCode = statusCode
	}
}

// WithErrorCode sets the provider business error code.
func WithErrorCode(code string) ErrorOption {
	return func(err *ProviderError) {
		err.Code = code
	}
}

// NormalizeError wraps a provider SDK error in an auditable common error.
func NormalizeError(provider string, err error, opts ...ErrorOption) error {
	if err == nil {
		return nil
	}
	providerErr := &ProviderError{
		Provider: provider,
		Message:  err.Error(),
		Err:      err,
	}
	for _, opt := range opts {
		opt(providerErr)
	}
	return providerErr
}

func normalizePart(part ResponsePart) (message.ContentBlock, error) {
	id := part.ID
	if id == "" {
		id = uuid.NewString()
	}
	switch part.Kind {
	case PartText:
		return message.NewTextBlock(part.Text, message.WithBlockID(id)), nil
	case PartThinking:
		return message.NewThinkingBlock(part.Thinking, message.WithThinkingBlockID(id)), nil
	case PartToolCall:
		return message.NewToolCallBlock(id, part.ToolName, part.ToolInput), nil
	case PartBase64Data:
		opts := []message.DataBlockOption{message.WithDataBlockID(id)}
		if part.Name != "" {
			opts = append(opts, message.WithDataBlockName(part.Name))
		}
		return message.NewDataBlock(message.NewBase64Source(part.Data, part.MediaType), opts...), nil
	case PartURLData:
		opts := []message.DataBlockOption{message.WithDataBlockID(id)}
		if part.Name != "" {
			opts = append(opts, message.WithDataBlockName(part.Name))
		}
		return message.NewDataBlock(message.NewURLSource(part.URL, part.MediaType), opts...), nil
	default:
		return nil, fmt.Errorf("agentscope/model: unsupported response part kind %q", part.Kind)
	}
}
