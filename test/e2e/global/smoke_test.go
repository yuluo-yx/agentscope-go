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

package global_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/tool"
	tasktool "github.com/yuluo-yx/agentscope-go/tool/task"
)

type scriptedChatModel struct {
	responses []*modelpkg.ChatResponse
	requests  []modelpkg.CallRequest
}

func (m *scriptedChatModel) Name() string {
	return "scripted-global-e2e"
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

func (m *scriptedChatModel) Stream(context.Context, modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	return nil, fmt.Errorf("scripted stream is not used")
}

func (m *scriptedChatModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

func TestGlobalAgentToolStateE2E(t *testing.T) {
	t.Parallel()

	kit, err := tool.NewToolkit(tasktool.NewTaskCreate())
	if err != nil {
		t.Fatalf("NewToolkit returned error: %v", err)
	}
	model := &scriptedChatModel{responses: []*modelpkg.ChatResponse{
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewToolCallBlock("task-call", "TaskCreate", `{"subject":"Track phase five","description":"Create a task from the global E2E smoke test.","metadata":{"phase":"five"}}`)},
			true,
		),
		modelpkg.NewChatResponse(
			message.ContentBlockList{message.NewTextBlock("task is tracked")},
			true,
		),
	}}
	agent, err := agentpkg.NewAgent("Friday", "Track structured work.", model, agentpkg.WithToolkit(kit))
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}
	userMsg, err := message.NewUserMessage("Tony", "Track phase five work")
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}

	reply, err := agent.Reply(context.Background(), userMsg)
	if err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	if text := reply.GetTextContent(""); text == nil || *text != "task is tracked" {
		t.Fatalf("final reply text mismatch: %#v", reply)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model should be called before and after tool execution, got %d calls", len(model.requests))
	}
	if !requestIncludesTool(model.requests[0], "TaskCreate") {
		t.Fatalf("initial model request should expose TaskCreate schema: %#v", model.requests[0].Tools)
	}
	tasks := agent.AgentState().TaskContext.Tasks
	if len(tasks) != 1 || tasks[0].Subject != "Track phase five" || tasks[0].Metadata["phase"] != "five" {
		t.Fatalf("TaskCreate should persist task state, got %#v", tasks)
	}
	lastModelMessage := model.requests[1].Messages[len(model.requests[1].Messages)-1]
	results := lastModelMessage.GetContentBlocks("tool_result")
	if len(results) != 1 {
		t.Fatalf("second model request should include tool result context, got %#v", lastModelMessage.Content)
	}
	result := results[0].(*message.ToolResultBlock)
	if result.State != message.ToolResultSuccess || !strings.Contains(result.Output.Blocks[0].(*message.TextBlock).Text, "created successfully") {
		t.Fatalf("tool result should be successful, got %#v", result)
	}
}

func requestIncludesTool(request modelpkg.CallRequest, name string) bool {
	for _, schema := range request.Tools {
		if schema.Function.Name == name {
			return true
		}
	}
	return false
}
