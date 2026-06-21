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

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/credential"
	automationgoal "github.com/yuluo-yx/agentscope-go/loop/automation/goal"
	automationstore "github.com/yuluo-yx/agentscope-go/loop/automation/store"
	"github.com/yuluo-yx/agentscope-go/loop/core"
	loopruntime "github.com/yuluo-yx/agentscope-go/loop/runtime"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "goal runner example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	spec := core.Spec{
		Name: "goal-ci-summary",
		Goal: "produce a verified CI failure summary with evidence",
		SuccessCriteria: []core.SuccessCriterion{
			{Name: "evidence", Description: "final summary includes verification evidence", Required: true},
		},
		Mode:   core.ModeReportOnly,
		Policy: core.DefaultPolicy(core.ModeReportOnly),
	}
	model, err := newDashScopeChatModel(false)
	if err != nil {
		return fmt.Errorf("create DashScope chat model: %w", err)
	}
	agent, err := agentpkg.NewAgent("Friday", "You summarize CI failures.", model, loopruntime.WithSpec(spec))
	if err != nil {
		return err
	}

	verifierAttempts := 0
	verifier := core.VerifierFunc(func(_ context.Context, input core.VerificationInput) (core.VerificationResult, error) {
		verifierAttempts++
		if input.State == nil || input.State.LoopContext == nil {
			return core.VerificationResult{
				Passed:     false,
				Reason:     "missing loop state",
				NextAction: "collect loop state",
			}, nil
		}
		if verifierAttempts == 1 {
			return core.VerificationResult{
				Passed:     false,
				Reason:     "missing evidence",
				Evidence:   []string{"first attempt did not include command output"},
				NextAction: "add concrete verification evidence",
			}, nil
		}
		return core.VerificationResult{
			Passed:   true,
			Reason:   "evidence accepted",
			Evidence: []string{"second attempt includes go test evidence"},
		}, nil
	})
	mapper, err := automationgoal.NewTemplateNextActionMapper(
		"Verifier rejected the previous attempt: {{.Result.Reason}}. Next action: {{.Result.NextAction}}.",
	)
	if err != nil {
		return err
	}
	store := automationstore.NewMemoryRunStore()
	runner := automationgoal.GoalRunner{
		Agent:    agent,
		Spec:     spec,
		Verifier: verifier,
		Store:    store,
		Policy:   automationgoal.ContinuePolicy{MaxAttempts: 2},
		Mapper:   mapper,
	}
	initial, err := message.NewUserMessage("user", "Summarize the failing CI run with evidence.")
	if err != nil {
		return err
	}

	result, err := runner.Run(ctx, initial)
	if err != nil {
		return err
	}
	fmt.Printf("completed=%t attempts=%d stop=%s verifier_attempts=%d\n",
		result.Completed,
		result.Attempts,
		result.StopReason,
		verifierAttempts,
	)
	fmt.Printf("runs=%d reports=%d run_stops=%s\n",
		len(store.Runs()),
		len(store.Reports()),
		runStops(store.Runs()),
	)
	return nil
}

func runStops(runs []automationstore.RunRecord) string {
	values := make([]string, 0, len(runs))
	for _, run := range runs {
		values = append(values, run.StopReason)
	}
	return strings.Join(values, ",")
}

func newDashScopeChatModel(stream bool) (*dashscope.ChatModel, error) {
	apiKey := os.Getenv("AI_DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}
	return dashscope.NewChatModel(
		credential.NewDashScope(apiKey).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(stream),
	)
}
