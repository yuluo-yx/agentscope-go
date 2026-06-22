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

package docker

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	asstate "github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/builtin"
)

const maxReadLineCharacters = 2000

type dockerTool struct {
	workspace       *Workspace
	delegate        tool.Tool
	readOnly        bool
	concurrencySafe bool
	execute         func(context.Context, map[string]any, *asstate.AgentState) (<-chan tool.ToolChunk, error)
}

func newBashTool(workspace *Workspace) tool.Tool {
	return &dockerTool{
		workspace:       workspace,
		delegate:        builtin.NewBash(),
		readOnly:        false,
		concurrencySafe: false,
		execute:         executeBash(workspace),
	}
}

func newEditTool(workspace *Workspace) tool.Tool {
	return &dockerTool{
		workspace:       workspace,
		delegate:        builtin.NewEdit(),
		readOnly:        false,
		concurrencySafe: false,
		execute:         executeEdit(workspace),
	}
}

func newGlobTool(workspace *Workspace) tool.Tool {
	return &dockerTool{
		workspace:       workspace,
		delegate:        builtin.NewGlob(),
		readOnly:        true,
		concurrencySafe: true,
		execute:         executeGlob(workspace),
	}
}

func newGrepTool(workspace *Workspace) tool.Tool {
	return &dockerTool{
		workspace:       workspace,
		delegate:        builtin.NewGrep(),
		readOnly:        true,
		concurrencySafe: true,
		execute:         executeGrep(workspace),
	}
}

func newReadTool(workspace *Workspace) tool.Tool {
	return &dockerTool{
		workspace:       workspace,
		delegate:        builtin.NewRead(),
		readOnly:        true,
		concurrencySafe: true,
		execute:         executeRead(workspace),
	}
}

func newWriteTool(workspace *Workspace) tool.Tool {
	return &dockerTool{
		workspace:       workspace,
		delegate:        builtin.NewWrite(),
		readOnly:        false,
		concurrencySafe: false,
		execute:         executeWrite(workspace),
	}
}

func (t *dockerTool) Name() string {
	return t.delegate.Name()
}

func (t *dockerTool) Description() string {
	return t.delegate.Description() + " The operation runs inside the Docker workspace container."
}

func (t *dockerTool) InputSchema() map[string]any {
	return t.delegate.InputSchema()
}

func (t *dockerTool) IsConcurrencySafe() bool {
	return t.concurrencySafe
}

func (t *dockerTool) IsReadOnly() bool {
	return t.readOnly
}

func (t *dockerTool) IsExternalTool() bool {
	return false
}

func (t *dockerTool) IsStateInjected() bool {
	return false
}

func (t *dockerTool) IsMCP() bool {
	return false
}

func (t *dockerTool) MCPName() string {
	return ""
}

func (t *dockerTool) CheckPermissions(ctx context.Context, input map[string]any, permissionCtx *permission.Context) (*permission.Decision, error) {
	return t.delegate.CheckPermissions(ctx, input, permissionCtx)
}

func (t *dockerTool) MatchRule(ruleContent string, input map[string]any) bool {
	return t.delegate.MatchRule(ruleContent, input)
}

func (t *dockerTool) GenerateSuggestions(input map[string]any) []permission.Rule {
	return t.delegate.GenerateSuggestions(input)
}

func (t *dockerTool) Execute(ctx context.Context, input map[string]any, state *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	if t.workspace == nil || t.workspace.runtime == nil || t.workspace.containerID == "" {
		return errorText("Error: Docker workspace is not initialized."), nil
	}
	return t.execute(ctx, input, state)
}

func executeBash(workspace *Workspace) func(context.Context, map[string]any, *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	return func(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan tool.ToolChunk, error) {
		command := strings.TrimSpace(stringValue(input, "command"))
		if command == "" {
			return errorText("Error: command is required"), nil
		}
		timeout := timeoutValue(input, "timeout_ms", defaultBashTimeout, maxBashTimeout)
		result, err := workspace.runtime.Run(ctx, workspace.containerID, runRequest{
			Command: command,
			User:    workspace.containerUser(),
			Workdir: workspace.containerWorkdir,
			Timeout: timeout,
		})
		if err != nil {
			return errorText("Error: " + err.Error()), nil
		}
		output := result.Stdout + result.Stderr
		if result.ExitCode != 0 {
			return errorText(fmt.Sprintf("Command failed with exit code %d: %s\n%s", result.ExitCode, command, output)), nil
		}
		return successText(output), nil
	}
}

func executeRead(workspace *Workspace) func(context.Context, map[string]any, *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	return func(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan tool.ToolChunk, error) {
		filePath, err := requireContainerPath(input, "file_path")
		if err != nil {
			return errorText("Error: " + err.Error()), nil
		}
		content, err := workspace.runtime.ReadFile(ctx, workspace.containerID, filePath)
		if err != nil {
			return errorText("Error reading file: " + err.Error()), nil
		}
		lines := splitLinesPreserve(string(content))
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
}

func executeWrite(workspace *Workspace) func(context.Context, map[string]any, *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	return func(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan tool.ToolChunk, error) {
		filePath, err := requireContainerPath(input, "file_path")
		if err != nil {
			return errorText("Error: " + err.Error()), nil
		}
		content := stringValue(input, "content")
		if err := workspace.runtime.WriteFile(ctx, workspace.containerID, filePath, []byte(content), defaultFileMode); err != nil {
			return errorText("Error writing file: " + err.Error()), nil
		}
		lineCount := len(strings.Split(content, "\n"))
		return successText(fmt.Sprintf("The file %s has been written successfully inside the Docker workspace (%d lines).", filePath, lineCount)), nil
	}
}

func executeEdit(workspace *Workspace) func(context.Context, map[string]any, *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	return func(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan tool.ToolChunk, error) {
		filePath, err := requireContainerPath(input, "file_path")
		if err != nil {
			return errorText("Error: " + err.Error()), nil
		}
		oldString := stringValue(input, "old_string")
		newString := stringValue(input, "new_string")
		if oldString == newString {
			return errorText("Error: old_string and new_string are identical. No changes to make."), nil
		}
		contentBytes, err := workspace.runtime.ReadFile(ctx, workspace.containerID, filePath)
		if err != nil {
			return errorText("Error reading file: " + err.Error()), nil
		}
		content := string(contentBytes)
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
		if err := workspace.runtime.WriteFile(ctx, workspace.containerID, filePath, []byte(updated), defaultFileMode); err != nil {
			return errorText("Error writing file: " + err.Error()), nil
		}
		return successText(fmt.Sprintf("Successfully replaced %s in %s inside the Docker workspace.", replacementMsg, filePath)), nil
	}
}

func executeGlob(workspace *Workspace) func(context.Context, map[string]any, *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	return func(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan tool.ToolChunk, error) {
		pattern := strings.TrimSpace(stringValue(input, "pattern"))
		if pattern == "" {
			return errorText("Error: pattern is required"), nil
		}
		baseDir := cleanContainerPath(stringValue(input, "path"))
		if baseDir == "" {
			baseDir = workspace.containerWorkdir
		}
		command := fmt.Sprintf("find %s -type f -print", shellQuote(baseDir))
		result, err := workspace.runtime.Run(ctx, workspace.containerID, runRequest{
			Command: command,
			User:    workspace.containerUser(),
			Workdir: workspace.containerWorkdir,
			Timeout: defaultBashTimeout,
		})
		if err != nil {
			return errorText("Error: " + err.Error()), nil
		}
		if result.ExitCode != 0 {
			return errorText(strings.TrimSpace(result.Stdout + result.Stderr)), nil
		}
		matches := filterGlobMatches(baseDir, result.Stdout, pattern)
		if len(matches) == 0 {
			return successText("No files found matching pattern: " + pattern), nil
		}
		sort.Strings(matches)
		return successText(strings.Join(matches, "\n")), nil
	}
}

func executeGrep(workspace *Workspace) func(context.Context, map[string]any, *asstate.AgentState) (<-chan tool.ToolChunk, error) {
	return func(ctx context.Context, input map[string]any, _ *asstate.AgentState) (<-chan tool.ToolChunk, error) {
		pattern := stringValue(input, "pattern")
		if strings.TrimSpace(pattern) == "" {
			return errorText("Error: pattern is required"), nil
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return errorText("Error: invalid regex pattern: " + err.Error()), nil
		}
		searchPath := cleanContainerPath(stringValue(input, "path"))
		if searchPath == "" {
			searchPath = workspace.containerWorkdir
		}
		args := "-R -n -E"
		if boolValue(input, "case_insensitive") {
			args += " -i"
		}
		command := fmt.Sprintf("grep %s -- %s %s", args, shellQuote(pattern), shellQuote(searchPath))
		result, err := workspace.runtime.Run(ctx, workspace.containerID, runRequest{
			Command: command,
			User:    workspace.containerUser(),
			Workdir: workspace.containerWorkdir,
			Timeout: defaultBashTimeout,
		})
		if err != nil {
			return errorText("Error: " + err.Error()), nil
		}
		if result.ExitCode == 1 {
			return successText("No matches found for pattern: " + pattern), nil
		}
		if result.ExitCode != 0 {
			return errorText(strings.TrimSpace(result.Stdout + result.Stderr)), nil
		}
		results := filterGrepOutput(result.Stdout, stringValue(input, "glob"), grepOutputMode(input))
		results = limitStrings(results, intValue(input, "head_limit", 0))
		if len(results) == 0 {
			return successText("No matches found for pattern: " + pattern), nil
		}
		return successText(strings.Join(results, "\n")), nil
	}
}

func requireContainerPath(input map[string]any, key string) (string, error) {
	path := strings.TrimSpace(stringValue(input, key))
	if path == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("%s must be an absolute container path", key)
	}
	return filepath.ToSlash(filepath.Clean(path)), nil
}

func successText(text string) <-chan tool.ToolChunk {
	return singleTextChunk(text, message.ToolResultSuccess)
}

func errorText(text string) <-chan tool.ToolChunk {
	return singleTextChunk(text, message.ToolResultError)
}

func singleTextChunk(text string, state message.ToolResultState) <-chan tool.ToolChunk {
	chunks := make(chan tool.ToolChunk, 1)
	chunks <- *tool.NewToolChunk(
		message.ContentBlockList{message.NewTextBlock(text)},
		tool.WithToolChunkState(state),
	)
	close(chunks)
	return chunks
}

func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func boolValue(input map[string]any, key string) bool {
	value, _ := input[key].(bool)
	return value
}

func intValue(input map[string]any, key string, fallback int) int {
	value, ok := input[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var out int
		if _, err := fmt.Sscanf(typed, "%d", &out); err == nil {
			return out
		}
	}
	return fallback
}

func timeoutValue(input map[string]any, key string, fallback, max time.Duration) time.Duration {
	timeoutMS := intValue(input, key, int(fallback/time.Millisecond))
	if timeoutMS <= 0 {
		return fallback
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout > max {
		return max
	}
	return timeout
}

func splitLinesPreserve(content string) []string {
	if content == "" {
		return []string{}
	}
	lines := strings.SplitAfter(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func filterGlobMatches(baseDir, stdout, pattern string) []string {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	matches := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rel := strings.TrimPrefix(line, strings.TrimRight(baseDir, "/")+"/")
		if matchGlob(pattern, rel) {
			matches = append(matches, line)
		}
	}
	return matches
}

func matchGlob(pattern, value string) bool {
	pattern = filepath.ToSlash(pattern)
	value = filepath.ToSlash(value)
	ok, err := filepath.Match(pattern, value)
	if err == nil && ok {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		ok, err = filepath.Match(strings.TrimPrefix(pattern, "**/"), filepath.Base(value))
		if err == nil && ok {
			return true
		}
	}
	if !strings.Contains(pattern, "**") {
		return false
	}
	regex := regexp.QuoteMeta(pattern)
	regex = strings.ReplaceAll(regex, `\*\*`, ".*")
	regex = strings.ReplaceAll(regex, `\*`, `[^/]*`)
	matched, _ := regexp.MatchString("^"+regex+"$", value)
	return matched
}

func filterGrepOutput(stdout, globPattern, outputMode string) []string {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	results := make([]string, 0, len(lines))
	seenFiles := map[string]bool{}
	for _, line := range lines {
		if line == "" {
			continue
		}
		filePath := line
		if colon := strings.Index(line, ":"); colon >= 0 {
			filePath = line[:colon]
		}
		if globPattern != "" {
			ok, err := filepath.Match(globPattern, filepath.Base(filePath))
			if err != nil || !ok {
				continue
			}
		}
		switch outputMode {
		case "files":
			if !seenFiles[filePath] {
				seenFiles[filePath] = true
				results = append(results, filePath)
			}
		case "count":
			seenFiles[filePath] = true
		default:
			results = append(results, line)
		}
	}
	if outputMode == "count" {
		for filePath := range seenFiles {
			count := 0
			for _, line := range lines {
				if strings.HasPrefix(line, filePath+":") {
					count++
				}
			}
			results = append(results, fmt.Sprintf("%s:%d", filePath, count))
		}
		sort.Strings(results)
	}
	return results
}

func grepOutputMode(input map[string]any) string {
	outputMode := stringValue(input, "output_mode")
	if outputMode == "" {
		return "content"
	}
	return outputMode
}

func limitStrings(values []string, limit int) []string {
	if limit > 0 && limit < len(values) {
		return values[:limit]
	}
	return values
}
