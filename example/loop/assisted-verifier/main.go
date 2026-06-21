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

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/loop/core"
	loopruntime "github.com/yuluo-yx/agentscope-go/loop/runtime"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

type scriptedChatModel struct {
	response *asmodel.ChatResponse
}

func (m *scriptedChatModel) Name() string { return "scripted-assisted-loop" }

func (m *scriptedChatModel) Call(context.Context, asmodel.CallRequest) (*asmodel.ChatResponse, error) {
	return m.response.Clone(), nil
}

func (m *scriptedChatModel) Stream(ctx context.Context, _ asmodel.CallRequest) (<-chan asmodel.ChatResponse, error) {
	out := make(chan asmodel.ChatResponse, 1)
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
		case out <- *m.response.Clone():
		}
	}()
	return out, nil
}

func (m *scriptedChatModel) CountTokens(request asmodel.CallRequest) (int, error) {
	return asmodel.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "assisted verifier example: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	spec := core.Spec{
		Name: "ci-sweeper",
		Goal: "summarize a CI failure and ask a verifier to check whether the loop can stop",
		Mode: core.ModeAssisted,
		Policy: core.Policy{
			MaxIterations: 3,
			MaxModelCalls: 4,
			MaxToolCalls:  4,
			MaxAttempts:   2,
		},
		HumanGates: []core.HumanGate{{Name: "security", Description: "security-sensitive changes need review"}},
	}
	verifier := core.VerifierFunc(func(_ context.Context, input core.VerificationInput) (core.VerificationResult, error) {
		if input.State == nil || input.State.LoopContext == nil {
			return core.VerificationResult{Passed: false, Reason: "missing loop state", NextAction: "escalate"}, nil
		}
		return core.VerificationResult{Passed: true, Reason: "scripted verifier accepted the run", Evidence: []string{"local scripted response"}}, nil
	})
	model := &scriptedChatModel{
		response: asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("ci failure summarized")}, true),
	}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"You summarize CI failures.",
		model,
		loopruntime.WithSpec(spec, loopruntime.WithVerifier(verifier)),
	)
	if err != nil {
		return err
	}
	user, err := message.NewUserMessage("user", "Summarize the CI failure.")
	if err != nil {
		return err
	}

	if err := agent.ReplyStream(ctx, user, nil); err != nil {
		return err
	}
	verification := agent.AgentState().LoopContext.LastVerification
	fmt.Printf("verified=%v reason=%s\n", verification.Passed, verification.Reason)
	return nil
}
