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

package sandboxed

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/permission"
	"github.com/yuluo-yx/agentscope-go/pkg/state"
	"github.com/yuluo-yx/agentscope-go/pkg/tool"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/builtin"
)

const (
	defaultBashTimeout    = 120 * time.Second
	maxBashTimeout        = 10 * time.Minute
	maxReadLineCharacters = 2000
)

type remoteTool struct {
	workspace       *Workspace
	delegate        tool.Tool
	readOnly        bool
	concurrencySafe bool
	execute         func(context.Context, map[string]any, *state.AgentState) (<-chan tool.ToolChunk, error)
}

func newRemoteTools(workspace *Workspace) []tool.Tool {
	return []tool.Tool{
		newRemoteTool(workspace, builtin.NewBash(), false, false, executeBash),
		newRemoteTool(workspace, builtin.NewEdit(), false, false, executeEdit),
		newRemoteTool(workspace, builtin.NewGlob(), true, true, executeGlob),
		newRemoteTool(workspace, builtin.NewGrep(), true, true, executeGrep),
		newRemoteTool(workspace, builtin.NewRead(), true, true, executeRead),
		newRemoteTool(workspace, builtin.NewWrite(), false, false, executeWrite),
	}
}

func newRemoteTool(
	workspace *Workspace,
	delegate tool.Tool,
	readOnly bool,
	concurrencySafe bool,
	execute func(context.Context, *Workspace, map[string]any, *state.AgentState) (<-chan tool.ToolChunk, error),
) tool.Tool {
	return &remoteTool{
		workspace:       workspace,
		delegate:        delegate,
		readOnly:        readOnly,
		concurrencySafe: concurrencySafe,
		execute: func(
			ctx context.Context,
			input map[string]any,
			agentState *state.AgentState,
		) (<-chan tool.ToolChunk, error) {
			return execute(ctx, workspace, input, agentState)
		},
	}
}

func (t *remoteTool) Name() string { return t.delegate.Name() }

func (t *remoteTool) Description() string {
	return t.delegate.Description() + " The operation runs inside the remote workspace sandbox."
}

func (t *remoteTool) InputSchema() map[string]any { return t.delegate.InputSchema() }

func (t *remoteTool) IsConcurrencySafe() bool { return t.concurrencySafe }

func (t *remoteTool) IsReadOnly() bool { return t.readOnly }

func (*remoteTool) IsExternalTool() bool { return false }

func (*remoteTool) IsStateInjected() bool { return false }

func (*remoteTool) IsMCP() bool { return false }

func (*remoteTool) MCPName() string { return "" }

func (t *remoteTool) CheckPermissions(
	ctx context.Context,
	input map[string]any,
	permissionCtx *permission.Context,
) (*permission.Decision, error) {
	return t.delegate.CheckPermissions(ctx, input, permissionCtx)
}

func (t *remoteTool) MatchRule(ruleContent string, input map[string]any) bool {
	return t.delegate.MatchRule(ruleContent, input)
}

func (t *remoteTool) GenerateSuggestions(input map[string]any) []permission.Rule {
	return t.delegate.GenerateSuggestions(input)
}

func (t *remoteTool) Execute(
	ctx context.Context,
	input map[string]any,
	agentState *state.AgentState,
) (<-chan tool.ToolChunk, error) {
	if t == nil || t.workspace == nil {
		return errorText("Error: remote workspace is not initialized."), nil
	}
	t.workspace.mu.Lock()
	defer t.workspace.mu.Unlock()
	if !t.workspace.alive || t.workspace.backend == nil {
		return errorText("Error: remote workspace is not initialized."), nil
	}
	return t.execute(ctx, input, agentState)
}

func executeBash(
	ctx context.Context,
	workspace *Workspace,
	input map[string]any,
	_ *state.AgentState,
) (<-chan tool.ToolChunk, error) {
	command := strings.TrimSpace(stringValue(input, "command"))
	if command == "" {
		return errorText("Error: command is required"), nil
	}
	timeout := timeoutValue(input, "timeout_ms", defaultBashTimeout, maxBashTimeout)
	result, err := workspace.backend.Exec(ctx, []string{"/bin/bash", "-lc", command}, ExecOptions{
		CWD:     workspace.workdir,
		Timeout: timeout,
	})
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	output := string(result.Stdout) + string(result.Stderr)
	if !result.OK() {
		return errorText(fmt.Sprintf(
			"Command failed with exit code %d: %s\n%s",
			result.ExitCode,
			command,
			output,
		)), nil
	}
	return successText(output), nil
}

func executeRead(
	ctx context.Context,
	workspace *Workspace,
	input map[string]any,
	_ *state.AgentState,
) (<-chan tool.ToolChunk, error) {
	filename, err := normalizeSandboxPath(stringValue(input, "file_path"), workspace.workdir)
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	content, err := workspace.backend.ReadFile(ctx, filename)
	if err != nil {
		return errorText("Error reading file: " + err.Error()), nil
	}
	lines := splitLinesPreserve(string(content))
	offset := max(intValue(input, "offset", 1), 1)
	limit := intValue(input, "limit", 2000)
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}
	start := min(offset-1, len(lines))
	end := min(start+limit, len(lines))
	formatted := make([]string, 0, end-start)
	for index, line := range lines[start:end] {
		line = strings.TrimRight(line, "\r\n")
		runes := []rune(line)
		if len(runes) > maxReadLineCharacters {
			line = string(runes[:maxReadLineCharacters]) + "[truncated]"
		}
		formatted = append(formatted, fmt.Sprintf("%6d\t%s", offset+index, line))
	}
	return successText(strings.Join(formatted, "\n")), nil
}

func executeWrite(
	ctx context.Context,
	workspace *Workspace,
	input map[string]any,
	_ *state.AgentState,
) (<-chan tool.ToolChunk, error) {
	filename, err := normalizeSandboxPath(stringValue(input, "file_path"), workspace.workdir)
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	content := stringValue(input, "content")
	if err := workspace.backend.WriteFile(ctx, filename, []byte(content)); err != nil {
		return errorText("Error writing file: " + err.Error()), nil
	}
	return successText(fmt.Sprintf(
		"The file %s has been written successfully inside the remote workspace (%d lines).",
		filename,
		len(strings.Split(content, "\n")),
	)), nil
}

func executeEdit(
	ctx context.Context,
	workspace *Workspace,
	input map[string]any,
	_ *state.AgentState,
) (<-chan tool.ToolChunk, error) {
	filename, err := normalizeSandboxPath(stringValue(input, "file_path"), workspace.workdir)
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	oldString := stringValue(input, "old_string")
	newString := stringValue(input, "new_string")
	if oldString == newString {
		return errorText("Error: old_string and new_string are identical. No changes to make."), nil
	}
	contentBytes, err := workspace.backend.ReadFile(ctx, filename)
	if err != nil {
		return errorText("Error reading file: " + err.Error()), nil
	}
	content := string(contentBytes)
	occurrences := strings.Count(content, oldString)
	if occurrences == 0 {
		return errorText(fmt.Sprintf("Error: old_string not found in %s", filename)), nil
	}
	replaceAll := boolValue(input, "replace_all")
	if occurrences > 1 && !replaceAll {
		return errorText(fmt.Sprintf(
			"Error: old_string appears %d times in %s. Set replace_all=true or make old_string more specific.",
			occurrences,
			filename,
		)), nil
	}
	updated := strings.Replace(content, oldString, newString, 1)
	replacementMessage := "1 occurrence"
	if replaceAll {
		updated = strings.ReplaceAll(content, oldString, newString)
		replacementMessage = fmt.Sprintf("all %d occurrences", occurrences)
	}
	if err := workspace.backend.WriteFile(ctx, filename, []byte(updated)); err != nil {
		return errorText("Error writing file: " + err.Error()), nil
	}
	return successText(fmt.Sprintf(
		"Successfully replaced %s in %s inside the remote workspace.",
		replacementMessage,
		filename,
	)), nil
}

func executeGlob(
	ctx context.Context,
	workspace *Workspace,
	input map[string]any,
	_ *state.AgentState,
) (<-chan tool.ToolChunk, error) {
	pattern := strings.TrimSpace(stringValue(input, "pattern"))
	if pattern == "" {
		return errorText("Error: pattern is required"), nil
	}
	baseDir, err := searchBaseDir(input, workspace.workdir)
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	result, err := workspace.backend.Exec(ctx, []string{"find", baseDir, "-type", "f", "-print"}, ExecOptions{
		CWD:     workspace.workdir,
		Timeout: defaultBashTimeout,
	})
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	if !result.OK() {
		return errorText(strings.TrimSpace(string(result.Stdout) + string(result.Stderr))), nil
	}
	matches := filterGlobMatches(baseDir, string(result.Stdout), pattern)
	if len(matches) == 0 {
		return successText("No files found matching pattern: " + pattern), nil
	}
	sort.Strings(matches)
	return successText(strings.Join(matches, "\n")), nil
}

func executeGrep(
	ctx context.Context,
	workspace *Workspace,
	input map[string]any,
	_ *state.AgentState,
) (<-chan tool.ToolChunk, error) {
	pattern := stringValue(input, "pattern")
	if strings.TrimSpace(pattern) == "" {
		return errorText("Error: pattern is required"), nil
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return errorText("Error: invalid regex pattern: " + err.Error()), nil
	}
	searchPath, err := searchBaseDir(input, workspace.workdir)
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	argv := []string{"grep", "-R", "-n", "-E"}
	if boolValue(input, "case_insensitive") {
		argv = append(argv, "-i")
	}
	argv = append(argv, "--", pattern, searchPath)
	result, err := workspace.backend.Exec(ctx, argv, ExecOptions{
		CWD:     workspace.workdir,
		Timeout: defaultBashTimeout,
	})
	if err != nil {
		return errorText("Error: " + err.Error()), nil
	}
	if result.ExitCode == 1 {
		return successText("No matches found for pattern: " + pattern), nil
	}
	if !result.OK() {
		return errorText(strings.TrimSpace(string(result.Stdout) + string(result.Stderr))), nil
	}
	results := filterGrepOutput(
		string(result.Stdout),
		stringValue(input, "glob"),
		grepOutputMode(input),
	)
	results = limitStrings(results, intValue(input, "head_limit", 0))
	if len(results) == 0 {
		return successText("No matches found for pattern: " + pattern), nil
	}
	return successText(strings.Join(results, "\n")), nil
}

func searchBaseDir(input map[string]any, workdir string) (string, error) {
	value := strings.TrimSpace(stringValue(input, "path"))
	if value == "" {
		return workdir, nil
	}
	return normalizeSandboxPath(value, workdir)
}

func normalizeSandboxPath(value, workdir string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("file_path is required")
	}
	if strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("file_path contains NUL byte")
	}
	if strings.HasPrefix(value, "/") {
		return path.Clean(value), nil
	}
	root := path.Clean(workdir)
	joined := path.Join(root, filepath.ToSlash(value))
	if !insideRemoteDir(root, joined) {
		return "", fmt.Errorf("file_path escapes workspace root")
	}
	return joined, nil
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

func timeoutValue(input map[string]any, key string, fallback, maximum time.Duration) time.Duration {
	timeoutMS := intValue(input, key, int(fallback/time.Millisecond))
	if timeoutMS <= 0 {
		return fallback
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout > maximum {
		return maximum
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

func successText(text string) <-chan tool.ToolChunk {
	return singleTextChunk(text, message.ToolResultSuccess)
}

func errorText(text string) <-chan tool.ToolChunk {
	return singleTextChunk(text, message.ToolResultError)
}

func singleTextChunk(text string, resultState message.ToolResultState) <-chan tool.ToolChunk {
	chunks := make(chan tool.ToolChunk, 1)
	chunks <- *tool.NewToolChunk(
		message.ContentBlockList{message.NewTextBlock(text)},
		tool.WithToolChunkState(resultState),
	)
	close(chunks)
	return chunks
}

func filterGlobMatches(baseDir, stdout, pattern string) []string {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	matches := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		relative := strings.TrimPrefix(line, strings.TrimRight(baseDir, "/")+"/")
		if matchGlob(pattern, relative) {
			matches = append(matches, line)
		}
	}
	return matches
}

func matchGlob(pattern, value string) bool {
	pattern = filepath.ToSlash(pattern)
	value = filepath.ToSlash(value)
	if matched, err := filepath.Match(pattern, value); err == nil && matched {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		if matched, err := filepath.Match(strings.TrimPrefix(pattern, "**/"), filepath.Base(value)); err == nil && matched {
			return true
		}
	}
	if !strings.Contains(pattern, "**") {
		return false
	}
	expression := regexp.QuoteMeta(pattern)
	expression = strings.ReplaceAll(expression, `\*\*`, ".*")
	expression = strings.ReplaceAll(expression, `\*`, `[^/]*`)
	matched, _ := regexp.MatchString("^"+expression+"$", value)
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
		filename := line
		if colon := strings.Index(line, ":"); colon >= 0 {
			filename = line[:colon]
		}
		if globPattern != "" {
			matched, err := filepath.Match(globPattern, filepath.Base(filename))
			if err != nil || !matched {
				continue
			}
		}
		switch outputMode {
		case "files":
			if !seenFiles[filename] {
				seenFiles[filename] = true
				results = append(results, filename)
			}
		case "count":
			seenFiles[filename] = true
		default:
			results = append(results, line)
		}
	}
	if outputMode == "count" {
		for filename := range seenFiles {
			count := 0
			for _, line := range lines {
				if strings.HasPrefix(line, filename+":") {
					count++
				}
			}
			results = append(results, fmt.Sprintf("%s:%d", filename, count))
		}
		sort.Strings(results)
	}
	return results
}

func grepOutputMode(input map[string]any) string {
	mode := stringValue(input, "output_mode")
	if mode == "" {
		return "content"
	}
	return mode
}

func limitStrings(values []string, limit int) []string {
	if limit > 0 && limit < len(values) {
		return values[:limit]
	}
	return values
}

var _ tool.Tool = (*remoteTool)(nil)
