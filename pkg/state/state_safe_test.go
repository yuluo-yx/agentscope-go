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

package state

import (
	"fmt"
	"sync"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

func TestSafeAgentState_ConcurrentAppendContext(t *testing.T) {
	state := NewSafeAgentState()

	const numGoroutines = 100
	const msgsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < msgsPerGoroutine; j++ {
				msg, err := message.NewUserMessage("user", fmt.Sprintf("msg-%d-%d", id, j))
				if err != nil {
					t.Errorf("failed to create message: %v", err)
					return
				}
				state.AppendContext(msg)
			}
		}(i)
	}

	wg.Wait()

	// Verify all messages were added
	ctx := state.GetContext()
	expected := numGoroutines * msgsPerGoroutine
	if len(ctx) != expected {
		t.Errorf("expected %d messages, got %d", expected, len(ctx))
	}
}

func TestSafeAgentState_ConcurrentReadWrite(t *testing.T) {
	state := NewSafeAgentState()

	const numReaders = 50
	const numWriters = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(numReaders + numWriters)

	// Start readers
	for i := 0; i < numReaders; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				_ = state.GetContext()
				_ = state.GetReplyID()
				_ = state.GetIteration()
			}
		}(i)
	}

	// Start writers
	for i := 0; i < numWriters; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				msg, err := message.NewUserMessage("user", fmt.Sprintf("msg-%d-%d", id, j))
				if err != nil {
					t.Errorf("failed to create message: %v", err)
					return
				}
				state.AppendContext(msg)
				state.SetReplyID(fmt.Sprintf("reply-%d-%d", id, j))
				state.IncrementIteration()
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is consistent
	finalState := state.GetState()
	if finalState == nil {
		t.Fatal("final state is nil")
	}
}

func TestSafeAgentState_WithLocks(t *testing.T) {
	state := NewSafeAgentState()

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer with write lock
	go func() {
		defer wg.Done()

		state.WithWriteLock(func(s *AgentState) {
			// Simulate complex mutation
			s.ReplyID = "test-reply"
			s.CurIter = 42
			msg, _ := message.NewUserMessage("user", "test")
			s.Context = append(s.Context, msg)
		})
	}()

	// Reader with read lock
	go func() {
		defer wg.Done()

		state.WithReadLock(func(s *AgentState) {
			// Simulate complex read
			_ = s.ReplyID
			_ = s.CurIter
			_ = len(s.Context)
		})
	}()

	wg.Wait()
}

func TestSafeAgentState_DefensiveCopiesAndNilInputs(t *testing.T) {
	state := NewSafeAgentState()

	state.AppendContext(nil)
	state.UpdateState(nil)
	state.SetReplyID("reply-safe")

	msg, err := message.NewUserMessage("user", "original")
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}
	state.AppendContext(msg)

	snapshot := state.GetState()
	if snapshot == nil {
		t.Fatal("snapshot is nil")
	}
	if snapshot.ReplyID != "reply-safe" {
		t.Fatalf("reply id = %q, want reply-safe", snapshot.ReplyID)
	}
	if len(snapshot.Context) != 1 {
		t.Fatalf("context length = %d, want 1", len(snapshot.Context))
	}

	snapshot.Context[0].Name = "mutated"
	got := state.GetContext()
	if len(got) != 1 || got[0].Name != "user" {
		t.Fatalf("state should return defensive message copies: %#v", got)
	}
}

func TestSafeAgentState_ZeroValue(t *testing.T) {
	var state SafeAgentState

	state.WithReadLock(func(snapshot *AgentState) {
		if snapshot == nil {
			t.Fatal("zero-value SafeAgentState should expose an initialized state")
		}
	})

	state.SetReplyID("zero")
	if got := state.GetReplyID(); got != "zero" {
		t.Fatalf("reply id = %q, want zero", got)
	}
}

func BenchmarkSafeAgentState_AppendContext(b *testing.B) {
	state := NewSafeAgentState()
	msg, _ := message.NewUserMessage("user", "benchmark message")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			state.AppendContext(msg)
		}
	})
}

func BenchmarkSafeAgentState_GetContext(b *testing.B) {
	state := NewSafeAgentState()

	// Pre-populate with some messages
	for i := 0; i < 100; i++ {
		msg, _ := message.NewUserMessage("user", fmt.Sprintf("msg-%d", i))
		state.AppendContext(msg)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = state.GetContext()
		}
	})
}
