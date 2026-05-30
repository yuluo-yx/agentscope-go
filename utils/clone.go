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

// Package utils provides lightweight helpers shared across packages.
package utils

// CloneAnyMap deep-copies map[string]any.
func CloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = CloneAny(value)
	}
	return out
}

// CloneAny deep-copies JSON-style dynamic values used by the framework.
func CloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return CloneAnyMap(typed)
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = CloneAny(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}
