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

package permission

import (
	"path/filepath"
	"strings"
)

// MatchPattern implements Bash prefix, file glob, and plain substring matching.
func MatchPattern(pattern, value string) bool {
	if pattern == "" {
		return true
	}

	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, ":*")
		return value == prefix || strings.HasPrefix(value, prefix+" ")
	}

	if ok, err := filepath.Match(pattern, value); err == nil && ok {
		return true
	}
	if strings.Contains(pattern, "**") {
		needle := strings.TrimSuffix(pattern, "**")
		if strings.HasPrefix(value, needle) {
			return true
		}
	}
	if strings.ContainsAny(pattern, "*?[") {
		return false
	}
	return strings.Contains(value, pattern)
}
