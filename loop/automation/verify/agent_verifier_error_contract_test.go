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
	"github.com/yuluo-yx/agentscope-go/state"
)

func TestVerificationAdaptersRejectNilFunctions(t *testing.T) {
	t.Parallel()

	if _, err := (verifypkg.VerificationMapperFunc)(nil).MapVerification(context.Background(), core.VerificationInput{}); err == nil {
		t.Fatalf("nil VerificationMapperFunc should fail")
	}
	if _, err := (verifypkg.VerificationParserFunc)(nil).ParseVerification(context.Background(), core.VerificationInput{}, nil); err == nil {
		t.Fatalf("nil VerificationParserFunc should fail")
	}
}

func TestDefaultVerificationMapperRejectsInvalidContextAndIncludesLoopStopReason(t *testing.T) {
	t.Parallel()

	mapper := verifypkg.DefaultVerificationMapper{}
	var nilCtx context.Context
	if _, err := mapper.MapVerification(nilCtx, core.VerificationInput{}); err == nil {
		t.Fatalf("MapVerification should reject a nil context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mapper.MapVerification(ctx, core.VerificationInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("MapVerification canceled error = %v, want %v", err, context.Canceled)
	}

	request, err := mapper.MapVerification(context.Background(), core.VerificationInput{
		Spec: core.Spec{Name: "release-loop", Goal: "verify release"},
		State: &state.AgentState{LoopContext: &state.LoopContext{
			StopReason: state.LoopStopWaitingUser,
		}},
	})
	if err != nil {
		t.Fatalf("MapVerification returned error: %v", err)
	}
	text := request.GetTextContent("")
	if text == nil || !strings.Contains(*text, "Loop stop reason: waiting_user") {
		t.Fatalf("verification prompt should include loop stop reason: %v", text)
	}
	if request.Name != "verifier" {
		t.Fatalf("default verification message name = %q, want verifier", request.Name)
	}
}

func TestJSONVerificationParserRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	parser := verifypkg.JSONVerificationParser{}
	var nilCtx context.Context
	if _, err := parser.ParseVerification(nilCtx, core.VerificationInput{}, nil); err == nil {
		t.Fatalf("ParseVerification should reject a nil context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reply := newAssistantReply(t, `{"passed":true}`)
	if _, err := parser.ParseVerification(ctx, core.VerificationInput{}, reply); !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseVerification canceled error = %v, want %v", err, context.Canceled)
	}
	if _, err := parser.ParseVerification(context.Background(), core.VerificationInput{}, nil); err == nil {
		t.Fatalf("ParseVerification should reject a nil reply")
	}
	if _, err := parser.ParseVerification(context.Background(), core.VerificationInput{}, newAssistantReply(t, "   ")); err == nil {
		t.Fatalf("ParseVerification should reject an empty reply")
	}
}

func TestAgentVerifierRejectsInvalidInputsAndPropagatesCollaboratorErrors(t *testing.T) {
	t.Parallel()

	var nilCtx context.Context
	if _, err := (verifypkg.AgentVerifier{}).Verify(nilCtx, core.VerificationInput{}); err == nil {
		t.Fatalf("Verify should reject a nil context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (verifypkg.AgentVerifier{}).Verify(ctx, core.VerificationInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify canceled error = %v, want %v", err, context.Canceled)
	}
	if _, err := (verifypkg.AgentVerifier{}).Verify(context.Background(), core.VerificationInput{}); err == nil {
		t.Fatalf("Verify should reject a nil checker agent")
	}

	checker, err := agent.NewAgent("checker", "verify", &verificationChatModel{})
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	mapperErr := errors.New("mapper failed")
	verifier := verifypkg.AgentVerifier{
		Agent: checker,
		Mapper: verifypkg.VerificationMapperFunc(func(context.Context, core.VerificationInput) (*message.Message, error) {
			return nil, mapperErr
		}),
	}
	if _, err := verifier.Verify(context.Background(), core.VerificationInput{}); !errors.Is(err, mapperErr) {
		t.Fatalf("Verify mapper error = %v, want %v", err, mapperErr)
	}

	verifier.Mapper = verifypkg.VerificationMapperFunc(func(context.Context, core.VerificationInput) (*message.Message, error) {
		return nil, nil
	})
	if _, err := verifier.Verify(context.Background(), core.VerificationInput{}); err == nil {
		t.Fatalf("Verify should reject a nil mapped request")
	}

	request, err := message.NewUserMessage("verifier", "check")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	verifier.Mapper = verifypkg.VerificationMapperFunc(func(context.Context, core.VerificationInput) (*message.Message, error) {
		return request, nil
	})
	if _, err := verifier.Verify(context.Background(), core.VerificationInput{}); err == nil || !strings.Contains(err.Error(), "no response") {
		t.Fatalf("Verify should propagate checker agent errors, got %v", err)
	}

	parserErr := errors.New("parser failed")
	checkerModel := &verificationChatModel{responses: []*model.ChatResponse{
		model.NewChatResponse(message.ContentBlockList{message.NewTextBlock("ok")}, true),
	}}
	checkerWithReply, err := agent.NewAgent("checker", "verify", checkerModel)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	parserVerifier := verifypkg.AgentVerifier{
		Agent:  checkerWithReply,
		Mapper: verifier.Mapper,
		Parser: verifypkg.VerificationParserFunc(func(context.Context, core.VerificationInput, *message.Message) (core.VerificationResult, error) {
			return core.VerificationResult{}, parserErr
		}),
	}
	if _, err := parserVerifier.Verify(context.Background(), core.VerificationInput{}); !errors.Is(err, parserErr) {
		t.Fatalf("Verify parser error = %v, want %v", err, parserErr)
	}
}

func newAssistantReply(t *testing.T, text string) *message.Message {
	t.Helper()

	reply, err := message.NewAssistantMessage("checker", text)
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	return reply
}
