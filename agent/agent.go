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

package agent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	agenterrors "github.com/yuluo-yx/agentscope-go/errors"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/utils"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

// ToolProvider is the minimal tool registry interface required by Agent.
type ToolProvider interface {
	// ToolSchemas returns model-facing tool schemas filtered by the active group names in agent state.
	ToolSchemas(activeGroups ...string) ([]ToolSchema, error)
	// FindTool looks up a tool by ToolCallBlock.Name and the currently active groups.
	FindTool(name string, activeGroups ...string) (Tool, bool)
	// CallTool executes a parsed tool call against the provided AgentState and returns streamed chunks.
	// The Agent owns permission checks and event emission; the provider owns dispatching to the concrete tool.
	CallTool(context.Context, *message.ToolCallBlock, *AgentState) (<-chan ToolChunk, error)
}

// AgentOption configures an Agent.
type AgentOption func(*Agent) error

// Agent runs ReAct reasoning, permission checks, and tool calls.
type Agent struct {
	name              string
	systemPrompt      string
	model             ChatModel
	toolkit           ToolProvider
	middlewareTools   []Tool
	middlewareToolkit ToolProvider
	state             *AgentState
	offloader         asworkspace.Offloader

	modelConfig       ModelConfig
	contextConfig     ContextConfig
	reactConfig       ReActConfig
	contextStrategies []ContextStrategy

	replyHooks           []ReplyHook
	reasoningHooks       []ReasoningHook
	actingHooks          []ActingHook
	modelCallHooks       []ModelCallHook
	compressContextHooks []CompressContextHook
	systemPromptHooks    []SystemPromptHook
}

// WithToolkit sets the toolkit used by the Agent.
func WithToolkit(toolkit ToolProvider) AgentOption {
	return func(agent *Agent) error {
		if toolkit == nil {
			return agenterrors.NewDeveloperError("agent toolkit is nil")
		}
		agent.toolkit = toolkit
		return nil
	}
}

// WithOffloader sets the offloader used for context, tool result, and DataBlock lifecycle.
func WithOffloader(offloader asworkspace.Offloader) AgentOption {
	return func(agent *Agent) error {
		if offloader == nil {
			return agenterrors.NewDeveloperError("agent offloader is nil")
		}
		agent.offloader = offloader
		return nil
	}
}

// WithWorkspace initializes a workspace and wires instructions, tools, skills, MCPs, and offloader into the Agent.
func WithWorkspace(ctx context.Context, workspace asworkspace.Workspace) AgentOption {
	return func(agent *Agent) error {
		resources, err := asworkspace.BuildAgentResources(ctx, workspace)
		if err != nil {
			return agenterrors.NewDeveloperError("invalid agent workspace", agenterrors.WithErrorCause(err))
		}
		agent.systemPrompt = appendSystemPrompt(agent.systemPrompt, resources.SystemPrompt)
		agent.toolkit = resources.Toolkit
		agent.offloader = resources.Offloader
		return nil
	}
}

// WithAgentResources wires prebuilt workspace resources into the Agent.
func WithAgentResources(resources *asworkspace.AgentResources) AgentOption {
	return func(agent *Agent) error {
		if resources == nil {
			return agenterrors.NewDeveloperError("agent resources are nil")
		}
		agent.systemPrompt = appendSystemPrompt(agent.systemPrompt, resources.SystemPrompt)
		if resources.Toolkit != nil {
			agent.toolkit = resources.Toolkit
		}
		if resources.Offloader != nil {
			agent.offloader = resources.Offloader
		}
		return nil
	}
}

// WithAgentState sets the initial Agent state.
func WithAgentState(state *AgentState) AgentOption {
	return func(agent *Agent) error {
		if state == nil {
			return agenterrors.NewDeveloperError("agent state is nil")
		}
		agent.state = state
		return nil
	}
}

// WithModelConfig sets model call configuration.
func WithModelConfig(config ModelConfig) AgentOption {
	return func(agent *Agent) error {
		if err := config.Validate(); err != nil {
			return agenterrors.NewDeveloperError("invalid model config", agenterrors.WithErrorCause(err))
		}
		agent.modelConfig = config
		return nil
	}
}

// WithContextConfig sets context management configuration.
func WithContextConfig(config ContextConfig) AgentOption {
	return func(agent *Agent) error {
		if err := config.Validate(); err != nil {
			return agenterrors.NewDeveloperError("invalid context config", agenterrors.WithErrorCause(err))
		}
		agent.contextConfig = config
		return nil
	}
}

// WithContextStrategies replaces the default context compression strategy chain.
func WithContextStrategies(strategies ...ContextStrategy) AgentOption {
	return func(agent *Agent) error {
		for _, strategy := range strategies {
			if strategy == nil {
				return agenterrors.NewDeveloperError("agent context strategy is nil")
			}
		}
		agent.contextStrategies = append([]ContextStrategy(nil), strategies...)
		return nil
	}
}

// WithReActConfig sets ReAct loop configuration.
func WithReActConfig(config ReActConfig) AgentOption {
	return func(agent *Agent) error {
		if err := config.Validate(); err != nil {
			return agenterrors.NewDeveloperError("invalid ReAct config", agenterrors.WithErrorCause(err))
		}
		agent.reactConfig = config
		return nil
	}
}

// WithMiddlewares registers middleware values by the hook interfaces they implement.
func WithMiddlewares(middlewares ...Middleware) AgentOption {
	return func(agent *Agent) error {
		for _, middleware := range middlewares {
			if middleware == nil {
				return agenterrors.NewDeveloperError("agent middleware is nil")
			}
			if err := agent.registerMiddleware(context.Background(), middleware); err != nil {
				return err
			}
		}
		return nil
	}
}

// NewAgent creates an Agent.
func NewAgent(name, systemPrompt string, model ChatModel, opts ...AgentOption) (*Agent, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, agenterrors.NewDeveloperError("agent name is empty")
	}
	if model == nil {
		return nil, agenterrors.NewDeveloperError("agent model is nil")
	}
	agent := &Agent{
		name:              name,
		systemPrompt:      systemPrompt,
		model:             model,
		toolkit:           emptyToolProvider{},
		state:             NewAgentState(),
		modelConfig:       DefaultModelConfig(),
		contextConfig:     DefaultContextConfig(),
		reactConfig:       DefaultReActConfig(),
		contextStrategies: DefaultContextStrategies(),
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(agent); err != nil {
			return nil, err
		}
	}
	return agent, nil
}

func appendSystemPrompt(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	switch {
	case base == "":
		return extra
	case extra == "":
		return base
	default:
		return base + "\n" + extra
	}
}

// AgentName returns the Agent name.
func (a *Agent) AgentName() string {
	if a == nil {
		return ""
	}
	return a.name
}

// AgentState returns the current Agent state.
func (a *Agent) AgentState() *AgentState {
	if a == nil {
		return nil
	}
	return a.state
}

// ReplyStream runs one reply and yields events one by one.
func (a *Agent) ReplyStream(ctx context.Context, input any, yield func(message.Event) error) error {
	if a == nil {
		return agenterrors.NewDeveloperError("agent is nil")
	}
	if yield == nil {
		yield = func(message.Event) error { return nil }
	}
	if len(a.replyHooks) == 0 {
		return a.runReply(ctx, input, yield)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, streamErr, err := a.replyEventStream(streamCtx, input)
	if err != nil {
		return err
	}
	for event := range events {
		if err := yield(event); err != nil {
			cancel()
			return err
		}
	}
	return streamErr()
}

// Reply runs one reply and returns the current assistant message.
func (a *Agent) Reply(ctx context.Context, input any) (*message.Message, error) {
	if err := a.ReplyStream(ctx, input, nil); err != nil {
		return nil, err
	}
	last := a.lastMessage()
	if last == nil || last.Role != message.RoleAssistant || last.ID != a.state.ReplyID {
		return nil, agenterrors.NewDeveloperError("agent did not produce a final assistant message")
	}
	return last.Clone(), nil
}

func (a *Agent) replyEventStream(ctx context.Context, input any) (<-chan message.Event, func() error, error) {
	var finalCalled atomic.Bool
	errs := make(chan error, 1)
	final := func(ctx context.Context) (<-chan message.Event, error) {
		finalCalled.Store(true)
		events := make(chan message.Event)
		go func() {
			errs <- a.runReply(ctx, input, func(event message.Event) error {
				select {
				case events <- event:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			close(events)
		}()
		return events, nil
	}
	events, err := a.applyReplyHooks(ctx, HookInput{"input": input}, final)
	if err != nil {
		return nil, nil, err
	}
	return events, func() error {
		if !finalCalled.Load() {
			return nil
		}
		select {
		case err := <-errs:
			return err
		default:
			return nil
		}
	}, nil
}

func (a *Agent) runReply(ctx context.Context, input any, emit func(message.Event) error) error {
	if err := a.appendInput(input); err != nil {
		return err
	}
	if assistant := a.resumableAssistant(); assistant != nil {
		a.state.ReplyID = assistant.ID
		return a.continueReply(ctx, assistant, emit)
	}
	a.state.ReplyID = utils.NewID()
	a.state.CurIter = 0
	assistant, err := message.NewAssistantMessage(a.name, nil, message.WithMessageID(a.state.ReplyID))
	if err != nil {
		return err
	}
	a.state.Context = append(a.state.Context, assistant)

	if err := a.emitAndApply(assistant, message.NewReplyStartEvent(a.state.SessionID, a.state.ReplyID, a.name), emit); err != nil {
		return err
	}
	return a.continueReply(ctx, assistant, emit)
}

func (a *Agent) continueReply(ctx context.Context, assistant *message.Message, emit func(message.Event) error) error {
	for a.state.CurIter < a.reactConfig.MaxIters {
		if err := a.CompressContext(ctx); err != nil {
			return err
		}
		if pending := pendingToolCalls(assistant); len(pending) > 0 {
			waiting, err := a.runActing(ctx, assistant, pending, emit)
			if err != nil {
				return err
			}
			if waiting {
				return a.emitAndApply(assistant, message.NewReplyEndEvent(a.state.SessionID, a.state.ReplyID), emit)
			}
			if err := a.CompressContext(ctx); err != nil {
				return err
			}
			a.state.CurIter++
			continue
		}
		if err := a.runReasoning(ctx, assistant, emit); err != nil {
			return err
		}
		pending := pendingToolCalls(assistant)
		if len(pending) == 0 {
			return a.emitAndApply(assistant, message.NewReplyEndEvent(a.state.SessionID, a.state.ReplyID), emit)
		}
		waiting, err := a.runActing(ctx, assistant, pending, emit)
		if err != nil {
			return err
		}
		if waiting {
			return a.emitAndApply(assistant, message.NewReplyEndEvent(a.state.SessionID, a.state.ReplyID), emit)
		}
		if err := a.CompressContext(ctx); err != nil {
			return err
		}
		a.state.CurIter++
	}
	return a.emitAndApply(assistant, message.NewExceedMaxItersEvent(a.state.ReplyID, a.name), emit)
}

func (a *Agent) resumableAssistant() *message.Message {
	last := a.lastMessage()
	if last == nil || last.Role != message.RoleAssistant || last.Name != a.name {
		return nil
	}
	if len(pendingToolCalls(last)) == 0 {
		return nil
	}
	return last
}

func (a *Agent) appendInput(input any) error {
	switch value := input.(type) {
	case nil:
		return nil
	case *message.Message:
		a.state.Context = append(a.state.Context, value.Clone())
		return nil
	case []*message.Message:
		for _, msg := range value {
			if msg != nil {
				a.state.Context = append(a.state.Context, msg.Clone())
			}
		}
		return nil
	case message.Event:
		return a.applyIncomingEvent(value)
	default:
		return fmt.Errorf("agentscope: unsupported reply input %T", input)
	}
}

func (a *Agent) applyIncomingEvent(event message.Event) error {
	last := a.lastMessage()
	if last == nil {
		return fmt.Errorf("agentscope: cannot apply incoming event without context")
	}
	if err := last.ApplyEvent(event); err != nil {
		return err
	}
	if confirm, ok := event.(*message.UserConfirmResultEvent); ok {
		for _, result := range confirm.ConfirmResults {
			for _, rule := range result.Rules {
				permission.NewEngine(a.state.PermissionContext).AddRule(rule)
			}
		}
	}
	return nil
}

func (a *Agent) emitAndApply(assistant *message.Message, event message.Event, emit func(message.Event) error) error {
	if event == nil {
		return nil
	}
	if assistant != nil {
		if err := assistant.ApplyEvent(event); err != nil {
			return err
		}
	}
	return emit(event)
}

func (a *Agent) lastMessage() *message.Message {
	if a == nil || a.state == nil || len(a.state.Context) == 0 {
		return nil
	}
	return a.state.Context[len(a.state.Context)-1]
}

func (a *Agent) registerMiddleware(ctx context.Context, middleware Middleware) error {
	if typed, ok := middleware.(ReplyMiddleware); ok {
		a.replyHooks = append(a.replyHooks, typed.OnReply)
	}
	if typed, ok := middleware.(ReasoningMiddleware); ok {
		a.reasoningHooks = append(a.reasoningHooks, typed.OnReasoning)
	}
	if typed, ok := middleware.(ActingMiddleware); ok {
		a.actingHooks = append(a.actingHooks, typed.OnActing)
	}
	if typed, ok := middleware.(ModelCallMiddleware); ok {
		a.modelCallHooks = append(a.modelCallHooks, typed.OnModelCall)
	}
	if typed, ok := middleware.(CompressContextMiddleware); ok {
		a.compressContextHooks = append(a.compressContextHooks, typed.OnCompressContext)
	}
	if typed, ok := middleware.(SystemPromptMiddleware); ok {
		a.systemPromptHooks = append(a.systemPromptHooks, typed.OnSystemPrompt)
	}
	if typed, ok := middleware.(ToolMiddleware); ok {
		tools, err := typed.ListTools(ctx, a)
		if err != nil {
			return agenterrors.NewDeveloperError(
				fmt.Sprintf("agent middleware %q failed to list tools", middleware.MiddlewareName()),
				agenterrors.WithErrorCause(err),
			)
		}
		if len(tools) > 0 {
			a.middlewareTools = append(a.middlewareTools, tools...)
			kit, err := astool.NewToolkit(a.middlewareTools...)
			if err != nil {
				return agenterrors.NewDeveloperError(
					fmt.Sprintf("agent middleware %q provided invalid tools", middleware.MiddlewareName()),
					agenterrors.WithErrorCause(err),
				)
			}
			a.middlewareToolkit = kit
		}
	}
	return nil
}

type emptyToolProvider struct{}

// ToolSchemas returns no schemas for an Agent without a configured toolkit.
func (emptyToolProvider) ToolSchemas(...string) ([]ToolSchema, error) {
	return nil, nil
}

// FindTool always misses for an Agent without a configured toolkit.
func (emptyToolProvider) FindTool(string, ...string) (Tool, bool) {
	return nil, false
}

// CallTool returns a configuration error for an Agent without a configured toolkit.
func (emptyToolProvider) CallTool(context.Context, *message.ToolCallBlock, *AgentState) (<-chan ToolChunk, error) {
	return nil, fmt.Errorf("agentscope: no toolkit configured")
}
