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
	"sync"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

// SafeAgentState is a concurrency-safe wrapper for AgentState.
// All reads and writes through this wrapper are protected by a lock.
type SafeAgentState struct {
	mu    sync.RWMutex
	state *AgentState
}

// NewSafeAgentState creates a concurrency-safe AgentState wrapper.
func NewSafeAgentState() *SafeAgentState {
	return &SafeAgentState{
		state: NewAgentState(),
	}
}

// WrapAgentState creates a concurrency-safe wrapper from an existing state.
func WrapAgentState(state *AgentState) *SafeAgentState {
	if state == nil {
		return NewSafeAgentState()
	}
	return &SafeAgentState{
		state: state.Clone(),
	}
}

// GetState returns a deep copy of the underlying state.
func (s *SafeAgentState) GetState() *AgentState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state == nil {
		return NewAgentState()
	}
	return s.state.Clone()
}

// UpdateState atomically replaces the current state; nil input resets it to a new state.
func (s *SafeAgentState) UpdateState(newState *AgentState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if newState == nil {
		s.state = NewAgentState()
		return
	}
	s.state = newState.Clone()
}

// AppendContext safely appends a context message; nil messages are ignored.
func (s *SafeAgentState) AppendContext(msg *message.Message) {
	if msg == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureStateLocked()
	s.state.Context = append(s.state.Context, msg.Clone())
}

// GetContext returns a deep copy of the context messages.
func (s *SafeAgentState) GetContext() []*message.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.state == nil {
		return nil
	}
	result := make([]*message.Message, len(s.state.Context))
	for i, msg := range s.state.Context {
		if msg != nil {
			result[i] = msg.Clone()
		}
	}
	return result
}

// SetReplyID atomically sets the current reply ID.
func (s *SafeAgentState) SetReplyID(replyID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureStateLocked()
	s.state.ReplyID = replyID
}

// GetReplyID returns the current reply ID.
func (s *SafeAgentState) GetReplyID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state == nil {
		return ""
	}
	return s.state.ReplyID
}

// IncrementIteration atomically increments the current iteration count.
func (s *SafeAgentState) IncrementIteration() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureStateLocked()
	s.state.CurIter++
	return s.state.CurIter
}

// GetIteration returns the current iteration count.
func (s *SafeAgentState) GetIteration() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state == nil {
		return 0
	}
	return s.state.CurIter
}

// WithReadLock runs fn while holding a read lock for read-only access that needs cross-field consistency.
func (s *SafeAgentState) WithReadLock(fn func(*AgentState)) {
	if fn == nil {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state == nil {
		fn(NewAgentState())
		return
	}
	fn(s.state)
}

// WithWriteLock runs fn while holding a write lock for state mutations that need cross-field consistency.
func (s *SafeAgentState) WithWriteLock(fn func(*AgentState)) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureStateLocked()
	fn(s.state)
}

func (s *SafeAgentState) ensureStateLocked() {
	if s.state == nil {
		s.state = NewAgentState()
	}
}
