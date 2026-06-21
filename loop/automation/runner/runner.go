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

package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/loop/automation/event"
	"github.com/yuluo-yx/agentscope-go/loop/automation/gate"
	"github.com/yuluo-yx/agentscope-go/loop/automation/store"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// Runner handles generic events by routing them to loop-enabled Agent runs.
type Runner struct {
	Router    event.Router
	Mapper    InputMapper
	Store     store.RunStore
	Agents    AgentResolver
	Gate      gate.Gate
	Workspace WorkspaceAllocator
	Yield     func(context.Context, event.Event, message.Event) error
}

// HandleEvent maps one event to an Agent run and records the result.
func (r Runner) HandleEvent(ctx context.Context, evt event.Event) error {
	if err := r.validateEventHandling(ctx, evt); err != nil {
		return err
	}

	record, skip, err := r.recordIncomingEvent(ctx, evt)
	if err != nil || skip {
		return err
	}

	decision, gateStopped, runErr := r.routeAndGate(ctx, evt, &record)
	if gateStopped {
		return r.Store.RecordRun(ctx, record)
	}

	var lease WorkspaceLease
	if runErr == nil {
		lease, decision, runErr = r.allocateWorkspace(ctx, evt, decision, &record)
	}

	var input *message.Message
	if runErr == nil {
		input, runErr = r.mapEventInput(ctx, evt, decision)
	}

	var agent *agentpkg.Agent
	if runErr == nil {
		agent, runErr = r.resolveRunAgent(ctx, decision, &record)
	}

	if runErr == nil {
		runErr = r.runAgentReply(ctx, evt, agent, input)
	}
	return r.finishRunRecord(ctx, record, agent, lease, runErr)
}

func (r Runner) validateEventHandling(ctx context.Context, evt event.Event) error {
	if ctx == nil {
		return fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := evt.Validate(); err != nil {
		return err
	}
	if r.Router == nil {
		return fmt.Errorf("automation: router is nil")
	}
	if r.Store == nil {
		return fmt.Errorf("automation: run store is nil")
	}
	return nil
}

func (r Runner) recordIncomingEvent(ctx context.Context, evt event.Event) (store.RunRecord, bool, error) {
	dedupKey := evt.DeduplicationKey()
	seen, err := r.Store.SeenEvent(ctx, dedupKey)
	if err != nil {
		return store.RunRecord{}, false, err
	}
	if seen {
		return store.RunRecord{}, true, nil
	}
	if err := r.Store.RecordEvent(ctx, evt); err != nil {
		return store.RunRecord{}, false, err
	}

	startedAt := time.Now()
	return store.RunRecord{
		ID:          utils.NewID(),
		EventID:     evt.ID,
		EventSource: evt.Source,
		EventType:   evt.Type,
		DedupKey:    dedupKey,
		StartedAt:   startedAt,
	}, false, nil
}

func (r Runner) routeAndGate(ctx context.Context, evt event.Event, record *store.RunRecord) (event.RouteDecision, bool, error) {
	decision, runErr := r.Router.Route(ctx, evt)
	if runErr != nil {
		return decision, false, runErr
	}
	record.LoopName = decision.LoopName
	record.AgentName = decision.AgentName
	if r.Gate == nil {
		return decision, false, nil
	}

	gateDecision, err := r.Gate.Evaluate(ctx, evt.Clone(), decision.Clone())
	if err != nil {
		return decision, false, err
	}
	if !gateDecision.RequiresStop() {
		return decision, false, nil
	}
	record.StopReason = gateDecision.StopReason
	record.GateReason = gateDecision.Reason
	record.GateMetadata = event.CloneStringMap(gateDecision.Metadata)
	record.FinishedAt = time.Now()
	return decision, true, nil
}

func (r Runner) allocateWorkspace(
	ctx context.Context,
	evt event.Event,
	decision event.RouteDecision,
	record *store.RunRecord,
) (WorkspaceLease, event.RouteDecision, error) {
	if r.Workspace == nil {
		return nil, decision, nil
	}
	lease, err := r.Workspace.Allocate(ctx, evt.Clone(), decision.Clone())
	if err != nil {
		return nil, decision, err
	}
	if lease == nil {
		return nil, decision, fmt.Errorf("automation: workspace lease is nil")
	}
	record.WorkspaceRoot = lease.Root()
	record.WorkspaceMetadata = event.CloneStringMap(lease.Metadata())
	return lease, withWorkspaceMetadata(decision, record.WorkspaceRoot, record.WorkspaceMetadata), nil
}

func (r Runner) mapEventInput(ctx context.Context, evt event.Event, decision event.RouteDecision) (*message.Message, error) {
	if r.Mapper == nil {
		return nil, fmt.Errorf("automation: input mapper is nil")
	}
	return r.Mapper.MapInput(ctx, evt, decision)
}

func (r Runner) resolveRunAgent(
	ctx context.Context,
	decision event.RouteDecision,
	record *store.RunRecord,
) (*agentpkg.Agent, error) {
	if r.Agents == nil {
		return nil, fmt.Errorf("automation: agent resolver is nil")
	}
	resolved, err := r.Agents.ResolveAgent(ctx, decision)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, fmt.Errorf("automation: resolved agent is nil")
	}
	if record.AgentName == "" {
		record.AgentName = resolved.AgentName()
	}
	return resolved, nil
}

func (r Runner) runAgentReply(ctx context.Context, evt event.Event, agent *agentpkg.Agent, input *message.Message) error {
	return agent.ReplyStream(ctx, input, func(streamEvent message.Event) error {
		if r.Yield == nil {
			return nil
		}
		return r.Yield(ctx, evt.Clone(), streamEvent)
	})
}

func (r Runner) finishRunRecord(
	ctx context.Context,
	record store.RunRecord,
	agent *agentpkg.Agent,
	lease WorkspaceLease,
	runErr error,
) error {
	closeErr := closeWorkspaceLease(ctx, lease)

	record.FinishedAt = time.Now()
	if runErr != nil {
		record.Error = runErr.Error()
	}
	if closeErr != nil {
		if record.Error == "" {
			record.Error = closeErr.Error()
		} else {
			record.Error = errors.Join(runErr, closeErr).Error()
		}
	}
	if agent != nil {
		fillRunRecordFromAgent(&record, agent)
	}
	storeErr := r.Store.RecordRun(ctx, record)
	if runErr != nil || closeErr != nil || storeErr != nil {
		return errors.Join(runErr, closeErr, storeErr)
	}
	return nil
}

func closeWorkspaceLease(ctx context.Context, lease WorkspaceLease) error {
	if lease == nil {
		return nil
	}
	return lease.Close(context.WithoutCancel(ctx))
}

func withWorkspaceMetadata(decision event.RouteDecision, root string, metadata map[string]string) event.RouteDecision {
	cp := decision.Clone()
	if cp.Metadata == nil {
		cp.Metadata = map[string]any{}
	}
	if root != "" {
		cp.Metadata[RouteMetadataWorkspaceRoot] = root
	}
	if len(metadata) > 0 {
		cp.Metadata[RouteMetadataWorkspaceMetadata] = event.CloneStringMap(metadata)
	}
	return cp
}

func fillRunRecordFromAgent(record *store.RunRecord, agentValue *agentpkg.Agent) {
	if record == nil || agentValue == nil || agentValue.AgentState() == nil {
		return
	}
	state := agentValue.AgentState()
	record.ReplyID = state.ReplyID
	record.SessionID = state.SessionID
	if record.AgentName == "" {
		record.AgentName = agentValue.AgentName()
	}
	if state.LoopContext != nil {
		loopCtx := state.LoopContext
		record.ModelCalls = loopCtx.ModelCalls
		record.ToolCalls = loopCtx.ToolCalls
		record.InputTokens = loopCtx.InputTokens
		record.OutputTokens = loopCtx.OutputTokens
		record.StopReason = string(loopCtx.StopReason)
	}
}
