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

// Package types defines lightweight types shared across model, tool, and Agent boundaries.
package types

import "encoding/json"

// JSONPrimitive represents a primitive value directly expressible in JSON.
type JSONPrimitive = any

// JSONSerializableObject represents an object encodable by encoding/json.
type JSONSerializableObject = any

// Embedding represents an embedding vector.
type Embedding []float64

// IsJSONPrimitive reports whether a value is a JSON primitive.
func IsJSONPrimitive(value any) bool {
	switch value.(type) {
	case nil, string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, json.Number:
		return true
	default:
		return false
	}
}

// IsJSONSerializable reports whether a value can be safely encoded by encoding/json.
func IsJSONSerializable(value any) bool {
	_, err := json.Marshal(value)
	return err == nil
}
