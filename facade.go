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
	asagent "github.com/yuluo-yx/agentscope-go/pkg/agent"
	automationevent "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
	automationgoal "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/goal"
	automationrunner "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/runner"
	automationstore "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/store"
	automationtemplate "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/template"
	asloop "github.com/yuluo-yx/agentscope-go/pkg/loop/core"
	loopruntime "github.com/yuluo-yx/agentscope-go/pkg/loop/runtime"
	asmiddleware "github.com/yuluo-yx/agentscope-go/pkg/middleware"
	asmodel "github.com/yuluo-yx/agentscope-go/pkg/model"
	asrag "github.com/yuluo-yx/agentscope-go/pkg/rag"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

type (
	Agent                  = asagent.Agent
	AgentOption            = asagent.AgentOption
	AgentConfig            = asagent.AgentConfig
	SecurityAuditEvent     = asagent.SecurityAuditEvent
	SecurityAuditEventType = asagent.SecurityAuditEventType
	SecurityAuditLogger    = asagent.SecurityAuditLogger
	SecurityAuditFunc      = asagent.SecurityAuditFunc
	ContextConfig          = asagent.ContextConfig
	ReActConfig            = asagent.ReActConfig
	ModelConfig            = asagent.ModelConfig
	ToolProvider           = asagent.ToolProvider
	ContextStrategy        = asagent.ContextStrategy
	ContextStrategyInput   = asagent.ContextStrategyInput
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
	SafeAgentState = asstate.SafeAgentState
	SummaryContent = asstate.SummaryContent
	ReadCacheEntry = asstate.ReadCacheEntry
	ToolContext    = asstate.ToolContext
	Task           = asstate.Task
	TaskContext    = asstate.TaskContext
	TaskState      = asstate.TaskState
	LoopContext    = asstate.LoopContext
	LoopRun        = asstate.LoopRun
	LoopStopReason = asstate.LoopStopReason
)

type (
	LoopSpec               = asloop.Spec
	LoopMode               = asloop.Mode
	LoopPolicy             = asloop.Policy
	LoopScope              = asloop.Scope
	LoopSuccessCriterion   = asloop.SuccessCriterion
	LoopHumanGate          = asloop.HumanGate
	LoopVerifier           = asloop.Verifier
	LoopVerifierFunc       = asloop.VerifierFunc
	LoopVerificationInput  = asloop.VerificationInput
	LoopVerificationResult = asloop.VerificationResult
	LoopObserver           = asloop.Observer
	LoopObserverFunc       = asloop.ObserverFunc
	LoopRunEvent           = asloop.RunEvent
	LoopMetrics            = asloop.Metrics
	LoopRuntime            = loopruntime.Runtime
	LoopRuntimeOption      = loopruntime.Option
)

type (
	AutomationEvent                    = automationevent.Event
	AutomationEventSource              = automationevent.EventSource
	AutomationEventHandler             = automationevent.EventHandler
	AutomationEventHandlerFunc         = automationevent.EventHandlerFunc
	AutomationRouter                   = automationevent.Router
	AutomationRouterFunc               = automationevent.RouterFunc
	AutomationRouteDecision            = automationevent.RouteDecision
	AutomationStaticRouter             = automationevent.StaticRouter
	AutomationRouteRule                = automationevent.RouteRule
	AutomationRuleRouter               = automationevent.RuleRouter
	AutomationInputMapper              = automationrunner.InputMapper
	AutomationInputMapperFunc          = automationrunner.InputMapperFunc
	AutomationTemplateMapper           = automationrunner.TemplateMapper
	AutomationTemplateMapperOption     = automationrunner.TemplateMapperOption
	AutomationTemplateData             = automationrunner.TemplateData
	AutomationAgentResolver            = automationrunner.AgentResolver
	AutomationAgentResolverFunc        = automationrunner.AgentResolverFunc
	AutomationStaticAgentResolver      = automationrunner.StaticAgentResolver
	AutomationRunStore                 = automationstore.RunStore
	AutomationRunRecord                = automationstore.RunRecord
	AutomationMemoryRunStore           = automationstore.MemoryRunStore
	AutomationWorkspaceAllocator       = automationrunner.WorkspaceAllocator
	AutomationWorkspaceAllocatorFunc   = automationrunner.WorkspaceAllocatorFunc
	AutomationWorkspaceLease           = automationrunner.WorkspaceLease
	AutomationNoopWorkspaceAllocator   = automationrunner.NoopWorkspaceAllocator
	AutomationStaticWorkspaceLease     = automationrunner.StaticWorkspaceLease
	AutomationTickerSource             = automationevent.TickerSource
	AutomationRunner                   = automationrunner.Runner
	AutomationContinuePolicy           = automationgoal.ContinuePolicy
	AutomationGoalResult               = automationgoal.GoalResult
	AutomationGoalRunEvent             = automationgoal.GoalRunEvent
	AutomationGoalRunner               = automationgoal.GoalRunner
	AutomationNextActionMapper         = automationgoal.NextActionMapper
	AutomationNextActionMapperFunc     = automationgoal.NextActionMapperFunc
	AutomationTemplateNextActionMapper = automationgoal.TemplateNextActionMapper
	AutomationNextActionTemplateData   = automationgoal.NextActionTemplateData
	AutomationReportRecorder           = automationstore.ReportRecorder
	AutomationFindingRecorder          = automationstore.FindingRecorder
	AutomationSink                     = automationstore.Sink
	AutomationSinkFunc                 = automationstore.SinkFunc
	AutomationMultiSink                = automationstore.MultiSink
	AutomationLoopReport               = automationstore.LoopReport
	AutomationFindingStatus            = automationstore.FindingStatus
	AutomationFinding                  = automationstore.Finding
	AutomationFileRunStore             = automationstore.FileRunStore
	AutomationLoopTemplate             = automationtemplate.LoopTemplate
	AutomationSkillRef                 = automationtemplate.SkillRef
	AutomationLoopTemplateConfig       = automationtemplate.LoopTemplateConfig
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

type (
	RAGSection             = asrag.Section
	RAGChunk               = asrag.Chunk
	RAGVectorRecord        = asrag.VectorRecord
	RAGVectorSearchResult  = asrag.VectorSearchResult
	RAGDocumentSummary     = asrag.DocumentSummary
	RAGVectorStore         = asrag.VectorStore
	RAGMemoryVectorStore   = asrag.MemoryVectorStore
	RAGParser              = asrag.Parser
	RAGTextParser          = asrag.TextParser
	RAGChunker             = asrag.Chunker
	RAGApproxTokenChunker  = asrag.ApproxTokenChunker
	RAGChunkerOption       = asrag.ChunkerOption
	RAGKnowledgeBase       = asrag.KnowledgeBase
	RAGKnowledgeBaseOption = asrag.KnowledgeBaseOption
	RAGInsertOption        = asrag.InsertOption
	RAGSearchOption        = asrag.SearchOption
	RAGMode                = asmiddleware.RAGMode
	RAGOption              = asmiddleware.RAGOption
	RAGMiddleware          = asmiddleware.RAGMiddleware
)

const (
	TaskPending    = asstate.TaskPending
	TaskInProgress = asstate.TaskInProgress
	TaskCompleted  = asstate.TaskCompleted

	LoopModeReportOnly                       = asloop.ModeReportOnly
	LoopModeAssisted                         = asloop.ModeAssisted
	LoopModeUnattended                       = asloop.ModeUnattended
	LoopStopCompleted                        = asstate.LoopStopCompleted
	LoopStopMaxIters                         = asstate.LoopStopMaxIterations
	LoopStopBudget                           = asstate.LoopStopBudgetExceeded
	LoopStopUser                             = asstate.LoopStopWaitingUser
	LoopStopExternal                         = asstate.LoopStopWaitingExternal
	LoopStopVerifyFail                       = asstate.LoopStopVerifierFailed
	LoopStopError                            = asstate.LoopStopError
	LoopEventStart                           = asloop.EventStart
	LoopEventStop                            = asloop.EventStop
	LoopEventVerifyEnd                       = asloop.EventVerifyEnd
	LoopEventWrapUp                          = asloop.EventWrapUp
	AutomationRouteMetadataWorkspaceRoot     = automationrunner.RouteMetadataWorkspaceRoot
	AutomationRouteMetadataWorkspaceMetadata = automationrunner.RouteMetadataWorkspaceMetadata
	AutomationEventTypeScheduleTick          = automationevent.EventTypeScheduleTick
	AutomationGoalStopCompleted              = automationgoal.GoalStopCompleted
	AutomationGoalStopMaxAttempts            = automationgoal.GoalStopMaxAttempts
	AutomationGoalStopMaxDuration            = automationgoal.GoalStopMaxDuration
	AutomationGoalStopWaitingUser            = automationgoal.GoalStopWaitingUser
	AutomationGoalStopWaitingExternal        = automationgoal.GoalStopWaitingExternal
	AutomationGoalStopError                  = automationgoal.GoalStopError
	AutomationFindingOpen                    = automationstore.FindingOpen
	AutomationFindingAccepted                = automationstore.FindingAccepted
	AutomationFindingDismissed               = automationstore.FindingDismissed
	AutomationFindingRunning                 = automationstore.FindingRunning
	AutomationFindingDone                    = automationstore.FindingDone

	ChatResponseType       = asmodel.ChatResponseType
	StructuredResponseType = asmodel.StructuredResponseType
	UsageTypeChat          = asmodel.UsageTypeChat
	RAGModeAgentic         = asmiddleware.RAGModeAgentic
	RAGModeStatic          = asmiddleware.RAGModeStatic

	SecurityAuditEventPermissionRequired = asagent.SecurityAuditEventPermissionRequired
	SecurityAuditEventPermissionDenied   = asagent.SecurityAuditEventPermissionDenied
	SecurityAuditEventToolExecutionError = asagent.SecurityAuditEventToolExecutionError
)

var (
	NewAgent                = asagent.NewAgent
	WithToolkit             = asagent.WithToolkit
	WithAgentResources      = asagent.WithAgentResources
	WithAgentState          = asagent.WithAgentState
	WithAgentConfig         = asagent.WithAgentConfig
	WithSecurityAuditLogger = asagent.WithSecurityAuditLogger
	WithModelConfig         = asagent.WithModelConfig
	WithContextConfig       = asagent.WithContextConfig
	WithContextStrategies   = asagent.WithContextStrategies
	WithReActConfig         = asagent.WithReActConfig
	WithMiddlewares         = asagent.WithMiddlewares
	WithLoopSpec            = loopruntime.WithSpec

	DefaultAgentConfig                    = asagent.DefaultAgentConfig
	DefaultContextConfig                  = asagent.DefaultContextConfig
	DefaultContextStrategies              = asagent.DefaultContextStrategies
	DefaultSummarySchema                  = asagent.DefaultSummarySchema
	DefaultReActConfig                    = asagent.DefaultReActConfig
	DefaultModelConfig                    = asagent.DefaultModelConfig
	ApplySystemPromptHooks                = asagent.ApplySystemPromptHooks
	DefaultLoopPolicy                     = asloop.DefaultPolicy
	ValidateLoopSpec                      = asloop.Validate
	NewLoopRuntime                        = loopruntime.New
	WithLoopVerifier                      = loopruntime.WithVerifier
	WithLoopObserver                      = loopruntime.WithObserver
	WithLoopEventEmission                 = loopruntime.WithEventEmission
	NewAutomationTemplateMapper           = automationrunner.NewTemplateMapper
	WithAutomationTemplateMapperUserName  = automationrunner.WithTemplateMapperUserName
	NewAutomationMemoryRunStore           = automationstore.NewMemoryRunStore
	NewAutomationTemplateNextActionMapper = automationgoal.NewTemplateNextActionMapper
	NewAutomationFileRunStore             = automationstore.NewFileRunStore

	NewAgentState     = asstate.NewAgentState
	NewSafeAgentState = asstate.NewSafeAgentState
	WrapAgentState    = asstate.WrapAgentState
	NewToolContext    = asstate.NewToolContext
	NewTask           = asstate.NewTask
	NewTaskContext    = asstate.NewTaskContext

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

	NewRAGTextParser            = asrag.NewTextParser
	NewRAGApproxTokenChunker    = asrag.NewApproxTokenChunker
	NewRAGMemoryVectorStore     = asrag.NewMemoryVectorStore
	WithRAGChunkSize            = asrag.WithChunkSize
	WithRAGChunkOverlap         = asrag.WithChunkOverlap
	NewRAGKnowledgeBase         = asrag.NewKnowledgeBase
	WithRAGMetadataFilter       = asrag.WithMetadataFilter
	WithRAGDocumentID           = asrag.WithDocumentID
	WithRAGDocumentMetadata     = asrag.WithDocumentMetadata
	WithRAGSearchTopK           = asrag.WithSearchTopK
	WithRAGSearchScoreThreshold = asrag.WithScoreThreshold
	NewRAGMiddleware            = asmiddleware.NewRAGMiddleware
	WithRAGMode                 = asmiddleware.WithRAGMode
	WithRAGTopK                 = asmiddleware.WithRAGTopK
	WithRAGScoreThreshold       = asmiddleware.WithRAGScoreThreshold
	WithRAGEmitHintEvent        = asmiddleware.WithRAGEmitHintEvent
	WithRAGPersistHint          = asmiddleware.WithRAGPersistHint
	WithRAGHintTemplate         = asmiddleware.WithRAGHintTemplate
	WithRAGRequireConfirm       = asmiddleware.WithRAGRequireConfirm

	ErrRAGInvalidInput       = asrag.ErrInvalidInput
	ErrRAGUnsupportedContent = asrag.ErrUnsupportedContent
	ErrRAGEmbeddingMismatch  = asrag.ErrEmbeddingMismatch
)
