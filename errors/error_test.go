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

package errors_test

import (
	stderrors "errors"
	"strings"
	"testing"

	agenterrors "github.com/yuluo-yx/agentscope-go/errors"
)

func TestAgentAndDeveloperErrorsWrapCauses(t *testing.T) {
	t.Parallel()

	cause := stderrors.New("provider rejected request")
	agentErr := agenterrors.NewAgentError("model call failed", agenterrors.WithErrorCause(cause))

	if !strings.Contains(agentErr.Error(), "AgentError: model call failed") {
		t.Fatalf("agent error should include class-style prefix, got %q", agentErr.Error())
	}
	if !stderrors.Is(agentErr, cause) {
		t.Fatalf("agent error should unwrap cause")
	}

	devErr := agenterrors.NewDeveloperError("invalid tool schema", agenterrors.WithErrorCause(cause))
	if !strings.Contains(devErr.Error(), "DeveloperError: invalid tool schema") {
		t.Fatalf("developer error should include class-style prefix, got %q", devErr.Error())
	}
	if !stderrors.Is(devErr, cause) {
		t.Fatalf("developer error should unwrap cause")
	}
}

func TestToolErrorsAreAgentOrientedAndTyped(t *testing.T) {
	t.Parallel()

	err := agenterrors.NewToolNotFoundError("Read")

	var agentErr *agenterrors.AgentError
	if !stderrors.As(err, &agentErr) {
		t.Fatalf("tool error should expose AgentError through errors.As")
	}
	if err.ToolName != "Read" {
		t.Fatalf("tool name not preserved: %q", err.ToolName)
	}
	if got := err.Error(); !strings.Contains(got, "ToolNotFoundError") || !strings.Contains(got, "Read") {
		t.Fatalf("tool error should include type and tool name, got %q", got)
	}
}

func TestToolErrorVariantsExposeAgentError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "interrupted",
			err:  agenterrors.NewToolInterruptedError("Bash"),
			want: "ToolInterruptedError",
		},
		{
			name: "json decode",
			err:  agenterrors.NewToolJSONDecodeError("Read"),
			want: "ToolJSONDecodeError",
		},
		{
			name: "inactive group",
			err:  agenterrors.NewToolGroupInactiveError("Write", "edit"),
			want: "ToolGroupInactiveError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var agentErr *agenterrors.AgentError
			if !stderrors.As(tt.err, &agentErr) {
				t.Fatalf("tool error should expose AgentError: %T", tt.err)
			}
			if !strings.Contains(tt.err.Error(), tt.want) {
				t.Fatalf("error should include variant name %q, got %q", tt.want, tt.err.Error())
			}
		})
	}
}
