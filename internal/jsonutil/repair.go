// Copyright 20\d\d AgentScope Go
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

package jsonutil

import (
	"encoding/json"
	"fmt"
	"strings"
)

// LoadObject parses tool call arguments as a JSON object with minimal stream-repair support.
func LoadObject(input string) (map[string]any, error) {
	if obj, err := decodeObject(input); err == nil {
		return obj, nil
	}

	repaired := repairObject(input)
	obj, err := decodeObject(repaired)
	if err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}
	return obj, nil
}

func decodeObject(input string) (map[string]any, error) {
	var value any
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		return nil, err
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", value)
	}
	return obj, nil
}

func repairObject(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return s
	}

	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}

	if countUnescapedQuotes(s)%2 == 1 {
		s += `"`
	}
	s = strings.TrimRight(s, " \t\r\n,")

	openBraces := strings.Count(s, "{")
	closeBraces := strings.Count(s, "}")
	for i := closeBraces; i < openBraces; i++ {
		s += "}"
	}

	return s
}

func countUnescapedQuotes(input string) int {
	count := 0
	escaped := false
	for _, r := range input {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			count++
		}
	}
	return count
}
