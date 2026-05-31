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

package package_structure_test

import (
	"context"
	"testing"

	"github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

type packageStructureModel struct{}

func (packageStructureModel) Name() string {
	return "package-structure"
}

func (packageStructureModel) Call(context.Context, model.CallRequest) (*model.ChatResponse, error) {
	return model.NewChatResponse(message.ContentBlockList{message.NewTextBlock("ok")}, true), nil
}

func (packageStructureModel) Stream(context.Context, model.CallRequest) (<-chan model.ChatResponse, error) {
	return nil, nil
}

func (packageStructureModel) CountTokens(model.CallRequest) (int, error) {
	return 0, nil
}

func TestDomainPackagesAndRootFacadeShareCoreTypes(t *testing.T) {
	t.Parallel()

	domainAgent, err := agent.NewAgent("Friday", "Use the domain packages.", packageStructureModel{})
	if err != nil {
		t.Fatalf("agent.NewAgent returned error: %v", err)
	}
	rootAgent := domainAgent
	if rootAgent.AgentName() != "Friday" {
		t.Fatalf("facade Agent alias mismatch: %q", rootAgent.AgentName())
	}

	domainState := state.NewAgentState()
	rootState := domainState
	if rootState.SessionID == "" {
		t.Fatal("facade AgentState alias should preserve initialized state")
	}

	domainChunk := tool.NewToolChunk("call-1", message.ContentBlockList{message.NewTextBlock("ok")})
	rootChunk := domainChunk
	if rootChunk.ID != "call-1" {
		t.Fatalf("facade ToolChunk alias mismatch: %q", rootChunk.ID)
	}
}
