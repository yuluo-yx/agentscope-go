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

	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	astate "github.com/yuluo-yx/agentscope-go/pkg/state"
	astool "github.com/yuluo-yx/agentscope-go/pkg/tool"
)

const maxReadLineCharacters = 2000

// Read reads an absolute file path and returns numbered content.
type Read struct {
	baseTool
}

// NewRead creates the Read tool.
func NewRead() *Read {
	return &Read{baseTool: baseTool{
		name:            "Read",
		description:     "Reads a file from an absolute path and returns line-numbered content.",
		concurrencySafe: true,
		readOnly:        true,
		stateInjected:   true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Absolute path of the file to read."},
				"offset":    map[string]any{"type": "integer", "description": "1-based line offset.", "default": 1},
				"limit":     map[string]any{"type": "integer", "description": "Maximum number of lines to return.", "default": 2000},
			},
			"required": []string{"file_path"},
		},
	}}
}

// MatchRule matches permission rules against file_path.
func (r *Read) MatchRule(ruleContent string, input map[string]any) bool {
	return fileMatchRule(ruleContent, input)
}

// GenerateSuggestions generates suggested rules for the target file directory.
func (r *Read) GenerateSuggestions(input map[string]any) []permission.Rule {
	return fileSuggestions(r.Name(), input)
}

// Execute reads the file and returns numbered content.
func (r *Read) Execute(_ context.Context, input map[string]any, state *astate.AgentState) (<-chan astool.ToolChunk, error) {
	filePath, err := absolutePath(stringValue(input, "file_path"))
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return errorText("Error: File does not exist: " + filePath), nil
		}
		return errorText("Error: " + err.Error()), nil
	}
	if info.IsDir() {
		return errorText("Error: Path is a directory, not a file: " + filePath), nil
	}
	lines, _, err := cachedOrDiskLines(filePath, state)
	if err != nil {
		return errorText("Error reading file: " + err.Error()), nil
	}
	cacheFile(state, filePath, lines)

	offset := intValue(input, "offset", 1)
	if offset < 1 {
		offset = 1
	}
	limit := intValue(input, "limit", 2000)
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}
	start := offset - 1
	if start > len(lines) {
		start = len(lines)
	}
	end := start + limit
	if end > len(lines) {
		end = len(lines)
	}
	formatted := make([]string, 0, end-start)
	for index, line := range lines[start:end] {
		lineContent := strings.TrimRight(line, "\r\n")
		if len([]rune(lineContent)) > maxReadLineCharacters {
			runes := []rune(lineContent)
			lineContent = string(runes[:maxReadLineCharacters]) + "[truncated]"
		}
		formatted = append(formatted, fmt.Sprintf("%6d\t%s", offset+index, lineContent))
	}
	return successText(strings.Join(formatted, "\n")), nil
}
