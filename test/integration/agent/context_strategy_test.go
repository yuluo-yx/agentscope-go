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

package agentintegration_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
	modelpkg "github.com/yuluo-yx/agentscope-go/model"
)

func TestCompressContextSummarizesAndOffloadsOldContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	state := agentpkg.NewAgentState()
	oldUser := mustUserMessage(t, "Tony", strings.Repeat("old user context ", 20))
	oldAssistant := mustAssistantMessage(t, "Friday", strings.Repeat("old assistant context ", 20))
	recentUser := mustUserMessage(t, "Tony", "keep the recent instruction")
	state.Context = []*message.Message{oldUser, oldAssistant, recentUser}
	offloader := &recordingOffloader{contextPath: "external://sessions/context.jsonl"}
	model := &summaryScriptedModel{
		callResponses: []*modelpkg.ChatResponse{
			modelpkg.NewChatResponse(message.ContentBlockList{message.NewTextBlock(`{
				"task_overview": "ship context compression",
				"current_state": "old context summarized",
				"important_discoveries": "workspace offload is available",
				"next_steps": "continue from the recent instruction",
				"context_to_preserve": "keep middleware semantics"
			}`)}, true),
		},
	}
	config := agentpkg.DefaultContextConfig()
	config.MaxTokens = 80
	config.TriggerRatio = 0.5
	config.ReserveRatio = 0.2
	config.ToolResultLimit = 20
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Use context carefully.",
		model,
		agentpkg.WithAgentState(state),
		agentpkg.WithOffloader(offloader),
		agentpkg.WithContextConfig(config),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}

	if err := agent.CompressContext(ctx); err != nil {
		t.Fatalf("CompressContext returned error: %v", err)
	}

	if len(model.callRequests) != 1 {
		t.Fatalf("summary strategy should call the model once, got %d calls", len(model.callRequests))
	}
	requestText := joinedRequestText(model.callRequests[0])
	if !strings.Contains(requestText, "old user context") || !strings.Contains(requestText, "generate_structured_output") {
		t.Fatalf("summary request should include old context and schema instruction: %s", requestText)
	}
	if got := len(state.Context); got != 1 {
		t.Fatalf("only the recent context should remain, got %d messages", got)
	}
	if text := state.Context[0].GetTextContent(""); text == nil || *text != "keep the recent instruction" {
		t.Fatalf("recent message was not preserved: %#v", state.Context[0])
	}
	if !strings.Contains(state.Summary.Text, "ship context compression") ||
		!strings.Contains(state.Summary.Text, "external://sessions/context.jsonl") {
		t.Fatalf("summary should include structured content and offload reminder: %s", state.Summary.Text)
	}
	if got := len(offloader.contextMessages); got != 2 {
		t.Fatalf("offloader should receive the compressed messages, got %d", got)
	}
	if text := offloader.contextMessages[0].GetTextContent(""); text == nil || !strings.Contains(*text, "old user context") {
		t.Fatalf("offloaded context should include old messages: %#v", offloader.contextMessages)
	}
}

func TestCompressContextAcceptsCustomStrategy(t *testing.T) {
	t.Parallel()

	state := agentpkg.NewAgentState()
	state.Context = []*message.Message{mustUserMessage(t, "Tony", "custom context")}
	model := &summaryScriptedModel{}
	agent, err := agentpkg.NewAgent(
		"Friday",
		"Use custom context strategy.",
		model,
		agentpkg.WithAgentState(state),
		agentpkg.WithContextStrategies(customContextStrategy{}),
	)
	if err != nil {
		t.Fatalf("NewAgent returned error: %v", err)
	}

	if err := agent.CompressContext(context.Background()); err != nil {
		t.Fatalf("CompressContext returned error: %v", err)
	}

	if state.Summary.Text != "custom strategy ran" {
		t.Fatalf("custom context strategy did not run: %q", state.Summary.Text)
	}
	if len(model.callRequests) != 0 {
		t.Fatalf("custom strategy should replace default summary strategy, got %d model calls", len(model.callRequests))
	}
}

type customContextStrategy struct{}

func (customContextStrategy) ContextStrategyName() string {
	return "custom-context"
}

func (customContextStrategy) ApplyContextStrategy(_ context.Context, input *agentpkg.ContextStrategyInput) error {
	input.State.Summary.Text = "custom strategy ran"
	return nil
}

type summaryScriptedModel struct {
	callResponses   []*modelpkg.ChatResponse
	streamResponses []*modelpkg.ChatResponse
	callRequests    []modelpkg.CallRequest
	streamRequests  []modelpkg.CallRequest
}

func (m *summaryScriptedModel) Name() string {
	return "summary-scripted"
}

func (m *summaryScriptedModel) Call(_ context.Context, request modelpkg.CallRequest) (*modelpkg.ChatResponse, error) {
	m.callRequests = append(m.callRequests, request.Clone())
	if len(m.callResponses) == 0 {
		return nil, fmt.Errorf("summary scripted model has no call response")
	}
	response := m.callResponses[0]
	m.callResponses = m.callResponses[1:]
	return response.Clone(), nil
}

func (m *summaryScriptedModel) Stream(ctx context.Context, request modelpkg.CallRequest) (<-chan modelpkg.ChatResponse, error) {
	m.streamRequests = append(m.streamRequests, request.Clone())
	if len(m.streamResponses) == 0 {
		return nil, fmt.Errorf("summary scripted model has no stream response")
	}
	response := m.streamResponses[0]
	m.streamResponses = m.streamResponses[1:]
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

func (m *summaryScriptedModel) CountTokens(request modelpkg.CallRequest) (int, error) {
	return modelpkg.ApproximateTokenCount(request.Messages, request.Tools), nil
}

type recordingOffloader struct {
	contextPath     string
	contextMessages []*message.Message
	toolResults     []*message.ToolResultBlock
}

func (o *recordingOffloader) OffloadContext(_ context.Context, _ string, messages []*message.Message) (string, error) {
	for _, msg := range messages {
		if msg != nil {
			o.contextMessages = append(o.contextMessages, msg.Clone())
		}
	}
	return o.contextPath, nil
}

func (o *recordingOffloader) OffloadToolResult(_ context.Context, _ string, result *message.ToolResultBlock) (string, error) {
	o.toolResults = append(o.toolResults, result.Clone().(*message.ToolResultBlock))
	return "external://tool-results/" + result.ID + ".txt", nil
}

func (o *recordingOffloader) OffloadDataBlock(_ context.Context, block *message.DataBlock) (*message.DataBlock, error) {
	return block.Clone().(*message.DataBlock), nil
}

func mustUserMessage(t *testing.T, name, content string) *message.Message {
	t.Helper()
	msg, err := message.NewUserMessage(name, content)
	if err != nil {
		t.Fatalf("NewUserMessage returned error: %v", err)
	}
	return msg
}

func mustAssistantMessage(t *testing.T, name, content string) *message.Message {
	t.Helper()
	msg, err := message.NewAssistantMessage(name, content)
	if err != nil {
		t.Fatalf("NewAssistantMessage returned error: %v", err)
	}
	return msg
}

func joinedRequestText(request modelpkg.CallRequest) string {
	parts := make([]string, 0, len(request.Messages))
	for _, msg := range request.Messages {
		if msg == nil {
			continue
		}
		if text := msg.GetTextContent("\n"); text != nil {
			parts = append(parts, *text)
		}
	}
	return strings.Join(parts, "\n")
}
