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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

// Grep searches file contents by regular expression.
type Grep struct {
	baseTool
}

// NewGrep creates the Grep tool.
func NewGrep() *Grep {
	return &Grep{baseTool: baseTool{
		name:            "Grep",
		description:     "Searches file contents with regular expressions and optional glob filtering.",
		concurrencySafe: true,
		readOnly:        true,
		stateInjected:   false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":          map[string]any{"type": "string", "description": "Regular expression pattern to search for."},
				"path":             map[string]any{"type": "string", "description": "File or directory to search. Defaults to current working directory."},
				"glob":             map[string]any{"type": "string", "description": "Glob pattern used to filter files."},
				"output_mode":      map[string]any{"type": "string", "description": "One of content, files, or count.", "default": "content"},
				"case_insensitive": map[string]any{"type": "boolean", "default": false},
				"head_limit":       map[string]any{"type": "integer", "description": "Maximum result rows to return."},
			},
			"required": []string{"pattern"},
		},
	}}
}

// MatchRule matches permission rules against the search path.
func (g *Grep) MatchRule(ruleContent string, input map[string]any) bool {
	path := stringValue(input, "path")
	if path == "" {
		return false
	}
	return permission.MatchPattern(ruleContent, path)
}

// GenerateSuggestions generates suggested rules for the search path.
func (g *Grep) GenerateSuggestions(input map[string]any) []permission.Rule {
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

// Execute runs regex search.
func (g *Grep) Execute(_ context.Context, input map[string]any, _ *astate.AgentState) (<-chan astool.ToolChunk, error) {
	originalPattern := stringValue(input, "pattern")
	re, err := compileGrepPattern(originalPattern, boolValue(input, "case_insensitive"))
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	searchPath, err := grepSearchPath(input)
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	files, err := grepFiles(searchPath, stringValue(input, "glob"))
	if err != nil {
		return errorText(err.Error()), nil
	}
	results, matched := collectGrepResults(files, re, grepOutputMode(input))
	if !matched {
		return successText("No matches found for pattern: " + originalPattern), nil
	}
	results = limitGrepResults(results, intValue(input, "head_limit", 0))
	return successText(strings.Join(results, "\n")), nil
}

func compileGrepPattern(pattern string, caseInsensitive bool) (*regexp.Regexp, error) {
	if strings.TrimSpace(pattern) == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	if caseInsensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}
	return re, nil
}

func grepSearchPath(input map[string]any) (string, error) {
	searchPath := stringValue(input, "path")
	if searchPath != "" {
		return searchPath, nil
	}
	return os.Getwd()
}

func grepOutputMode(input map[string]any) string {
	outputMode := stringValue(input, "output_mode")
	if outputMode == "" {
		return "content"
	}
	return outputMode
}

func collectGrepResults(files []string, re *regexp.Regexp, outputMode string) ([]string, bool) {
	results := make([]string, 0)
	matched := false
	for _, filePath := range files {
		lines, err := grepFile(filePath, re)
		if err != nil || len(lines) == 0 {
			continue
		}
		matched = true
		results = appendGrepResult(results, outputMode, filePath, lines)
	}
	if outputMode == "files" {
		sort.Strings(results)
	}
	return results, matched
}

func appendGrepResult(results []string, outputMode, filePath string, lines []grepLine) []string {
	switch outputMode {
	case "files":
		return append(results, filePath)
	case "count":
		return append(results, fmt.Sprintf("%s:%d", filePath, len(lines)))
	default:
		for _, line := range lines {
			results = append(results, fmt.Sprintf("%s:%d:%s", filePath, line.number, line.text))
		}
		return results
	}
}

func limitGrepResults(results []string, limit int) []string {
	if limit > 0 && limit < len(results) {
		return results[:limit]
	}
	return results
}

type grepLine struct {
	number int
	text   string
}

func grepFiles(searchPath, globPattern string) ([]string, error) {
	info, err := os.Stat(searchPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("path not found: %s", searchPath)
		}
		return nil, err
	}
	if !info.IsDir() {
		if globPattern != "" && !globMatch(globPattern, filepath.Base(searchPath)) {
			return nil, nil
		}
		return []string{searchPath}, nil
	}
	files := make([]string, 0)
	err = filepath.WalkDir(searchPath, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if globPattern != "" {
			rel, relErr := filepath.Rel(searchPath, path)
			if relErr != nil || !globMatch(globPattern, rel) {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

func grepFile(filePath string, re *regexp.Regexp) ([]grepLine, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(content) {
		return nil, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	matches := make([]grepLine, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		text := scanner.Text()
		if re.MatchString(text) {
			matches = append(matches, grepLine{number: lineNumber, text: text})
		}
	}
	return matches, scanner.Err()
}
