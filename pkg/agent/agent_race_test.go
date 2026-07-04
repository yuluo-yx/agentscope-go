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

//go:build race

package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/model"
)

// mockChatModel is a simple mock for testing concurrent agent calls
type mockChatModel struct {
	name string
}

func (m *mockChatModel) Name() string {
	return m.name
}

func (m *mockChatModel) Call(ctx context.Context, req model.CallRequest) (*model.ChatResponse, error) {
	// Simulate simple response
	content := message.ContentBlockList{message.NewTextBlock("Mock response")}
	response := &model.ChatResponse{
		Content: content,
		IsLast:  true,
		Usage:   &model.ChatUsage{InputTokens: 5, OutputTokens: 5},
	}
	return response, nil
}

func (m *mockChatModel) Stream(ctx context.Context, req model.CallRequest) (<-chan model.ChatResponse, error) {
	ch := make(chan model.ChatResponse, 1)
	go func() {
		defer close(ch)
		content := message.ContentBlockList{message.NewTextBlock("Mock")}
		ch <- model.ChatResponse{
			Content: content,
			IsLast:  true,
			Usage:   &model.ChatUsage{InputTokens: 5, OutputTokens: 5},
		}
	}()
	return ch, nil
}

func (m *mockChatModel) CountTokens(req model.CallRequest) (int, error) {
	// Simple estimation: 1 token per message
	return len(req.Messages), nil
}

func TestAgent_ConcurrentReplies(t *testing.T) {
	model := &mockChatModel{name: "test-model"}
	agent, err := NewAgent("test-agent", "You are a helpful assistant", model)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			msg, err := message.NewUserMessage("user", fmt.Sprintf("Query %d", id))
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: failed to create message: %w", id, err)
				return
			}

			_, err = agent.Reply(context.Background(), msg)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: reply failed: %w", id, err)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Error(err)
	}
}

func TestAgent_ConcurrentReplyStream(t *testing.T) {
	model := &mockChatModel{name: "test-model"}
	agent, err := NewAgent("test-agent", "You are a helpful assistant", model)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	const numGoroutines = 30
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			msg, err := message.NewUserMessage("user", fmt.Sprintf("Stream query %d", id))
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: failed to create message: %w", id, err)
				return
			}

			err = agent.ReplyStream(context.Background(), msg, func(event message.Event) error {
				// Consume events
				return nil
			})
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: stream failed: %w", id, err)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Error(err)
	}
}

func TestAgent_ConcurrentObserve(t *testing.T) {
	model := &mockChatModel{name: "test-model"}
	agent, err := NewAgent("test-agent", "You are a helpful assistant", model)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			msg, err := message.NewUserMessage("user", fmt.Sprintf("Observe %d", id))
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: failed to create message: %w", id, err)
				return
			}

			err = agent.Observe(context.Background(), msg)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: observe failed: %w", id, err)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Error(err)
	}

	// Verify all messages were added
	state := agent.AgentState()
	if state == nil {
		t.Fatal("agent state is nil")
	}

	if len(state.Context) != numGoroutines {
		t.Errorf("expected %d messages in context, got %d", numGoroutines, len(state.Context))
	}
}

func TestAgent_MixedConcurrentOperations(t *testing.T) {
	model := &mockChatModel{name: "test-model"}
	agent, err := NewAgent("test-agent", "You are a helpful assistant", model)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	const numOperations = 100
	var wg sync.WaitGroup
	wg.Add(numOperations)

	errors := make(chan error, numOperations)

	// Mix of different operations
	for i := 0; i < numOperations; i++ {
		op := i % 3
		go func(id int, opType int) {
			defer wg.Done()

			msg, err := message.NewUserMessage("user", fmt.Sprintf("Mixed op %d", id))
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: failed to create message: %w", id, err)
				return
			}

			switch opType {
			case 0: // Reply
				_, err = agent.Reply(context.Background(), msg)
			case 1: // ReplyStream
				err = agent.ReplyStream(context.Background(), msg, nil)
			case 2: // Observe
				err = agent.Observe(context.Background(), msg)
			}

			if err != nil {
				errors <- fmt.Errorf("goroutine %d (op %d): operation failed: %w", id, opType, err)
				return
			}
		}(i, op)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Error(err)
	}
}
