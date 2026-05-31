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

// Package errors defines the AgentScope framework error hierarchy.
package errors

import "fmt"

// ErrorOption configures additional framework error information.
type ErrorOption func(*errorOptions)

type errorOptions struct {
	cause error
}

// WithErrorCause records the underlying error for standard errors.Is/errors.As.
func WithErrorCause(cause error) ErrorOption {
	return func(opts *errorOptions) {
		opts.cause = cause
	}
}

// AgentError represents runtime errors that can be exposed to agent reasoning.
type AgentError struct {
	Message string
	Cause   error
}

// NewAgentError creates an agent-facing error.
func NewAgentError(message string, opts ...ErrorOption) *AgentError {
	options := collectErrorOptions(opts...)
	return &AgentError{Message: message, Cause: options.cause}
}

// Error returns text with a Python-compatible class-name prefix.
func (e *AgentError) Error() string {
	return formatError("AgentError", e.Message, e.Cause)
}

// Unwrap returns the underlying error.
func (e *AgentError) Unwrap() error {
	return e.Cause
}

// DeveloperError represents configuration or programming errors for developers.
type DeveloperError struct {
	Message string
	Cause   error
}

// NewDeveloperError creates a developer-facing error.
func NewDeveloperError(message string, opts ...ErrorOption) *DeveloperError {
	options := collectErrorOptions(opts...)
	return &DeveloperError{Message: message, Cause: options.cause}
}

// Error returns text with a Python-compatible class-name prefix.
func (e *DeveloperError) Error() string {
	return formatError("DeveloperError", e.Message, e.Cause)
}

// Unwrap returns the underlying error.
func (e *DeveloperError) Unwrap() error {
	return e.Cause
}

func collectErrorOptions(opts ...ErrorOption) errorOptions {
	var options errorOptions
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

func formatError(kind, message string, cause error) string {
	if cause == nil {
		return kind + ": " + message
	}
	return fmt.Sprintf("%s: %s: %v", kind, message, cause)
}
