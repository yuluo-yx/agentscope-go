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

package verify_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/agent"
	verifypkg "github.com/yuluo-yx/agentscope-go/loop/automation/verify"
	"github.com/yuluo-yx/agentscope-go/loop/core"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/model"
)

func TestAgentVerifierRunsCheckerAgentAndParsesJSONResult(t *testing.T) {
	t.Parallel()

	checkerModel := &verificationChatModel{
		responses: []*model.ChatResponse{
			model.NewChatResponse(message.ContentBlockList{message.NewTextBlock(`{
				"passed": true,
				"reason": "all checks passed",
				"evidence": ["go test ./..."],
				"next_action": "done"
			}`)}, true),
		},
	}
	checker, err := agent.NewAgent("checker", "verify loop output", checkerModel)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	verifier := verifypkg.AgentVerifier{Agent: checker}

	result, err := verifier.Verify(context.Background(), core.VerificationInput{
		AgentName: "maker",
		SessionID: "session-1",
		ReplyID:   "reply-1",
		Spec: core.Spec{
			Name: "daily-check",
			Goal: "confirm the change is complete",
			SuccessCriteria: []core.SuccessCriterion{
				{Name: "tests", Description: "all tests pass", Required: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !result.Passed || result.Reason != "all checks passed" || result.NextAction != "done" {
		t.Fatalf("verification result mismatch: %#v", result)
	}
	if len(result.Evidence) != 1 || result.Evidence[0] != "go test ./..." {
		t.Fatalf("verification evidence mismatch: %#v", result.Evidence)
	}
	if len(checkerModel.requests) != 1 {
		t.Fatalf("checker model requests = %d, want 1", len(checkerModel.requests))
	}
	requestText := requestText(checkerModel.requests[0])
	if !strings.Contains(requestText, "daily-check") ||
		!strings.Contains(requestText, "confirm the change is complete") ||
		!strings.Contains(requestText, "all tests pass") {
		t.Fatalf("default verifier prompt did not include loop context: %q", requestText)
	}
}

func TestAgentVerifierUsesCustomMapperAndParser(t *testing.T) {
	t.Parallel()

	checkerModel := &verificationChatModel{
		responses: []*model.ChatResponse{
			model.NewChatResponse(message.ContentBlockList{message.NewTextBlock("custom reply")}, true),
		},
	}
	checker, err := agent.NewAgent("checker", "verify loop output", checkerModel)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	mapped := false
	parsed := false
	verifier := verifypkg.AgentVerifier{
		Agent: checker,
		Mapper: verifypkg.VerificationMapperFunc(func(_ context.Context, input core.VerificationInput) (*message.Message, error) {
			mapped = input.Spec.Name == "custom-loop"
			return message.NewUserMessage("verifier", "custom prompt")
		}),
		Parser: verifypkg.VerificationParserFunc(func(_ context.Context, input core.VerificationInput, reply *message.Message) (core.VerificationResult, error) {
			text := reply.GetTextContent("")
			parsed = input.Spec.Name == "custom-loop" && text != nil && *text == "custom reply"
			return core.VerificationResult{Passed: false, Reason: "needs changes", NextAction: "retry"}, nil
		}),
	}

	result, err := verifier.Verify(context.Background(), core.VerificationInput{Spec: core.Spec{Name: "custom-loop"}})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if !mapped || !parsed {
		t.Fatalf("custom mapper/parser were not used: mapped=%v parsed=%v", mapped, parsed)
	}
	if result.Passed || result.Reason != "needs changes" || result.NextAction != "retry" {
		t.Fatalf("custom result mismatch: %#v", result)
	}
}

func TestJSONVerificationParserHandlesFencedJSONAndRejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	parser := verifypkg.JSONVerificationParser{}
	reply, err := message.NewAssistantMessage("checker", "```json\n{\"passed\":false,\"reason\":\"missing evidence\",\"evidence\":[\"unit test\"],\"next_action\":\"collect evidence\"}\n```")
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}

	result, err := parser.ParseVerification(context.Background(), core.VerificationInput{}, reply)
	if err != nil {
		t.Fatalf("ParseVerification returned error: %v", err)
	}
	if result.Passed || result.Reason != "missing evidence" || result.NextAction != "collect evidence" {
		t.Fatalf("parsed result mismatch: %#v", result)
	}
	if len(result.Evidence) != 1 || result.Evidence[0] != "unit test" {
		t.Fatalf("parsed evidence mismatch: %#v", result.Evidence)
	}

	invalidReply, err := message.NewAssistantMessage("checker", "not json")
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	_, err = parser.ParseVerification(context.Background(), core.VerificationInput{}, invalidReply)
	if err == nil {
		t.Fatalf("ParseVerification should reject invalid json")
	}
}

type verificationChatModel struct {
	responses []*model.ChatResponse
	requests  []model.CallRequest
}

func (m *verificationChatModel) Name() string {
	return "verification"
}

func (m *verificationChatModel) Call(_ context.Context, request model.CallRequest) (*model.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, errors.New("verification model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response.Clone(), nil
}

func (m *verificationChatModel) Stream(ctx context.Context, request model.CallRequest) (<-chan model.ChatResponse, error) {
	m.requests = append(m.requests, request.Clone())
	if len(m.responses) == 0 {
		return nil, errors.New("verification model has no response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	ch := make(chan model.ChatResponse, 1)
	select {
	case ch <- *response.Clone():
	case <-ctx.Done():
	}
	close(ch)
	return ch, nil
}

func (m *verificationChatModel) CountTokens(request model.CallRequest) (int, error) {
	return model.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func requestText(request model.CallRequest) string {
	var parts []string
	for _, msg := range request.Messages {
		text := msg.GetTextContent("")
		if text != nil {
			parts = append(parts, *text)
		}
	}
	return strings.Join(parts, "\n")
}
