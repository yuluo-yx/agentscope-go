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

package goal_test

import (
	"context"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	goalpkg "github.com/yuluo-yx/agentscope-go/loop/automation/goal"
	automationstore "github.com/yuluo-yx/agentscope-go/loop/automation/store"
	"github.com/yuluo-yx/agentscope-go/loop/core"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	statepkg "github.com/yuluo-yx/agentscope-go/state"
)

func TestGoalRunnerRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	validAgent, initial := newGoalRunnerValidationFixtures(t)
	validVerifier := core.VerifierFunc(func(context.Context, core.VerificationInput) (core.VerificationResult, error) {
		return core.VerificationResult{Passed: true}, nil
	})
	validStore := automationstore.NewMemoryRunStore()
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	var nilCtx context.Context

	tests := []struct {
		name     string
		ctx      context.Context
		runner   goalpkg.GoalRunner
		initial  *message.Message
		contains string
	}{
		{name: "nil context", ctx: nilCtx, runner: goalpkg.GoalRunner{Agent: validAgent, Verifier: validVerifier, Store: validStore}, initial: initial, contains: "context is nil"},
		{name: "canceled context", ctx: canceledCtx, runner: goalpkg.GoalRunner{Agent: validAgent, Verifier: validVerifier, Store: validStore}, initial: initial, contains: context.Canceled.Error()},
		{name: "nil agent", ctx: context.Background(), runner: goalpkg.GoalRunner{Verifier: validVerifier, Store: validStore}, initial: initial, contains: "agent is nil"},
		{name: "nil verifier", ctx: context.Background(), runner: goalpkg.GoalRunner{Agent: validAgent, Store: validStore}, initial: initial, contains: "verifier is nil"},
		{name: "nil store", ctx: context.Background(), runner: goalpkg.GoalRunner{Agent: validAgent, Verifier: validVerifier}, initial: initial, contains: "store is nil"},
		{name: "nil initial message", ctx: context.Background(), runner: goalpkg.GoalRunner{Agent: validAgent, Verifier: validVerifier, Store: validStore}, contains: "initial message is nil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.runner.Run(tt.ctx, tt.initial)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("Run error = %v, want containing %q", err, tt.contains)
			}
		})
	}
}

func TestGoalRunnerStopsWhenAgentWaitsForUser(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("needs approval")}, true),
	}}
	agentState := statepkg.NewAgentState()
	agentState.LoopContext = &statepkg.LoopContext{StopReason: statepkg.LoopStopWaitingUser}
	agent, err := agentpkg.NewAgent("Friday", "Ask for approval.", model, agentpkg.WithAgentState(agentState))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	initial, err := message.NewUserMessage("user", "Check release readiness.")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	store := automationstore.NewMemoryRunStore()
	runner := goalpkg.GoalRunner{
		Agent: agent,
		Spec:  core.Spec{Name: "release-check", Goal: "wait for approval", Mode: core.ModeReportOnly},
		Verifier: core.VerifierFunc(func(context.Context, core.VerificationInput) (core.VerificationResult, error) {
			return core.VerificationResult{Passed: false, Reason: "requires approval", NextAction: "ask user"}, nil
		}),
		Store:  store,
		Policy: goalpkg.ContinuePolicy{MaxAttempts: 3, StopOnWaitingUser: true},
	}

	result, err := runner.Run(context.Background(), initial)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Completed || result.StopReason != goalpkg.GoalStopWaitingUser || result.Attempts != 1 {
		t.Fatalf("GoalResult mismatch: %#v", result)
	}
	runs := store.Runs()
	if len(runs) != 1 || runs[0].StopReason != goalpkg.GoalStopWaitingUser {
		t.Fatalf("run stop reason mismatch: %#v", runs)
	}
}

func TestGoalRunnerRequiresMapperBeforeContinuing(t *testing.T) {
	t.Parallel()

	agent, initial := newGoalRunnerValidationFixtures(t)
	store := automationstore.NewMemoryRunStore()
	runner := goalpkg.GoalRunner{
		Agent: agent,
		Spec:  core.Spec{Name: "ci-sweeper", Goal: "produce a verified CI summary", Mode: core.ModeReportOnly},
		Verifier: core.VerifierFunc(func(context.Context, core.VerificationInput) (core.VerificationResult, error) {
			return core.VerificationResult{Passed: false, Reason: "missing evidence", NextAction: "add evidence"}, nil
		}),
		Store:  store,
		Policy: goalpkg.ContinuePolicy{MaxAttempts: 2},
	}

	result, err := runner.Run(context.Background(), initial)
	if err == nil || !strings.Contains(err.Error(), "next action mapper is nil") {
		t.Fatalf("Run error = %v, want next action mapper is nil", err)
	}
	if result.StopReason != "" || result.Attempts != 1 {
		t.Fatalf("GoalResult should preserve retryable state before mapper error: %#v", result)
	}
	if runs := store.Runs(); len(runs) != 1 || runs[0].StopReason != string(statepkg.LoopStopVerifierFailed) {
		t.Fatalf("first failed run should be recorded before mapper error: %#v", runs)
	}
}

func newGoalRunnerValidationFixtures(t *testing.T) (*agentpkg.Agent, *message.Message) {
	t.Helper()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("attempt")}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("second attempt")}, true),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Summarize.", model)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	initial, err := message.NewUserMessage("user", "Summarize the failing CI run.")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	return agent, initial
}
