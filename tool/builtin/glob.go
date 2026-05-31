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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

// Glob finds files by glob pattern.
type Glob struct {
	baseTool
}

// NewGlob creates the Glob tool.
func NewGlob() *Glob {
	return &Glob{baseTool: baseTool{
		name:            "Glob",
		description:     "Fast file pattern matching with support for patterns such as **/*.go.",
		concurrencySafe: true,
		readOnly:        true,
		stateInjected:   false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Glob pattern to match."},
				"path":    map[string]any{"type": "string", "description": "Base directory. Defaults to current working directory."},
			},
			"required": []string{"pattern"},
		},
	}}
}

// MatchRule matches permission rules against the search path or glob pattern.
func (g *Glob) MatchRule(ruleContent string, input map[string]any) bool {
	path := stringValue(input, "path")
	pattern := stringValue(input, "pattern")
	return (path != "" && permission.MatchPattern(ruleContent, path)) ||
		(pattern != "" && permission.MatchPattern(ruleContent, pattern))
}

// GenerateSuggestions generates suggested rules for the search directory.
func (g *Glob) GenerateSuggestions(input map[string]any) []permission.Rule {
	path := stringValue(input, "path")
	if path == "" {
		path, _ = os.Getwd()
	}
	return []permission.Rule{{
		ToolName:    g.Name(),
		RuleContent: filepath.ToSlash(filepath.Clean(path)) + "/**",
		Behavior:    permission.BehaviorAllow,
		Source:      sourceSuggested,
	}}
}

// Execute runs glob file matching.
func (g *Glob) Execute(_ context.Context, input map[string]any, _ *astate.AgentState) (<-chan astool.ToolChunk, error) {
	pattern := stringValue(input, "pattern")
	if strings.TrimSpace(pattern) == "" {
		return errorText("Error: pattern is required"), nil
	}
	baseDir := stringValue(input, "path")
	if baseDir == "" {
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return errorText("Error: " + err.Error()), nil
		}
	}
	info, err := os.Stat(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errorText("Directory not found: " + baseDir), nil
		}
		return errorText("Error: " + err.Error()), nil
	}
	if !info.IsDir() {
		return errorText("Error: path is not a directory: " + baseDir), nil
	}
	matches := make([]string, 0)
	if err := filepath.WalkDir(baseDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return nil
		}
		if globMatch(pattern, rel) {
			matches = append(matches, path)
		}
		return nil
	}); err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		left, leftErr := os.Stat(matches[i])
		right, rightErr := os.Stat(matches[j])
		if leftErr != nil || rightErr != nil {
			return matches[i] < matches[j]
		}
		return left.ModTime().After(right.ModTime())
	})
	if len(matches) == 0 {
		return successText("No files found matching pattern: " + pattern), nil
	}
	return successText(strings.Join(matches, "\n")), nil
}
