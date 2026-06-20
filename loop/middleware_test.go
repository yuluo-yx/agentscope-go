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

package loop_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/loop"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/permission"
	"github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/types"
)

type scriptedChatModel struct {
	responses []*modelpkg.ChatResponse
	requests  []modelpkg.CallRequest
}

func (m *scriptedChatModel) Name() string {
	return "scripted"
}

func (m *scriptedChatModel) Call(_ context.Context, request modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func (m *scriptedChatModel) Stream(ctx context.Context, request modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, fmt.Errorf("scripted model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	ch := make(chan modelpkg.ChatResponse)
	go func() {
		defer close(ch)
		select {
		case ch <- *response.Clone():
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (m *scriptedChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func TestWithSpecInjectsPromptTracksStateAndEmitsEvents(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("triage complete")},
			true,
			modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{InputTokens: 5, OutputTokens: 3}),
		),
	}}
	spec := loop.Spec{
		Name: "daily-triage",
		Goal: "scan repository signals and produce a report",
		SuccessCriteria: []loop.SuccessCriterion{
			{Name: "report", Description: "final answer contains the findings", Required: true},
		},
		Mode:   loop.ModeReportOnly,
		Policy: loop.DefaultPolicy(loop.ModeReportOnly),
	}
	agent, err := agentpkg.NewAgent("Friday", "You are helpful.", model, loop.WithSpec(spec))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	user, err := message.NewUserMessage("user", "run triage")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	var customNames []string
	if err := agent.ReplyStream(context.Background(), user, func(event message.Event) error {
		if custom, ok := event.(*message.CustomEvent); ok {
			customNames = append(customNames, custom.Name)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplyStream returned error: %v", err)
	}

	systemText := *model.requests[0].Messages[0].GetTextContent("")
	if !strings.Contains(systemText, "Loop Engineering") || !strings.Contains(systemText, spec.Goal) {
		t.Fatalf("system prompt should include loop guidance and goal, got %q", systemText)
	}
	assertCustomEvents(t, customNames, []string{
		loop.EventStart,
		loop.EventIterationStart,
		loop.EventIterationEnd,
		loop.EventStop,
	})
	loopState := agent.AgentState().LoopContext
	if loopState == nil {
		t.Fatalf("LoopContext should be initialized")
	}
	if loopState.Name != spec.Name || loopState.Goal != spec.Goal || loopState.Mode != string(spec.Mode) {
		t.Fatalf("LoopContext did not capture spec: %#v", loopState)
	}
	if loopState.ModelCalls != 1 || loopState.InputTokens != 5 || loopState.OutputTokens != 3 {
		t.Fatalf("LoopContext metrics mismatch: %#v", loopState)
	}
	if loopState.StopReason != "completed" {
		t.Fatalf("LoopContext stop reason = %q, want completed", loopState.StopReason)
	}
}

func TestWithSpecRejectsUnattendedWithoutVerifier(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{}
	_, err := agentpkg.NewAgent("Friday", "You are helpful.", model, loop.WithSpec(loop.Spec{
		Name: "ci-sweeper",
		Goal: "fix ci failures",
		Mode: loop.ModeUnattended,
		Policy: loop.Policy{
			MaxAttempts:   3,
			MaxModelCalls: 6,
			MaxToolCalls:  6,
		},
		HumanGates: []loop.HumanGate{{Name: "security", Description: "security-sensitive files require human review"}},
	}))
	if err == nil || !strings.Contains(err.Error(), "verifier") {
		t.Fatalf("NewAgent error = %v, want verifier validation error", err)
	}
}

func TestVerifierResultIsRecorded(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("fixed")}, true),
	}}
	spec := loop.Spec{
		Name:       "ci-sweeper",
		Goal:       "fix ci failures",
		Mode:       loop.ModeAssisted,
		Policy:     loop.DefaultPolicy(loop.ModeAssisted),
		HumanGates: []loop.HumanGate{{Name: "fallback", Description: "escalate unclear failures"}},
	}
	agent, err := agentpkg.NewAgent("Friday", "You are helpful.", model, loop.WithSpec(
		spec,
		loop.WithVerifier(loop.VerifierFunc(func(_ context.Context, input loop.VerificationInput) (loop.VerificationResult, error) {
			if input.Spec.Name != spec.Name || input.State == nil {
				t.Fatalf("verification input mismatch: %#v", input)
			}
			return loop.VerificationResult{Passed: false, Reason: "missing test evidence", Evidence: []string{"go test not run"}, NextAction: "escalate"}, nil
		})),
	))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	user, err := message.NewUserMessage("user", "run")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	var customNames []string
	if err := agent.ReplyStream(context.Background(), user, func(event message.Event) error {
		if custom, ok := event.(*message.CustomEvent); ok {
			customNames = append(customNames, custom.Name)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplyStream returned error: %v", err)
	}

	assertCustomEvents(t, customNames, []string{loop.EventVerifyStart, loop.EventVerifyEnd, loop.EventStop})
	verification := agent.AgentState().LoopContext.LastVerification
	if verification == nil || verification.Passed || verification.Reason != "missing test evidence" {
		t.Fatalf("verification state mismatch: %#v", verification)
	}
	if agent.AgentState().LoopContext.StopReason != "verifier_failed" {
		t.Fatalf("stop reason = %q, want verifier_failed", agent.AgentState().LoopContext.StopReason)
	}
}

func TestBudgetExhaustionForcesWrapUpModelCall(t *testing.T) {
	t.Parallel()

	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("call-1", "Lookup", "{}")},
			true,
			modelpkg.WithChatResponseUsage(&modelpkg.ChatUsage{InputTokens: 4, OutputTokens: 2}),
		),
		modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock("wrapped up")}, true),
	}}
	lookup, err := tool.NewFunctionTool(
		"Lookup",
		"look up test data",
		nil,
		func(context.Context, map[string]any, *agentpkg.AgentState) (message.ContentBlockList, error) {
			return message.ContentBlockList{message.NewTextBlock("lookup result")}, nil
		},
		tool.WithFunctionPermissionFunc(func(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
			return &permission.Decision{Behavior: permission.BehaviorAllow}, nil
		}),
	)
	if err != nil {
		t.Fatalf("NewFunctionTool returned error: %v", err)
	}
	kit, err := tool.NewToolkit(lookup)
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	spec := loop.Spec{
		Name: "ci-sweeper",
		Goal: "look up data and wrap up",
		Mode: loop.ModeAssisted,
		Policy: loop.Policy{
			MaxIterations: 3,
			MaxModelCalls: 1,
			MaxToolCalls:  3,
			MaxAttempts:   1,
			WrapUpHint:    "stop now and summarize",
		},
	}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"You are helpful.",
		model,
		agentpkg.WithToolkit(kit),
		loop.WithSpec(spec),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	user, err := message.NewUserMessage("user", "run")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	var customNames []string
	if err := agent.ReplyStream(context.Background(), user, func(event message.Event) error {
		if custom, ok := event.(*message.CustomEvent); ok {
			customNames = append(customNames, custom.Name)
		}
		return nil
	}); err != nil {
		t.Fatalf("ReplyStream returned error: %v", err)
	}

	if len(model.requests) != 2 {
		t.Fatalf("model requests = %d, want 2", len(model.requests))
	}
	if model.requests[1].ToolChoice == nil || model.requests[1].ToolChoice.Mode != string(types.ToolChoiceNone) {
		t.Fatalf("second request should force tool_choice none, got %#v", model.requests[1].ToolChoice)
	}
	var foundHint bool
	for _, msg := range model.requests[1].Messages {
		for _, block := range msg.GetContentBlocks("hint") {
			hint, ok := block.(*message.HintBlock)
			if ok && strings.Contains(hint.Hint, "stop now and summarize") {
				foundHint = true
				break
			}
		}
		if foundHint {
			break
		}
	}
	if !foundHint {
		t.Fatalf("second request should include wrap-up hint: %#v", model.requests[1].Messages)
	}
	if agent.AgentState().LoopContext.StopReason != "budget_exceeded" {
		t.Fatalf("stop reason = %q, want budget_exceeded", agent.AgentState().LoopContext.StopReason)
	}
	assertCustomEvents(t, customNames, []string{loop.EventWrapUp, loop.EventStop})
}

func assertCustomEvents(t *testing.T, got, expected []string) {
	t.Helper()
	next := 0
	for _, name := range got {
		if next < len(expected) && name == expected[next] {
			next++
		}
	}
	if next != len(expected) {
		t.Fatalf("custom events = %v, want subsequence %v", got, expected)
	}
}
