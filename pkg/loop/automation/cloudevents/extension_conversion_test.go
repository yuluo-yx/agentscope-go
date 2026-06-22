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

package cloudevents

import (
	"testing"

	cesdk "github.com/cloudevents/sdk-go/v2"
)

func TestExtensionStringCoversSupportedValueTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "string", value: "direct", want: "direct"},
		{name: "stringer", value: stringerValue("from-stringer"), want: "from-stringer"},
		{name: "bytes", value: []byte("from-bytes"), want: "from-bytes"},
		{name: "int", value: 12, want: "12"},
		{name: "int32", value: int32(13), want: "13"},
		{name: "int64", value: int64(14), want: "14"},
		{name: "whole float64", value: float64(15), want: "15"},
		{name: "fractional float64", value: 15.5, want: "15.5"},
		{name: "unsupported", value: struct{}{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extensionString(tt.value); got != tt.want {
				t.Fatalf("extensionString(%T) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestExtensionIntCoversSupportedValueTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  int
	}{
		{name: "int", value: 12, want: 12},
		{name: "int32", value: int32(13), want: 13},
		{name: "int64", value: int64(14), want: 14},
		{name: "float64", value: float64(15.9), want: 15},
		{name: "string", value: " 16 ", want: 16},
		{name: "bad string", value: "bad", want: 0},
		{name: "unsupported", value: struct{}{}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extensionInt(tt.value); got != tt.want {
				t.Fatalf("extensionInt(%T) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestSetStringExtensionSkipsBlankValues(t *testing.T) {
	t.Parallel()

	event := cesdk.NewEvent()
	setStringExtension(&event, "blank", "   ")
	setStringExtension(&event, "present", " value ")

	extensions := event.Extensions()
	if _, ok := extensions["blank"]; ok {
		t.Fatalf("blank extension should be skipped: %#v", extensions)
	}
	if extensions["present"] != "value" {
		t.Fatalf("present extension = %#v, want trimmed value", extensions["present"])
	}
}

type stringerValue string

func (v stringerValue) String() string {
	return string(v)
}
