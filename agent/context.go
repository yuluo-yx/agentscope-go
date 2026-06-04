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

package agent

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/yuluo-yx/agentscope-go/message"
)

// CompressContext runs local context cleanup and drives the workspace offload lifecycle.
func (a *Agent) CompressContext(ctx context.Context) error {
	if a == nil || a.state == nil {
		return nil
	}
	if err := a.offloadDataBlocks(ctx); err != nil {
		return err
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
			if a.offloader != nil {
				if err := a.offloadToolResult(ctx, result, limit); err != nil {
					return err
				}
				continue
			}
			truncateToolResult(result, limit)
		}
	}
	return nil
}

func (a *Agent) offloadDataBlocks(ctx context.Context) error {
	if a == nil || a.offloader == nil || a.state == nil {
		return nil
	}
	for _, msg := range a.state.Context {
		if msg == nil {
			continue
		}
		if err := a.offloadDataBlocksInList(ctx, msg.Content); err != nil {
			return err
		}
	}
	if len(a.state.Summary.Blocks) > 0 {
		return a.offloadDataBlocksInList(ctx, a.state.Summary.Blocks)
	}
	return nil
}

func (a *Agent) offloadDataBlocksInList(ctx context.Context, blocks message.ContentBlockList) error {
	for _, block := range blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch typed := block.(type) {
		case *message.DataBlock:
			if _, ok := typed.Source.(*message.Base64Source); !ok {
				continue
			}
			offloaded, err := a.offloader.OffloadDataBlock(ctx, typed)
			if err != nil {
				return err
			}
			if offloaded != nil {
				*typed = *offloaded
			}
		case *message.ToolResultBlock:
			if err := a.offloadDataBlocksInList(ctx, typed.Output.Blocks); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Agent) offloadToolResult(ctx context.Context, result *message.ToolResultBlock, limit int) error {
	if result == nil || limit <= 0 || toolResultHasOffloadReminder(result) {
		return nil
	}
	reserved, offloaded, ok := splitToolResult(result, limit)
	if !ok {
		return nil
	}
	path, err := a.offloader.OffloadToolResult(ctx, a.state.SessionID, offloaded)
	if err != nil {
		return err
	}
	*result = *reserved
	appendToolResultReminder(result, path)
	return nil
}

func splitToolResult(result *message.ToolResultBlock, limit int) (*message.ToolResultBlock, *message.ToolResultBlock, bool) {
	if result == nil || limit <= 0 {
		return result, nil, false
	}
	if result.Output.Raw != "" {
		if len(result.Output.Raw) <= limit {
			return result, nil, false
		}
		head, tail := splitUTF8(result.Output.Raw, limit)
		return message.NewToolResultBlock(result.ID, result.Name, message.ToolResultOutput{Raw: head}, result.State),
			message.NewToolResultBlock(result.ID, result.Name, message.ToolResultOutput{Raw: tail}, result.State),
			true
	}
	reservedBlocks := message.ContentBlockList{}
	offloadBlocks := message.ContentBlockList{}
	used := 0
	offloading := false
	for _, block := range result.Output.Blocks {
		if block == nil {
			continue
		}
		if offloading {
			offloadBlocks = append(offloadBlocks, block.Clone())
			continue
		}
		text, ok := block.(*message.TextBlock)
		if !ok {
			reservedBlocks = append(reservedBlocks, block.Clone())
			continue
		}
		remaining := limit - used
		if remaining <= 0 {
			offloadBlocks = append(offloadBlocks, block.Clone())
			offloading = true
			continue
		}
		if len(text.Text) <= remaining {
			reservedBlocks = append(reservedBlocks, text.Clone())
			used += len(text.Text)
			continue
		}
		head, tail := splitUTF8(text.Text, remaining)
		if head != "" {
			reservedBlocks = append(reservedBlocks, message.NewTextBlock(head, message.WithBlockID(text.ID)))
		}
		if tail != "" {
			offloadBlocks = append(offloadBlocks, message.NewTextBlock(tail, message.WithBlockID(text.ID)))
		}
		offloading = true
	}
	if len(offloadBlocks) == 0 {
		return result, nil, false
	}
	return message.NewToolResultBlock(result.ID, result.Name, message.ToolResultOutput{Blocks: reservedBlocks}, result.State),
		message.NewToolResultBlock(result.ID, result.Name, message.ToolResultOutput{Blocks: offloadBlocks}, result.State),
		true
}

func appendToolResultReminder(result *message.ToolResultBlock, path string) {
	reminder := "\n<<<TRUNCATED>>>\n<system-reminder>The remaining content has been omitted for limited context and offloaded to '" + path + "'.</system-reminder>"
	if result.Output.Raw != "" {
		result.Output.Raw += reminder
		return
	}
	result.Output.Blocks = append(result.Output.Blocks, message.NewTextBlock(reminder))
}

func toolResultHasOffloadReminder(result *message.ToolResultBlock) bool {
	if result == nil {
		return false
	}
	if strings.Contains(result.Output.Raw, "offloaded to") {
		return true
	}
	for _, block := range result.Output.Blocks {
		text, ok := block.(*message.TextBlock)
		if ok && strings.Contains(text.Text, "offloaded to") {
			return true
		}
	}
	return false
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

func splitUTF8(text string, limit int) (string, string) {
	if limit <= 0 {
		return "", text
	}
	if len(text) <= limit {
		return text, ""
	}
	head := truncateUTF8(text, limit)
	return head, text[len(head):]
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
