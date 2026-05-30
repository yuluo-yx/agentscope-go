// Copyright 20\d\d AgentScope Go
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

package errors

import "fmt"

// ToolNotFoundError means the model requested an unregistered tool.
type ToolNotFoundError struct {
	*AgentError
	ToolName string
}

// NewToolNotFoundError creates a missing-tool error.
func NewToolNotFoundError(toolName string, opts ...ErrorOption) *ToolNotFoundError {
	return &ToolNotFoundError{
		AgentError: NewAgentError(fmt.Sprintf("tool %q not found", toolName), opts...),
		ToolName:   toolName,
	}
}

// Error returns typed missing-tool error text.
func (e *ToolNotFoundError) Error() string {
	return formatError("ToolNotFoundError", e.Message, e.Cause)
}

// As allows the tool error to be recognized as AgentError.
func (e *ToolNotFoundError) As(target any) bool {
	return asAgentError(e.AgentError, target)
}

// ToolInterruptedError means a tool call was interrupted by a user or external flow.
type ToolInterruptedError struct {
	*AgentError
	ToolName string
}

// NewToolInterruptedError creates a tool interruption error.
func NewToolInterruptedError(toolName string, opts ...ErrorOption) *ToolInterruptedError {
	return &ToolInterruptedError{
		AgentError: NewAgentError(fmt.Sprintf("tool %q interrupted", toolName), opts...),
		ToolName:   toolName,
	}
}

// Error returns typed interruption error text.
func (e *ToolInterruptedError) Error() string {
	return formatError("ToolInterruptedError", e.Message, e.Cause)
}

// As allows the tool error to be recognized as AgentError.
func (e *ToolInterruptedError) As(target any) bool {
	return asAgentError(e.AgentError, target)
}

// ToolJSONDecodeError means tool argument JSON decoding or repair failed.
type ToolJSONDecodeError struct {
	*AgentError
	ToolName string
}

// NewToolJSONDecodeError creates a tool argument JSON error.
func NewToolJSONDecodeError(toolName string, opts ...ErrorOption) *ToolJSONDecodeError {
	return &ToolJSONDecodeError{
		AgentError: NewAgentError(fmt.Sprintf("tool %q arguments are not valid JSON", toolName), opts...),
		ToolName:   toolName,
	}
}

// Error returns typed tool argument JSON error text.
func (e *ToolJSONDecodeError) Error() string {
	return formatError("ToolJSONDecodeError", e.Message, e.Cause)
}

// As allows the tool error to be recognized as AgentError.
func (e *ToolJSONDecodeError) As(target any) bool {
	return asAgentError(e.AgentError, target)
}

// ToolGroupInactiveError means the tool's owning group is inactive.
type ToolGroupInactiveError struct {
	*AgentError
	ToolName string
	Group    string
}

// NewToolGroupInactiveError creates an inactive tool group error.
func NewToolGroupInactiveError(toolName, group string, opts ...ErrorOption) *ToolGroupInactiveError {
	return &ToolGroupInactiveError{
		AgentError: NewAgentError(fmt.Sprintf("tool %q group %q is inactive", toolName, group), opts...),
		ToolName:   toolName,
		Group:      group,
	}
}

// Error returns typed inactive tool group error text.
func (e *ToolGroupInactiveError) Error() string {
	return formatError("ToolGroupInactiveError", e.Message, e.Cause)
}

// As allows the tool error to be recognized as AgentError.
func (e *ToolGroupInactiveError) As(target any) bool {
	return asAgentError(e.AgentError, target)
}

func asAgentError(err *AgentError, target any) bool {
	if target == nil {
		return false
	}
	if targetErr, ok := target.(**AgentError); ok {
		*targetErr = err
		return true
	}
	return false
}
