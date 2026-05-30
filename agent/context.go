// Copyright 20\d\d AgentScope Go
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

package agent

import (
	"context"
	"unicode/utf8"

	"github.com/yuluo-yx/agentscope-go/message"
)

// CompressContext runs the phase-four local context cleanup.
//
// The current Go version has not introduced workspace/offloader yet, so this
// only truncates tool results locally. Cross-window summary compression will be
// added with the later workspace/offloader stage.
func (a *Agent) CompressContext(context.Context) error {
	if a == nil || a.state == nil {
		return nil
	}
	limit := a.contextConfig.ToolResultLimit
	if limit <= 0 {
		limit = DefaultContextConfig().ToolResultLimit
	}
	for _, msg := range a.state.Context {
		if msg == nil {
			continue
		}
		for _, block := range msg.GetContentBlocks("tool_result") {
			result, ok := block.(*message.ToolResultBlock)
			if !ok {
				continue
			}
			truncateToolResult(result, limit)
		}
	}
	return nil
}

func truncateToolResult(result *message.ToolResultBlock, limit int) {
	if result == nil || limit <= 0 {
		return
	}
	if result.Output.Raw != "" && len(result.Output.Raw) > limit {
		result.Output.Raw = truncateUTF8(result.Output.Raw, limit) + "\n... (tool result truncated)"
	}
	for _, block := range result.Output.Blocks {
		text, ok := block.(*message.TextBlock)
		if !ok || len(text.Text) <= limit {
			continue
		}
		text.Text = truncateUTF8(text.Text, limit) + "\n... (tool result truncated)"
	}
}

func truncateUTF8(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(text) <= limit {
		return text
	}
	end := 0
	for index, r := range text {
		next := index + utf8.RuneLen(r)
		if next > limit {
			break
		}
		end = next
	}
	return text[:end]
}
