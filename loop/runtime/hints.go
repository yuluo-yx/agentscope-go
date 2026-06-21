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

package runtime

import (
	"strings"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/message"
)

func (m *Runtime) markHinted(agent agentpkg.AgentAccessor) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := loopKey(agent)
	if m.hinted[key] {
		return false
	}
	m.hinted[key] = true
	return true
}

func (m *Runtime) clearHintLocked(agent agentpkg.AgentAccessor) {
	key := loopKey(agent)
	delete(m.hinted, key)
}

func loopKey(agent agentpkg.AgentAccessor) string {
	if agent == nil || agent.AgentState() == nil {
		return ":"
	}
	state := agent.AgentState()
	return state.SessionID + ":" + state.ReplyID
}

func appendHint(agent agentpkg.AgentAccessor, hint string) error {
	if strings.TrimSpace(hint) == "" || agent == nil || agent.AgentState() == nil {
		return nil
	}
	block := message.NewHintBlock(hint, message.WithHintSource("loop"))
	state := agent.AgentState()
	if len(state.Context) > 0 {
		last := state.Context[len(state.Context)-1]
		if last != nil && last.Role == message.RoleAssistant {
			last.Content = append(last.Content, block)
			return nil
		}
	}
	msg, err := message.NewAssistantMessage(agent.AgentName(), message.ContentBlockList{block})
	if err != nil {
		return err
	}
	state.Context = append(state.Context, msg)
	return nil
}
