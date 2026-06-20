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

package microsandbox

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

func cleanSandboxPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func normalizeSandboxPath(path, workdir string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("file_path is required")
	}
	if strings.HasPrefix(path, "/") {
		return cleanSandboxPath(path), nil
	}
	root := cleanSandboxPath(workdir)
	if root == "" {
		root = defaultContainerWorkdir
	}
	joined := cleanSandboxPath(filepath.ToSlash(filepath.Join(root, path)))
	if joined != root && !strings.HasPrefix(joined, strings.TrimRight(root, "/")+"/") {
		return "", fmt.Errorf("file_path escapes workspace root")
	}
	return joined, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneMCPClientConfig(config asworkspace.MCPClientConfig) asworkspace.MCPClientConfig {
	out := config
	if config.Stdio != nil {
		stdio := *config.Stdio
		stdio.Args = append([]string(nil), config.Stdio.Args...)
		stdio.Env = cloneStringMap(config.Stdio.Env)
		out.Stdio = &stdio
	}
	if config.HTTP != nil {
		http := *config.HTTP
		http.Headers = cloneStringMap(config.HTTP.Headers)
		out.HTTP = &http
	}
	out.EnabledTools = append([]string(nil), config.EnabledTools...)
	out.DisabledTools = append([]string(nil), config.DisabledTools...)
	return out
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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
	timeout := fallback
	switch value := input[key].(type) {
	case int:
		timeout = time.Duration(value) * time.Millisecond
	case int64:
		timeout = time.Duration(value) * time.Millisecond
	case float64:
		timeout = time.Duration(value) * time.Millisecond
	case jsonNumber:
		if parsed, err := value.Int64(); err == nil {
			timeout = time.Duration(parsed) * time.Millisecond
		}
	}
	if timeout <= 0 {
		return fallback
	}
	if timeout > max {
		return max
	}
	return timeout
}

type jsonNumber interface {
	Int64() (int64, error)
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
