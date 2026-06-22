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

package runner_test

import (
	"context"
	"strings"
	"testing"

	automationevent "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/event"
	runnerpkg "github.com/yuluo-yx/agentscope-go/pkg/loop/automation/runner"
)

func TestTemplateMapperBuildsUserMessageFromEvent(t *testing.T) {
	t.Parallel()

	mapper, err := runnerpkg.NewTemplateMapper(
		"Handle {{.Event.Type}} from {{.Event.Source}} for {{.Event.Subject}} with {{.Route.LoopName}}.",
		runnerpkg.WithTemplateMapperUserName("loop-runner"),
	)
	if err != nil {
		t.Fatalf("NewTemplateMapper returned error: %v", err)
	}
	msg, err := mapper.MapInput(context.Background(),
		automationevent.Event{ID: "evt-1", Source: "webhook://ci", Type: "ci.workflow.failed", Subject: "repo://current"},
		automationevent.RouteDecision{LoopName: "ci-sweeper", AgentName: "Friday"},
	)
	if err != nil {
		t.Fatalf("MapInput returned error: %v", err)
	}

	text := msg.Content.GetTextContent("")
	if msg.Name != "loop-runner" || text == nil || !strings.Contains(*text, "ci.workflow.failed") ||
		!strings.Contains(*text, "ci-sweeper") {
		t.Fatalf("mapped message mismatch: name=%q text=%v", msg.Name, text)
	}
}
