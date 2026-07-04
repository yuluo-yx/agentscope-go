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
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	agenterrors "github.com/yuluo-yx/agentscope-go/pkg/errors"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/utils"
	asworkspace "github.com/yuluo-yx/agentscope-go/pkg/workspace"
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
//
// Concurrency contract: Reply, ReplyStream, and Observe serialize execution
// through a mutex to protect internal Agent state consistency. Multiple
// goroutines may call these methods concurrently, but the underlying execution
// completes sequentially; high-concurrency sessions should use separate Agent
// instances.
type Agent struct {

	// Agent name, agent ID.
	name string
	// Define Agent role and behavior.
	systemPrompt string

	// Runtime properties.
	model ChatModel

	toolkit           ToolProvider
	middlewareTools   []Tool
	middlewareToolkit ToolProvider

	// mu serializes Reply, ReplyStream, and Observe to protect internal state writes.
	mu    sync.Mutex
	state *AgentState
	// Workspace offloader, used to offload context to external storage and reload
	// it on demand, in order to overcome context limits.
	offloader asworkspace.Offloader

	modelConfig ModelConfig
	// Context management strategy (window size, truncation rules, etc.)
	contextConfig ContextConfig
	// ReAct loop configuration (max iterations, termination conditions, etc.)
	reactConfig ReActConfig
	// List of context strategies (e.g., summarization, truncation), sorted by priority.
	contextStrategies []ContextStrategy
	// Auto permission classifier used when PermissionContext.Mode is permission.ModeAuto.
	autoPermissionClassifier permission.AutoPermissionClassifier
	// Security audit logger records permission and tool execution events without full inputs.
	securityAuditLogger SecurityAuditLogger

	// Agent Hooks config, allow change agent behavior without changing core logic.
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

// WithAdditionalToolkit appends a toolkit to the Agent without replacing the existing provider.
func WithAdditionalToolkit(toolkit ToolProvider) AgentOption {

	return func(agent *Agent) error {
		if toolkit == nil {
			return agenterrors.NewDeveloperError("agent toolkit is nil")
		}
		agent.toolkit = composeToolProviders(agent.toolkit, toolkit)

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
		agent.addWorkspaceRootPermission(workspace)

		return nil
	}
}

func (a *Agent) addWorkspaceRootPermission(workspace asworkspace.Workspace) {
	rooted, ok := workspace.(asworkspace.RootedWorkspace)
	if !ok || a == nil {
		return
	}
	root := strings.TrimSpace(rooted.WorkspaceRoot())
	if root == "" {
		return
	}
	if a.state == nil {
		a.state = NewAgentState()
	}
	if a.state.PermissionContext == nil {
		a.state.PermissionContext = permission.NewContext(permission.ModeDefault)
	}
	if a.state.PermissionContext.WorkingDirectories == nil {
		a.state.PermissionContext.WorkingDirectories = map[string]permission.AdditionalWorkingDirectory{}
	}
	if _, exists := a.state.PermissionContext.WorkingDirectories[root]; exists {
		return
	}
	a.state.PermissionContext.WorkingDirectories[root] = permission.AdditionalWorkingDirectory{
		Path:   root,
		Source: "session",
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

// WithAgentConfig sets model, context, and ReAct configuration together.
func WithAgentConfig(config AgentConfig) AgentOption {

	return func(agent *Agent) error {
		if err := config.Validate(); err != nil {
			return agenterrors.NewDeveloperError("invalid agent config", agenterrors.WithErrorCause(err))
		}

		clone := config.Clone()
		agent.modelConfig = clone.Model
		agent.contextConfig = clone.Context
		agent.reactConfig = clone.ReAct

		return nil
	}
}

// WithModelConfig sets model call configuration.
func WithModelConfig(config ModelConfig) AgentOption {

	return func(agent *Agent) error {
		if err := config.Validate(); err != nil {
			return agenterrors.NewDeveloperError("invalid model config", agenterrors.WithErrorCause(err))
		}

		agent.modelConfig = config.Clone()

		return nil
	}
}

// WithContextConfig sets context management configuration.
func WithContextConfig(config ContextConfig) AgentOption {

	return func(agent *Agent) error {
		if err := config.Validate(); err != nil {
			return agenterrors.NewDeveloperError("invalid context config", agenterrors.WithErrorCause(err))
		}

		agent.contextConfig = config.Clone()
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

// WithAutoPermissionClassifier sets the classifier used by auto permission mode.
func WithAutoPermissionClassifier(classifier permission.AutoPermissionClassifier) AgentOption {

	return func(agent *Agent) error {
		if classifier == nil {
			return agenterrors.NewDeveloperError("agent auto permission classifier is nil")
		}

		agent.autoPermissionClassifier = classifier
		return nil
	}
}

// WithSecurityAuditLogger sets the logger used for security audit events.
func WithSecurityAuditLogger(logger SecurityAuditLogger) AgentOption {

	return func(agent *Agent) error {
		if logger == nil {
			return agenterrors.NewDeveloperError("agent security audit logger is nil")
		}

		agent.securityAuditLogger = logger
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
		ordered, err := orderMiddlewares(middlewares)
		if err != nil {
			return err
		}
		for _, middleware := range ordered {
			if err := agent.registerMiddleware(context.Background(), middleware); err != nil {
				return err
			}
		}

		return nil
	}
}

type middlewareOrderEntry struct {
	middleware   Middleware
	name         string
	priority     int
	dependencies []string
}

func orderMiddlewares(middlewares []Middleware) ([]Middleware, error) {
	entries := make([]middlewareOrderEntry, 0, len(middlewares))
	names := make(map[string]bool, len(middlewares))

	for _, middleware := range middlewares {
		if middleware == nil {
			return nil, agenterrors.NewDeveloperError("agent middleware is nil")
		}

		name := strings.TrimSpace(middleware.MiddlewareName())
		if name == "" {
			return nil, agenterrors.NewDeveloperError("agent middleware name is empty")
		}
		names[name] = true

		priority := 0
		if typed, ok := middleware.(MiddlewarePrioritizer); ok {
			priority = typed.MiddlewarePriority()
		}

		var dependencies []string
		if typed, ok := middleware.(MiddlewareDependencyProvider); ok {
			for _, dependency := range typed.MiddlewareDependsOn() {
				dependency = strings.TrimSpace(dependency)
				if dependency == "" {
					continue
				}
				dependencies = append(dependencies, dependency)
			}
		}

		entries = append(entries, middlewareOrderEntry{
			middleware:   middleware,
			name:         name,
			priority:     priority,
			dependencies: dependencies,
		})
	}

	for _, entry := range entries {
		for _, dependency := range entry.dependencies {
			if !names[dependency] {
				return nil, agenterrors.NewDeveloperError(
					fmt.Sprintf("agent middleware %q depends on missing middleware %q", entry.name, dependency),
				)
			}
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].priority < entries[j].priority
	})

	ordered := make([]Middleware, 0, len(entries))
	done := make(map[string]bool, len(entries))
	for len(entries) > 0 {
		selected := -1
		for index, entry := range entries {
			if middlewareDependenciesReady(entry.dependencies, done) {
				selected = index
				break
			}
		}
		if selected == -1 {
			return nil, agenterrors.NewDeveloperError("agent middleware dependencies cannot be ordered")
		}

		entry := entries[selected]
		ordered = append(ordered, entry.middleware)
		done[entry.name] = true
		entries = append(entries[:selected], entries[selected+1:]...)
	}

	return ordered, nil
}

func middlewareDependenciesReady(dependencies []string, done map[string]bool) bool {
	for _, dependency := range dependencies {
		if !done[dependency] {
			return false
		}
	}
	return true
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
		name:                name,
		systemPrompt:        systemPrompt,
		model:               model,
		toolkit:             emptyToolProvider{},
		state:               NewAgentState(),
		modelConfig:         DefaultAgentConfig().Model,
		contextConfig:       DefaultAgentConfig().Context,
		reactConfig:         DefaultAgentConfig().ReAct,
		contextStrategies:   DefaultContextStrategies(),
		securityAuditLogger: slogSecurityAuditLogger{},
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

	// Serialize the reply flow to avoid concurrent Agent state mutation.
	a.mu.Lock()
	defer a.mu.Unlock()

	// If no any hooks, exec.
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
		// callback api, stop streaming if callback returns error.
		if err := yield(event); err != nil {
			cancel()
			return err
		}
	}

	return streamErr()
}

// Reply runs one reply and returns the current assistant message.
func (a *Agent) Reply(ctx context.Context, input any) (*message.Message, error) {

	if a == nil {
		return nil, agenterrors.NewDeveloperError("agent is nil")
	}

	// Use the same lock as ReplyStream to avoid re-entering the reply flow.
	a.mu.Lock()
	defer a.mu.Unlock()

	// Run the underlying flow directly to avoid calling ReplyStream and locking twice.
	if len(a.replyHooks) == 0 {
		if err := a.runReply(ctx, input, nil); err != nil {
			return nil, err
		}
	} else {
		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		events, streamErr, err := a.replyEventStream(streamCtx, input)
		if err != nil {
			return nil, err
		}
		for range events {
		}
		if err := streamErr(); err != nil {
			return nil, err
		}
	}

	// The lock is already held, so the last message can be read directly.
	if a.state == nil || len(a.state.Context) == 0 {
		return nil, agenterrors.NewDeveloperError("agent did not produce a final assistant message")
	}
	last := a.state.Context[len(a.state.Context)-1]
	if last == nil || last.Role != message.RoleAssistant || last.ID != a.state.ReplyID {
		return nil, agenterrors.NewDeveloperError("agent did not produce a final assistant message")
	}

	return last.Clone(), nil
}

// Observe receives external observations and applies them to Agent state without
// starting a reply turn. Messages are appended to context; events are replayed
// against the assistant message with the matching reply id.
func (a *Agent) Observe(ctx context.Context, input any) error {

	if a == nil {
		return agenterrors.NewDeveloperError("agent is nil")
	}
	if a.state == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Serialize the observe flow to avoid concurrent Agent state mutation.
	a.mu.Lock()
	defer a.mu.Unlock()

	switch value := input.(type) {
	default:
		return fmt.Errorf("agentscope: unsupported observe input %T", input)
	case nil:
		return nil
	case *message.Message:
		return a.observeMessage(value)
	case []*message.Message:
		for _, msg := range value {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := a.observeMessage(msg); err != nil {
				return err
			}
		}
		return nil
	case message.Event:
		return a.observeEvent(value)
	case []message.Event:
		for _, event := range value {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := a.observeEvent(event); err != nil {
				return err
			}
		}
		return nil
	}
}

func (a *Agent) replyEventStream(ctx context.Context, input any) (<-chan message.Event, func() error, error) {

	var finalCalled atomic.Bool

	// make channle, buffer 1 to avoid goroutine leak if final is called but runReply is not started yet.
	errs := make(chan error, 1)

	final := func(ctx context.Context) (<-chan message.Event, error) {
		finalCalled.Store(true)
		events := make(chan message.Event)

		go func() {
			errs <- a.runReply(
				ctx,
				input,
				func(event message.Event) error {
					select {
					case events <- event:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				},
			)
			close(events)
		}()

		return events, nil
	}

	events, err := a.applyReplyHooks(ctx, HookInput{"input": input}, final)
	if err != nil {
		return nil, nil, err
	}

	returnFunc := func() error {
		if !finalCalled.Load() {
			return nil
		}
		select {
		case err := <-errs:
			return err
		default:
			return nil
		}
	}

	return events, returnFunc, nil
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

// Check whether the current conversation can resume from where it was interrupted,
// allowing the Agent to continue processing unfinished tool calls from the previous
// assistant message after the user grants permission or external tool results are returned.
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
	default:
		return fmt.Errorf("agentscope: unsupported reply input %T", input)
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
	}
}

func (a *Agent) applyIncomingEvent(event message.Event) error {

	last := a.lastMessage()
	if last == nil {
		return fmt.Errorf("agentscope: cannot apply incoming event without context")
	}

	return a.applyIncomingEventToMessage(last, event)
}

func (a *Agent) observeMessage(msg *message.Message) error {

	if msg == nil {
		return nil
	}
	if msg.Role == message.RoleSystem || msg.Content.HasContentBlocks("tool_call", "tool_result", "thinking") {
		return fmt.Errorf("agentscope: invalid observed message %q: role must be user or assistant and content must not contain tool calls, tool results or thinking blocks", msg.ID)
	}
	a.state.Context = append(a.state.Context, msg.Clone())

	return nil
}

func (a *Agent) observeEvent(event message.Event) error {

	if event == nil {
		return nil
	}
	for index := len(a.state.Context) - 1; index >= 0; index-- {
		msg := a.state.Context[index]
		if msg != nil && msg.ID == event.ReplyID() {
			return a.applyIncomingEventToMessage(msg, event)
		}
	}

	return fmt.Errorf("agentscope: cannot apply observed event for reply %q without matching context message", event.ReplyID())
}

func (a *Agent) applyIncomingEventToMessage(target *message.Message, event message.Event) error {

	if target == nil {
		return fmt.Errorf("agentscope: cannot apply incoming event without context")
	}
	if err := target.ApplyEvent(event); err != nil {
		return err
	}
	if confirm, ok := event.(*message.UserConfirmResultEvent); ok {
		for _, result := range confirm.ConfirmResults {
			for _, rule := range result.Rules {
				permission.NewEngine(a.state.PermissionContext).AddRule(rule)
			}
		}
	}
	if external, ok := event.(*message.ExternalExecutionResultEvent); ok {
		for _, result := range external.ExecutionResults {
			if result == nil {
				continue
			}
			if block, ok := target.FindBlock("tool_call", result.ID).(*message.ToolCallBlock); ok {
				block.State = message.ToolCallFinished
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
	if emit != nil {
		return emit(event)
	}
	return nil
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
