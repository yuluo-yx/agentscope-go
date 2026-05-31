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

package builtin

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

// Edit performs exact string replacement on an absolute file path.
type Edit struct {
	baseTool
}

// NewEdit creates the Edit tool.
func NewEdit() *Edit {
	return &Edit{baseTool: baseTool{
		name:            "Edit",
		description:     "Replaces an exact string in an absolute file path.",
		concurrencySafe: false,
		readOnly:        false,
		stateInjected:   true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path":   map[string]any{"type": "string", "description": "Absolute path of the file to edit."},
				"old_string":  map[string]any{"type": "string", "description": "Exact string to replace."},
				"new_string":  map[string]any{"type": "string", "description": "Replacement string."},
				"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences instead of requiring uniqueness.", "default": false},
			},
			"required": []string{"file_path", "old_string", "new_string"},
		},
	}}
}

// CheckPermissions performs permission checks for file edits.
func (e *Edit) CheckPermissions(_ context.Context, input map[string]any, ctx *permission.Context) (*permission.Decision, error) {
	return writableFilePermission(e.Name(), "editing", input, ctx)
}

// MatchRule matches permission rules against file_path.
func (e *Edit) MatchRule(ruleContent string, input map[string]any) bool {
	return fileMatchRule(ruleContent, input)
}

// GenerateSuggestions generates suggested rules for the target file directory.
func (e *Edit) GenerateSuggestions(input map[string]any) []permission.Rule {
	return fileSuggestions(e.Name(), input)
}

// Execute performs exact string replacement.
func (e *Edit) Execute(_ context.Context, input map[string]any, state *astate.AgentState) (<-chan astool.ToolChunk, error) {
	filePath, err := absolutePath(stringValue(input, "file_path"))
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	if _, statErr := os.Stat(filePath); statErr != nil {
		if os.IsNotExist(statErr) {
			return errorText("Error: File not found: " + filePath), nil
		}
		return errorText("Error: " + statErr.Error()), nil
	}
	oldString := stringValue(input, "old_string")
	newString := stringValue(input, "new_string")
	if oldString == newString {
		return errorText("Error: old_string and new_string are identical. No changes to make."), nil
	}
	if state == nil {
		return errorText(fmt.Sprintf("Error: agent state required to verify prior Read before editing existing file %s.", filePath)), nil
	}
	var lines []string
	if state.ToolContext == nil {
		return errorText("Error: To edit a file, you must first read it using the Read tool."), nil
	}
	cache, ok := state.ToolContext.GetCache(filePath)
	if !ok {
		return errorText("Error: To edit a file, you must first read it using the Read tool."), nil
	}
	lines = append([]string(nil), cache.Lines...)
	content := strings.Join(lines, "")
	occurrences := strings.Count(content, oldString)
	if occurrences == 0 {
		return errorText(fmt.Sprintf("Error: old_string not found in %s", filePath)), nil
	}
	replaceAll := boolValue(input, "replace_all")
	if occurrences > 1 && !replaceAll {
		return errorText(fmt.Sprintf("Error: old_string appears %d times in %s. Set replace_all=true to replace all occurrences, or make old_string more specific.", occurrences, filePath)), nil
	}
	updated := strings.Replace(content, oldString, newString, 1)
	replacementMsg := "1 occurrence"
	if replaceAll {
		updated = strings.ReplaceAll(content, oldString, newString)
		replacementMsg = fmt.Sprintf("all %d occurrences", occurrences)
	}
	if err := os.WriteFile(filePath, []byte(updated), defaultFileMode); err != nil {
		return errorText("Error writing file: " + err.Error()), nil
	}
	cacheFile(state, filePath, splitLinesPreserve(updated))
	return successText(fmt.Sprintf("Successfully replaced %s in %s", replacementMsg, filePath)), nil
}
