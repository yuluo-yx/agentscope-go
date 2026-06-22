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
	"path/filepath"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	astate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

// Write writes content to an absolute file path.
type Write struct {
	baseTool
}

// NewWrite creates the Write tool.
func NewWrite() *Write {
	return &Write{baseTool: baseTool{
		name:            "Write",
		description:     "Writes content to an absolute file path, creating parent directories when needed.",
		concurrencySafe: false,
		readOnly:        false,
		stateInjected:   true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Absolute path of the file to write."},
				"content":   map[string]any{"type": "string", "description": "Content to write."},
			},
			"required": []string{"file_path", "content"},
		},
	}}
}

// CheckPermissions performs permission checks for file writes.
func (w *Write) CheckPermissions(_ context.Context, input map[string]any, ctx *permission.Context) (*permission.Decision, error) {
	return writableFilePermission(w.Name(), "writing", input, ctx)
}

// MatchRule matches permission rules against file_path.
func (w *Write) MatchRule(ruleContent string, input map[string]any) bool {
	return fileMatchRule(ruleContent, input)
}

// GenerateSuggestions generates suggested rules for the target file directory.
func (w *Write) GenerateSuggestions(input map[string]any) []permission.Rule {
	return fileSuggestions(w.Name(), input)
}

// Execute writes file content.
func (w *Write) Execute(_ context.Context, input map[string]any, state *astate.AgentState) (<-chan astool.ToolChunk, error) {
	filePath, err := absolutePath(stringValue(input, "file_path"))
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	content := stringValue(input, "content")
	if _, statErr := os.Stat(filePath); statErr == nil {
		if state == nil {
			return errorText(fmt.Sprintf("Error: agent state required to verify prior Read before writing existing file %s.", filePath)), nil
		}
		if state.ToolContext == nil {
			return errorText(fmt.Sprintf("Error: File %s exists but has not been read yet. You must read the file first before writing to it.", filePath)), nil
		}
		if _, ok := state.ToolContext.GetCache(filePath); !ok {
			return errorText(fmt.Sprintf("Error: File %s exists but has not been read yet. You must read the file first before writing to it.", filePath)), nil
		}
	} else if !os.IsNotExist(statErr) {
		return errorText("Error: " + statErr.Error()), nil
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return errorText("Error creating parent directories: " + err.Error()), nil
	}
	if err := os.WriteFile(filePath, []byte(content), defaultFileMode); err != nil {
		return errorText("Error writing file: " + err.Error()), nil
	}
	cacheFile(state, filePath, splitLinesPreserve(content))
	lineCount := len(strings.Split(content, "\n"))
	return successText(fmt.Sprintf("The file %s has been written successfully (%d lines).", filePath, lineCount)), nil
}
