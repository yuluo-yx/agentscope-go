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

// Package agentscope provides compatibility aliases for common core APIs.
package agentscope

//revive:disable:exported

import (
	asagent "github.com/yuluo-yx/agentscope-go/agent"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

type (
	Agent         = asagent.Agent
	AgentOption   = asagent.AgentOption
	ContextConfig = asagent.ContextConfig
	ReActConfig   = asagent.ReActConfig
	ModelConfig   = asagent.ModelConfig
	ToolProvider  = asagent.ToolProvider
)

type (
	HookInput              = asagent.HookInput
	AgentAccessor          = asagent.AgentAccessor
	EventHandler           = asagent.EventHandler
	ToolHandler            = asagent.ToolHandler
	ModelCallHandler       = asagent.ModelCallHandler
	ReplyHook              = asagent.ReplyHook
	ReasoningHook          = asagent.ReasoningHook
	ActingHook             = asagent.ActingHook
	ModelCallHook          = asagent.ModelCallHook
	SystemPromptHook       = asagent.SystemPromptHook
	Middleware             = asagent.Middleware
	ReplyMiddleware        = asagent.ReplyMiddleware
	ReasoningMiddleware    = asagent.ReasoningMiddleware
	ActingMiddleware       = asagent.ActingMiddleware
	ModelCallMiddleware    = asagent.ModelCallMiddleware
	SystemPromptMiddleware = asagent.SystemPromptMiddleware
)

type (
	AgentState     = asstate.AgentState
	SummaryContent = asstate.SummaryContent
	ReadCacheEntry = asstate.ReadCacheEntry
	ToolContext    = asstate.ToolContext
	Task           = asstate.Task
	TaskContext    = asstate.TaskContext
	TaskState      = asstate.TaskState
)

type (
	ChatResponseKind   = asmodel.ChatResponseKind
	UsageType          = asmodel.UsageType
	ChatUsage          = asmodel.ChatUsage
	ChatResponse       = asmodel.ChatResponse
	ChatResponseOption = asmodel.ChatResponseOption
	StructuredResponse = asmodel.StructuredResponse
	CallRequest        = asmodel.CallRequest
	ChatModel          = asmodel.ChatModel
	FunctionSchema     = asmodel.FunctionSchema
	ToolSchema         = asmodel.ToolSchema
)

type (
	Tool            = astool.Tool
	ToolChunk       = astool.ToolChunk
	ToolChunkOption = astool.ToolChunkOption
	ToolResponse    = astool.ToolResponse
)

const (
	TaskPending    = asstate.TaskPending
	TaskInProgress = asstate.TaskInProgress
	TaskCompleted  = asstate.TaskCompleted

	ChatResponseType       = asmodel.ChatResponseType
	StructuredResponseType = asmodel.StructuredResponseType
	UsageTypeChat          = asmodel.UsageTypeChat
)

var (
	NewAgent          = asagent.NewAgent
	WithToolkit       = asagent.WithToolkit
	WithAgentState    = asagent.WithAgentState
	WithModelConfig   = asagent.WithModelConfig
	WithContextConfig = asagent.WithContextConfig
	WithReActConfig   = asagent.WithReActConfig
	WithMiddlewares   = asagent.WithMiddlewares

	DefaultContextConfig   = asagent.DefaultContextConfig
	DefaultSummarySchema   = asagent.DefaultSummarySchema
	DefaultReActConfig     = asagent.DefaultReActConfig
	DefaultModelConfig     = asagent.DefaultModelConfig
	ApplySystemPromptHooks = asagent.ApplySystemPromptHooks

	NewAgentState  = asstate.NewAgentState
	NewToolContext = asstate.NewToolContext
	NewTask        = asstate.NewTask
	NewTaskContext = asstate.NewTaskContext

	NewChatResponse           = asmodel.NewChatResponse
	WithChatResponseID        = asmodel.WithChatResponseID
	WithChatResponseCreatedAt = asmodel.WithChatResponseCreatedAt
	WithChatResponseUsage     = asmodel.WithChatResponseUsage
	WithChatResponseMetadata  = asmodel.WithChatResponseMetadata
	ApproximateTokenCount     = asmodel.ApproximateTokenCount

	NewToolChunk          = astool.NewToolChunk
	WithToolChunkState    = astool.WithToolChunkState
	WithToolChunkIsLast   = astool.WithToolChunkIsLast
	WithToolChunkMetadata = astool.WithToolChunkMetadata
	NewToolResponse       = astool.NewToolResponse
)
