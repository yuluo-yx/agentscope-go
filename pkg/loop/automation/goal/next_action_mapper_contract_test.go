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

	goalpkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/goal"
	"github.com/yuluo-yx/agentscope-go/pkg/loop/core"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

func TestNextActionMapperFuncAndTemplateBoundaries(t *testing.T) {
	t.Parallel()

	input := core.VerificationInput{ReplyID: "reply-1", Spec: core.Spec{Name: "ci-sweeper"}}
	result := core.VerificationResult{Reason: "missing evidence", NextAction: "add go test output"}

	_, err := (goalpkg.NextActionMapperFunc(nil)).MapNextAction(context.Background(), input, result)
	if err == nil || !strings.Contains(err.Error(), "next action mapper is nil") {
		t.Fatalf("nil NextActionMapperFunc error = %v, want next action mapper is nil", err)
	}

	mapped := false
	msg, err := (goalpkg.NextActionMapperFunc(func(_ context.Context, input core.VerificationInput, result core.VerificationResult) (*message.Message, error) {
		mapped = input.ReplyID == "reply-1" && result.NextAction == "add go test output"
		return message.NewUserMessage("reviewer", "retry with evidence")
	})).MapNextAction(context.Background(), input, result)
	if err != nil || !mapped || msg.Role != message.RoleUser {
		t.Fatalf("NextActionMapperFunc result msg=%#v mapped=%v err=%v", msg, mapped, err)
	}

	_, err = goalpkg.NewTemplateNextActionMapper(" ")
	if err == nil || !strings.Contains(err.Error(), "template is empty") {
		t.Fatalf("empty template error = %v, want template is empty", err)
	}
	_, err = goalpkg.NewTemplateNextActionMapper("{{")
	if err == nil || !strings.Contains(err.Error(), "parse next action template") {
		t.Fatalf("bad template error = %v, want parse error", err)
	}

	mapper, err := goalpkg.NewTemplateNextActionMapper("Reason={{.Result.Reason}} Next={{.Result.NextAction}}")
	if err != nil {
		t.Fatalf("NewTemplateNextActionMapper returned error: %v", err)
	}
	msg, err = mapper.MapNextAction(context.Background(), input, result)
	if err != nil {
		t.Fatalf("MapNextAction returned error: %v", err)
	}
	text := msg.GetTextContent("")
	if text == nil || !strings.Contains(*text, "missing evidence") || msg.Metadata["automation_reply_id"] != "reply-1" ||
		msg.Metadata["automation_loop"] != "ci-sweeper" {
		t.Fatalf("mapped message mismatch: text=%v metadata=%#v", text, msg.Metadata)
	}
	var nilCtx context.Context
	_, err = mapper.MapNextAction(nilCtx, input, result)
	if err == nil || !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("MapNextAction nil context error = %v, want context is nil", err)
	}
}
