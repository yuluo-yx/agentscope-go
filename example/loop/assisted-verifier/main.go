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

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/loop"
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
	spec := loop.Spec{
		Name: "ci-sweeper",
		Goal: "summarize a CI failure and ask a verifier to check whether the loop can stop",
		Mode: loop.ModeAssisted,
		Policy: loop.Policy{
			MaxIterations: 3,
			MaxModelCalls: 4,
			MaxToolCalls:  4,
			MaxAttempts:   2,
		},
		HumanGates: []loop.HumanGate{{Name: "security", Description: "security-sensitive changes need review"}},
	}
	verifier := loop.VerifierFunc(func(_ context.Context, input loop.VerificationInput) (loop.VerificationResult, error) {
		if input.State == nil || input.State.LoopContext == nil {
			return loop.VerificationResult{Passed: false, Reason: "missing loop state", NextAction: "escalate"}, nil
		}
		return loop.VerificationResult{Passed: true, Reason: "scripted verifier accepted the run", Evidence: []string{"local scripted response"}}, nil
	})
	model := &scriptedChatModel{
		response: asmodel.NewChatResponse(message.ContentBlockList{message.NewTextBlock("ci failure summarized")}, true),
	}
	agent := mustAgent(agentpkg.NewAgent(
		"Friday",
		"You summarize CI failures.",
		model,
		loop.WithSpec(spec, loop.WithVerifier(verifier)),
	))
	user := mustMessage(message.NewUserMessage("user", "Summarize the CI failure."))

	if err := agent.ReplyStream(context.Background(), user, nil); err != nil {
		panic(err)
	}
	verification := agent.AgentState().LoopContext.LastVerification
	fmt.Printf("verified=%v reason=%s\n", verification.Passed, verification.Reason)
}

func mustAgent(agent *agentpkg.Agent, err error) *agentpkg.Agent {
	if err != nil {
		panic(err)
	}
	return agent
}

func mustMessage(msg *message.Message, err error) *message.Message {
	if err != nil {
		panic(err)
	}
	return msg
}
