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
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
	"github.com/yuluo-yx/agentscope-go/utils"
)

const (
	sourceSuggested = "suggested"
	defaultFileMode = 0o644
)

type baseTool struct {
	name            string
	description     string
	schema          map[string]any
	concurrencySafe bool
	readOnly        bool
	stateInjected   bool
}

// Name returns the tool name.
func (b baseTool) Name() string {
	return b.name
}

// Description returns the tool description.
func (b baseTool) Description() string {
	return b.description
}

// InputSchema returns the tool input JSON Schema.
func (b baseTool) InputSchema() map[string]any {
	return utils.CloneAnyMap(b.schema)
}

// IsConcurrencySafe reports whether the tool can run concurrently.
func (b baseTool) IsConcurrencySafe() bool {
	return b.concurrencySafe
}

// IsReadOnly reports whether the tool only reads external state.
func (b baseTool) IsReadOnly() bool {
	return b.readOnly
}

// IsExternalTool reports whether the tool runs in an external system.
func (b baseTool) IsExternalTool() bool {
	return false
}

// IsStateInjected reports whether tool execution requires AgentState.
func (b baseTool) IsStateInjected() bool {
	return b.stateInjected
}

// IsMCP reports whether the tool comes from an MCP service.
func (b baseTool) IsMCP() bool {
	return false
}

// MCPName returns the MCP service name.
func (b baseTool) MCPName() string {
	return ""
}

// CheckPermissions returns the built-in base permission decision.
func (b baseTool) CheckPermissions(context.Context, map[string]any, *permission.Context) (*permission.Decision, error) {
	if b.readOnly {
		return &permission.Decision{
			Behavior:       permission.BehaviorAllow,
			Message:        fmt.Sprintf("%s is read-only.", b.name),
			DecisionReason: "Read-only tool is allowed",
		}, nil
	}
	return &permission.Decision{Behavior: permission.BehaviorPassthrough}, nil
}

// MatchRule matches a permission rule.
func (b baseTool) MatchRule(ruleContent string, _ map[string]any) bool {
	return ruleContent == ""
}

// GenerateSuggestions generates suggested permission rules.
func (b baseTool) GenerateSuggestions(map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName: b.name,
		Behavior: permission.BehaviorAllow,
		Source:   sourceSuggested,
	}}
}

func singleTextChunk(text string, state message.ToolResultState) <-chan astool.ToolChunk {
	chunks := make(chan astool.ToolChunk, 1)
	chunks <- *astool.NewToolChunk(
		message.ContentBlockList{message.NewTextBlock(text)},
		astool.WithToolChunkState(state),
	)
	close(chunks)
	return chunks
}

func successText(text string) <-chan astool.ToolChunk {
	return singleTextChunk(text, message.ToolResultSuccess)
}

func errorText(text string) <-chan astool.ToolChunk {
	return singleTextChunk(text, message.ToolResultError)
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
	if parsed, ok := signedIntValue(value); ok {
		return parsed
	}
	if parsed, ok := unsignedIntValue(value); ok {
		return parsed
	}
	if parsed, ok := floatIntValue(value); ok {
		return parsed
	}
	if typed, ok := value.(string); ok {
		parsed, err := strconv.Atoi(typed)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func signedIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	default:
		return 0, false
	}
}

func unsignedIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case uint:
		if typed > math.MaxInt {
			return 0, false
		}
		return int(typed), true
	case uint8:
		return int(typed), true
	case uint16:
		return int(typed), true
	case uint32:
		if uint64(typed) > uint64(math.MaxInt) {
			return 0, false
		}
		return int(typed), true
	case uint64:
		if typed > uint64(math.MaxInt) {
			return 0, false
		}
		return int(typed), true
	default:
		return 0, false
	}
}

func floatIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func absolutePath(filePath string) (string, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}
	if strings.HasPrefix(filePath, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if filePath == "~" {
				filePath = home
			} else if strings.HasPrefix(filePath, "~/") {
				filePath = filepath.Join(home, strings.TrimPrefix(filePath, "~/"))
			}
		}
	}
	if !filepath.IsAbs(filePath) {
		return "", fmt.Errorf("file_path must be an absolute path, got: %s", filePath)
	}
	return filepath.Clean(filePath), nil
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

func readFileLines(filePath string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return splitLinesPreserve(string(content)), nil
}

func cachedOrDiskLines(filePath string, state *astate.AgentState) ([]string, bool, error) {
	if state != nil && state.ToolContext != nil {
		if cache, ok := state.ToolContext.GetCache(filePath); ok {
			return append([]string(nil), cache.Lines...), true, nil
		}
	}
	lines, err := readFileLines(filePath)
	return lines, false, err
}

func cacheFile(state *astate.AgentState, filePath string, lines []string) {
	if state == nil {
		return
	}
	if state.ToolContext == nil {
		state.ToolContext = astate.NewToolContext()
	}
	_ = state.ToolContext.CacheFile(filePath, lines)
}

func fileMatchRule(ruleContent string, input map[string]any) bool {
	filePath := stringValue(input, "file_path")
	if filePath == "" {
		return false
	}
	return permission.MatchPattern(ruleContent, filePath)
}

func fileSuggestions(toolName string, input map[string]any) []permission.Rule {
	filePath := stringValue(input, "file_path")
	pattern := "**"
	if filePath != "" {
		parent := filepath.Dir(filePath)
		if parent != "." && parent != "" {
			pattern = filepath.ToSlash(filepath.Clean(parent)) + "/**"
		}
	}
	return []permission.Rule{{
		ToolName:    toolName,
		RuleContent: pattern,
		Behavior:    permission.BehaviorAllow,
		Source:      sourceSuggested,
	}}
}

func isDangerousPath(filePath string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(filePath))
	lower := strings.ToLower(cleaned)
	base := strings.ToLower(filepath.Base(lower))
	dangerousFiles := map[string]bool{
		".gitconfig":             true,
		".gitmodules":            true,
		".bashrc":                true,
		".bash_profile":          true,
		".zshrc":                 true,
		".zprofile":              true,
		".profile":               true,
		".netrc":                 true,
		".npmrc":                 true,
		".pypirc":                true,
		".env":                   true,
		".envrc":                 true,
		".env.local":             true,
		".env.development":       true,
		".env.development.local": true,
		".env.test":              true,
		".env.test.local":        true,
		".env.staging":           true,
		".env.production":        true,
		".env.production.local":  true,
		"config":                 strings.Contains(lower, "/.ssh/config"),
		"authorized_keys":        strings.Contains(lower, "/.ssh/authorized_keys"),
	}
	if dangerousFiles[base] {
		return true
	}
	for _, segment := range strings.Split(lower, "/") {
		switch segment {
		case ".git", ".vscode", ".idea", ".ssh":
			return true
		}
	}
	return false
}

func pathInAllowedWorkingDir(filePath string, ctx *permission.Context) bool {
	if ctx == nil {
		return false
	}
	candidate := filepath.Clean(filePath)
	for _, workingDir := range ctx.WorkingDirectories {
		if workingDir.Path == "" {
			continue
		}
		base := filepath.Clean(workingDir.Path)
		rel, err := filepath.Rel(base, candidate)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
			return true
		}
	}
	return false
}

func writableFilePermission(toolName, action string, input map[string]any, ctx *permission.Context) (*permission.Decision, error) {
	filePath := stringValue(input, "file_path")
	if filePath == "" {
		return &permission.Decision{
			Behavior:       permission.BehaviorAsk,
			Message:        fmt.Sprintf("Permission required for %s", toolName),
			DecisionReason: "Missing file_path",
		}, nil
	}
	if isDangerousPath(filePath) {
		return &permission.Decision{
			Behavior:       permission.BehaviorAsk,
			Message:        fmt.Sprintf("Permission required: %s operates on sensitive file %s", toolName, filePath),
			DecisionReason: "Safety check: dangerous file path",
		}, nil
	}
	if ctx != nil && ctx.Mode == permission.ModeAcceptEdits && pathInAllowedWorkingDir(filePath, ctx) {
		return &permission.Decision{
			Behavior:       permission.BehaviorAllow,
			Message:        fmt.Sprintf("Permission granted for %s %s (accept edits mode)", action, filePath),
			DecisionReason: "Accept edits mode allows paths in working directories",
		}, nil
	}
	return &permission.Decision{
		Behavior:       permission.BehaviorAsk,
		Message:        fmt.Sprintf("Permission required to %s %s", action, filePath),
		DecisionReason: "Write-capable tools require user approval",
	}, nil
}

func globMatch(pattern, value string) bool {
	pattern = normalizeGlobSeparators(pattern)
	value = normalizeGlobSeparators(value)
	if pattern == "" {
		return true
	}
	if ok, err := filepath.Match(pattern, value); err == nil && ok {
		return true
	}
	if !strings.Contains(pattern, "/") {
		if ok, err := filepath.Match(pattern, filepath.Base(value)); err == nil && ok {
			return true
		}
	}
	regex, err := regexp.Compile("^" + globToRegexp(pattern) + "$")
	if err != nil {
		return false
	}
	return regex.MatchString(value)
}

func normalizeGlobSeparators(value string) string {
	return strings.ReplaceAll(filepath.ToSlash(value), "\\", "/")
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		if ch == '*' {
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
				continue
			}
			b.WriteString("[^/]*")
			continue
		}
		if ch == '?' {
			b.WriteString("[^/]")
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(ch)))
	}
	return b.String()
}
