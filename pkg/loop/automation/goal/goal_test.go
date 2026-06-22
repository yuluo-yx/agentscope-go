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
	"errors"
	"fmt"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	goalpkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/goal"
	automationstore "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/store"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/core"
	loopruntime "github.com/yuluo-yx/agentscope-go/pkg/loop/runtime"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/pkg/model"
	statepkg "github.com/yuluo-yx/agentscope-go/pkg/state"
)

type scriptedChatModel struct {
	responses []*modelpkg.ChatResponse
}

func (m *scriptedChatModel) Name() string {
	return "scripted"
}

func (m *scriptedChatModel) Call(context.Context, modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	return m.nextResponse()
}

func (m *scriptedChatModel) Stream(ctx context.Context, _ modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	response, err := m.nextResponse()
	if err != nil {
		return nil, err
	}
	ch := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(ch)
		select {
		case ch <- *response:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (m *scriptedChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func (m *scriptedChatModel) nextResponse() (*modelpkg.ChatResponse, error) {
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func TestGoalRunnerContinuesUntilVerifierPasses(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("first attempt")}, true),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("second attempt")}, true),
	}}
	spec := core.Spec{
		Name:   "ci-sweeper",
		Goal:   "produce a verified CI summary",
		Mode:   core.ModeReportOnly,
		Policy: core.DefaultPolicy(core.ModeReportOnly),
	}
	agent, err := agentpkg.NewAgent("Friday", "You summarize CI failures.", model, loopruntime.WithSpec(spec))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	attempts := 0
	verifier := core.VerifierFunc(func(_ context.Context, input core.VerificationInput) (core.VerificationResult, error) {
		attempts++
		if input.State == nil || input.State.LoopContext == nil {
			t.Fatalf("verification input missing loop state: %#v", input)
		}
		if attempts == 1 {
			return core.VerificationResult{
				Passed:     false,
				Reason:     "missing evidence",
				Evidence:   []string{"no test output"},
				NextAction: "add verification evidence",
			}, nil
		}
		return core.VerificationResult{
			Passed:   true,
			Reason:   "evidence accepted",
			Evidence: []string{"go test ./loop/automation"},
		}, nil
	})
	mapper, err := goalpkg.NewTemplateNextActionMapper("Continue: {{.Result.NextAction}} because {{.Result.Reason}}")
	if err != nil {
		t.Fatalf("NewTemplateNextActionMapper returned error: %v", err)
	}
	store := automationstore.NewMemoryRunStore()
	runner := goalpkg.GoalRunner{
		Agent:    agent,
		Spec:     spec,
		Verifier: verifier,
		Store:    store,
		Policy:   goalpkg.ContinuePolicy{MaxAttempts: 2},
		Mapper:   mapper,
	}
	initial, err := message.NewUserMessage("user", "Summarize the failing CI run.")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	result, err := runner.Run(context.Background(), initial)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Completed || result.StopReason != goalpkg.GoalStopCompleted || result.Attempts != 2 {
		t.Fatalf("GoalResult mismatch: %#v", result)
	}
	if attempts != 2 {
		t.Fatalf("verifier attempts = %d, want 2", attempts)
	}
	runs := store.Runs()
	if len(runs) != 2 {
		t.Fatalf("recorded runs = %d, want 2", len(runs))
	}
	if runs[0].StopReason != string(statepkg.LoopStopVerifierFailed) || runs[1].StopReason != goalpkg.GoalStopCompleted {
		t.Fatalf("run stop reasons = %q, %q", runs[0].StopReason, runs[1].StopReason)
	}
	if len(store.Reports()) != 2 {
		t.Fatalf("recorded reports = %d, want 2", len(store.Reports()))
	}
}

func TestGoalRunnerStopsAtMaxAttempts(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("only attempt")}, true),
	}}
	spec := core.Spec{Name: "ci-sweeper", Goal: "produce a verified CI summary", Mode: core.ModeReportOnly}
	agent, err := agentpkg.NewAgent("Friday", "You summarize CI failures.", model, loopruntime.WithSpec(spec))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	runner := goalpkg.GoalRunner{
		Agent: agent,
		Spec:  spec,
		Verifier: core.VerifierFunc(func(context.Context, core.VerificationInput) (core.VerificationResult, error) {
			return core.VerificationResult{Passed: false, Reason: "still failing", NextAction: "try again"}, nil
		}),
		Store:  automationstore.NewMemoryRunStore(),
		Policy: goalpkg.ContinuePolicy{MaxAttempts: 1},
		Mapper: goalpkg.NextActionMapperFunc(func(context.Context, core.VerificationInput, core.VerificationResult) (*message.Message, error) {
			t.Fatalf("mapper should not be called after max attempts")
			return nil, nil
		}),
	}
	initial, err := message.NewUserMessage("user", "Summarize the failing CI run.")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	result, err := runner.Run(context.Background(), initial)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Completed || result.StopReason != goalpkg.GoalStopMaxAttempts || result.Attempts != 1 {
		t.Fatalf("GoalResult mismatch: %#v", result)
	}
}

func TestGoalRunnerPublishesRunReportToSink(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("verified")}, true),
	}}
	spec := core.Spec{Name: "release-check", Goal: "verify release readiness", Mode: core.ModeReportOnly}
	agent, err := agentpkg.NewAgent("Friday", "You verify releases.", model, loopruntime.WithSpec(spec))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	store := automationstore.NewMemoryRunStore()
	var publishedRun automationstore.RunRecord
	var publishedReport automationstore.LoopReport
	runner := goalpkg.GoalRunner{
		Agent: agent,
		Spec:  spec,
		Verifier: core.VerifierFunc(func(context.Context, core.VerificationInput) (core.VerificationResult, error) {
			return core.VerificationResult{Passed: true, Reason: "release checks passed", Evidence: []string{"go test ./..."}}, nil
		}),
		Store: store,
		Sink: automationstore.SinkFunc(func(_ context.Context, run automationstore.RunRecord, report automationstore.LoopReport) error {
			publishedRun = run
			publishedReport = report
			return nil
		}),
	}
	initial, err := message.NewUserMessage("user", "Check release readiness.")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	result, err := runner.Run(context.Background(), initial)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Completed {
		t.Fatalf("GoalResult should be completed: %#v", result)
	}
	if publishedRun.ID == "" || publishedReport.RunID != publishedRun.ID || !publishedReport.Verified {
		t.Fatalf("published run/report mismatch: %#v %#v", publishedRun, publishedReport)
	}
}

func TestGoalRunnerReturnsSinkErrorAfterRecording(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("verified")}, true),
	}}
	spec := core.Spec{Name: "release-check", Goal: "verify release readiness", Mode: core.ModeReportOnly}
	agent, err := agentpkg.NewAgent("Friday", "You verify releases.", model, loopruntime.WithSpec(spec))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	store := automationstore.NewMemoryRunStore()
	sinkErr := errors.New("sink unavailable")
	runner := goalpkg.GoalRunner{
		Agent: agent,
		Spec:  spec,
		Verifier: core.VerifierFunc(func(context.Context, core.VerificationInput) (core.VerificationResult, error) {
			return core.VerificationResult{Passed: true, Reason: "release checks passed"}, nil
		}),
		Store: store,
		Sink: automationstore.SinkFunc(func(context.Context, automationstore.RunRecord, automationstore.LoopReport) error {
			return sinkErr
		}),
	}
	initial, err := message.NewUserMessage("user", "Check release readiness.")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	result, err := runner.Run(context.Background(), initial)
	if err == nil || !strings.Contains(err.Error(), sinkErr.Error()) {
		t.Fatalf("Run error = %v, want sink error", err)
	}
	if !result.Completed {
		t.Fatalf("GoalResult should preserve verification result: %#v", result)
	}
	if len(store.Runs()) != 1 || len(store.Reports()) != 1 {
		t.Fatalf("store should keep run/report before sink failure: runs=%d reports=%d", len(store.Runs()), len(store.Reports()))
	}
}
