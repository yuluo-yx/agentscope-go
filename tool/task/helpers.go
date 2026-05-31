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

package task

import (
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/message"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/utils"
)

func taskContextOrError(toolName string, state *astate.AgentState) (*astate.TaskContext, <-chan astool.ToolChunk) {
	if state == nil {
		return nil, errorText(fmt.Sprintf("Error: %s requires AgentState to be provided.", toolName))
	}
	if state.TaskContext == nil {
		state.TaskContext = astate.NewTaskContext()
	}
	return state.TaskContext, nil
}

func successText(text string) <-chan astool.ToolChunk {
	return singleChunk(astool.NewToolChunk(
		"",
		message.ContentBlockList{message.NewTextBlock(text)},
		astool.WithToolChunkState(message.ToolResultSuccess),
	))
}

func errorText(text string) <-chan astool.ToolChunk {
	return singleChunk(astool.NewToolChunk(
		"",
		message.ContentBlockList{message.NewTextBlock(text)},
		astool.WithToolChunkState(message.ToolResultError),
	))
}

func singleChunk(chunk *astool.ToolChunk) <-chan astool.ToolChunk {
	chunks := make(chan astool.ToolChunk, 1)
	if chunk != nil {
		chunks <- *chunk
	}
	close(chunks)
	return chunks
}

func stringValue(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, ok := input[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func optionalString(input map[string]any, key string) (string, bool) {
	if input == nil {
		return "", false
	}
	value, ok := input[key]
	if !ok || value == nil {
		return "", false
	}
	if text, ok := value.(string); ok {
		return text, true
	}
	return fmt.Sprint(value), true
}

func metadataValue(input map[string]any, key string) map[string]any {
	value, _ := optionalMetadata(input, key)
	return value
}

func optionalMetadata(input map[string]any, key string) (map[string]any, bool) {
	if input == nil {
		return nil, false
	}
	value, ok := input[key]
	if !ok || value == nil {
		return nil, false
	}
	if metadata, ok := value.(map[string]any); ok {
		return utils.CloneAnyMap(metadata), true
	}
	return nil, false
}

func stringSliceValue(input map[string]any, key string) []string {
	if input == nil {
		return nil
	}
	switch value := input[key].(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if item == nil {
				continue
			}
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}

func taskIndex(taskContext *astate.TaskContext, id string) int {
	if taskContext == nil {
		return -1
	}
	for index := range taskContext.Tasks {
		if taskContext.Tasks[index].ID == id {
			return index
		}
	}
	return -1
}

func prefixedIDs(ids []string) string {
	prefixed := make([]string, 0, len(ids))
	for _, id := range ids {
		prefixed = append(prefixed, "#"+id)
	}
	return strings.Join(prefixed, ", ")
}
