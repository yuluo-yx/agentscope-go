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

package goal

import (
	"context"
	"fmt"
	"time"

	"github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/loop/automation/store"
	"github.com/yuluo-yx/agentscope-go/loop/core"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/utils"
)

const (
	// GoalStopCompleted means the verifier accepted the goal.
	GoalStopCompleted = "completed"
	// GoalStopMaxAttempts means ContinuePolicy.MaxAttempts stopped the goal.
	GoalStopMaxAttempts = "max_attempts"
	// GoalStopMaxDuration means ContinuePolicy.MaxDuration stopped the goal.
	GoalStopMaxDuration = "max_duration"
	// GoalStopWaitingUser means the Agent requested user confirmation.
	GoalStopWaitingUser = "waiting_user"
	// GoalStopWaitingExternal means the Agent requested external execution.
	GoalStopWaitingExternal = "waiting_external"
	// GoalStopError means the goal stopped because an error occurred.
	GoalStopError = "error"
)

// ContinuePolicy bounds a goal across multiple Agent runs.
type ContinuePolicy struct {
	MaxAttempts       int
	MaxDuration       time.Duration
	StopOnWaitingUser bool
	StopOnExternal    bool
}

func (p ContinuePolicy) maxAttempts() int {
	if p.MaxAttempts <= 0 {
		return 1
	}
	return p.MaxAttempts
}

// GoalResult summarizes a run-until-done goal execution.
type GoalResult struct {
	Completed        bool
	StopReason       string
	Attempts         int
	LastVerification core.VerificationResult
}

// GoalRunEvent is emitted by GoalRunner after each attempt when Yield is set.
type GoalRunEvent struct {
	Attempt      int
	Run          store.RunRecord
	Report       store.LoopReport
	Verification core.VerificationResult
}

// GoalRunner keeps running an Agent until a verifier accepts the goal or a
// continue policy stops it.
type GoalRunner struct {
	Agent    *agent.Agent
	Spec     core.Spec
	Verifier core.Verifier
	Store    store.RunStore
	Sink     store.Sink
	Policy   ContinuePolicy
	Mapper   NextActionMapper
	Yield    func(context.Context, GoalRunEvent) error
}

// Run executes the goal from initial input.
func (r GoalRunner) Run(ctx context.Context, initial *message.Message) (GoalResult, error) {
	if err := r.validateRunInput(ctx, initial); err != nil {
		return GoalResult{}, err
	}

	startedAt := time.Now()
	input := initial
	var result GoalResult
	for attempt := 1; attempt <= r.Policy.maxAttempts(); attempt++ {
		if r.Policy.MaxDuration > 0 && time.Since(startedAt) >= r.Policy.MaxDuration {
			result.StopReason = GoalStopMaxDuration
			result.Attempts = attempt - 1
			return result, nil
		}

		attemptResult, next, err := r.runGoalAttempt(ctx, input, attempt)
		result = attemptResult
		if err != nil {
			return result, err
		}
		if result.StopReason != "" {
			return result, nil
		}
		input = next
	}
	return result, nil
}

func (r GoalRunner) validateRunInput(ctx context.Context, initial *message.Message) error {
	if ctx == nil {
		return fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Agent == nil {
		return fmt.Errorf("automation: goal runner agent is nil")
	}
	if r.Verifier == nil {
		return fmt.Errorf("automation: goal runner verifier is nil")
	}
	if r.Store == nil {
		return fmt.Errorf("automation: goal runner store is nil")
	}
	if initial == nil {
		return fmt.Errorf("automation: initial message is nil")
	}
	return nil
}

func (r GoalRunner) runGoalAttempt(ctx context.Context, input *message.Message, attempt int) (GoalResult, *message.Message, error) {
	run, runErr := r.executeAgentAttempt(ctx, input)
	if runErr != nil {
		return GoalResult{StopReason: GoalStopError, Attempts: attempt}, nil, runErr
	}

	verificationInput := r.verificationInput()
	verification := r.verifyGoalAttempt(ctx, verificationInput)
	result, run := r.applyVerificationOutcome(attempt, run, verification)
	report := reportFromAttempt(run, verification)
	if err := r.persistGoalAttempt(ctx, attempt, run, report, verification); err != nil {
		return result, nil, err
	}
	if result.StopReason != "" {
		return result, nil, nil
	}
	next, err := r.mapNextGoalInput(ctx, verificationInput, verification)
	return result, next, err
}

func (r GoalRunner) executeAgentAttempt(ctx context.Context, input *message.Message) (store.RunRecord, error) {
	runStartedAt := time.Now()
	runErr := r.Agent.ReplyStream(ctx, input, nil)
	runFinishedAt := time.Now()
	run := runRecordFromAgent(r.Agent, store.RunRecord{
		ID:         utils.NewID(),
		LoopName:   r.Spec.Name,
		AgentName:  r.Agent.AgentName(),
		StartedAt:  runStartedAt,
		FinishedAt: runFinishedAt,
	})
	if runErr != nil {
		run.Error = runErr.Error()
		run.StopReason = GoalStopError
		_ = r.Store.RecordRun(ctx, run)
	}
	return run, runErr
}

func (r GoalRunner) verifyGoalAttempt(ctx context.Context, input core.VerificationInput) core.VerificationResult {
	verification, err := r.Verifier.Verify(ctx, input)
	if err != nil {
		return core.VerificationResult{
			Passed:     false,
			Reason:     err.Error(),
			NextAction: "escalate",
		}
	}
	return verification
}

func (r GoalRunner) applyVerificationOutcome(attempt int, run store.RunRecord, verification core.VerificationResult) (GoalResult, store.RunRecord) {
	result := GoalResult{Attempts: attempt, LastVerification: verification}
	switch {
	case verification.Passed:
		result.Completed = true
		result.StopReason = GoalStopCompleted
		run.StopReason = GoalStopCompleted
	case shouldStopOnAgentState(r.Policy, r.Agent.AgentState()):
		result.StopReason = stopReasonFromAgentState(r.Agent.AgentState())
		run.StopReason = result.StopReason
	case attempt >= r.Policy.maxAttempts():
		result.StopReason = GoalStopMaxAttempts
		run.StopReason = GoalStopMaxAttempts
	default:
		run.StopReason = string(state.LoopStopVerifierFailed)
	}
	return result, run
}

func (r GoalRunner) persistGoalAttempt(
	ctx context.Context,
	attempt int,
	run store.RunRecord,
	report store.LoopReport,
	verification core.VerificationResult,
) error {
	if err := r.Store.RecordRun(ctx, run); err != nil {
		return err
	}
	if recorder, ok := r.Store.(store.ReportRecorder); ok {
		if err := recorder.RecordReport(ctx, report); err != nil {
			return err
		}
	}
	if r.Sink != nil {
		if err := r.Sink.PublishRun(ctx, run, report); err != nil {
			return err
		}
	}
	if r.Yield != nil {
		event := GoalRunEvent{Attempt: attempt, Run: run, Report: report, Verification: verification}
		if err := r.Yield(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (r GoalRunner) mapNextGoalInput(
	ctx context.Context,
	input core.VerificationInput,
	verification core.VerificationResult,
) (*message.Message, error) {
	if r.Mapper == nil {
		return nil, fmt.Errorf("automation: next action mapper is nil")
	}
	return r.Mapper.MapNextAction(ctx, input, verification)
}

func (r GoalRunner) verificationInput() core.VerificationInput {
	var agentState *state.AgentState
	sessionID := ""
	replyID := ""
	if r.Agent != nil && r.Agent.AgentState() != nil {
		agentState = r.Agent.AgentState().Clone()
		sessionID = r.Agent.AgentState().SessionID
		replyID = r.Agent.AgentState().ReplyID
	}
	agentName := ""
	if r.Agent != nil {
		agentName = r.Agent.AgentName()
	}
	return core.VerificationInput{
		AgentName: agentName,
		SessionID: sessionID,
		ReplyID:   replyID,
		Spec:      r.Spec,
		State:     agentState,
	}
}

func runRecordFromAgent(agentValue *agent.Agent, record store.RunRecord) store.RunRecord {
	if agentValue == nil || agentValue.AgentState() == nil {
		return record
	}
	state := agentValue.AgentState()
	record.ReplyID = state.ReplyID
	record.SessionID = state.SessionID
	if state.LoopContext != nil {
		loopCtx := state.LoopContext
		record.ModelCalls = loopCtx.ModelCalls
		record.ToolCalls = loopCtx.ToolCalls
		record.InputTokens = loopCtx.InputTokens
		record.OutputTokens = loopCtx.OutputTokens
		if record.StopReason == "" {
			record.StopReason = string(loopCtx.StopReason)
		}
	}
	return record
}

func reportFromAttempt(run store.RunRecord, verification core.VerificationResult) store.LoopReport {
	return store.LoopReport{
		RunID:      run.ID,
		EventID:    run.EventID,
		LoopName:   run.LoopName,
		Summary:    verification.Reason,
		Evidence:   append([]string(nil), verification.Evidence...),
		NextAction: verification.NextAction,
		StopReason: run.StopReason,
		Verified:   verification.Passed,
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
	}
}

func shouldStopOnAgentState(policy ContinuePolicy, agentState *agent.AgentState) bool {
	if agentState == nil || agentState.LoopContext == nil {
		return false
	}
	switch agentState.LoopContext.StopReason {
	case state.LoopStopWaitingUser:
		return policy.StopOnWaitingUser
	case state.LoopStopWaitingExternal:
		return policy.StopOnExternal
	default:
		return false
	}
}

func stopReasonFromAgentState(agentState *agent.AgentState) string {
	if agentState == nil || agentState.LoopContext == nil {
		return ""
	}
	return string(agentState.LoopContext.StopReason)
}
