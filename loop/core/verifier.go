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

package core

import (
	"context"

	"github.com/yuluo-yx/agentscope-go/state"
)

// Verifier checks whether a loop run met its success criteria.
type Verifier interface {
	Verify(context.Context, VerificationInput) (VerificationResult, error)
}

// VerifierFunc adapts a function into a Verifier.
type VerifierFunc func(context.Context, VerificationInput) (VerificationResult, error)

// Verify calls f(ctx, input).
func (f VerifierFunc) Verify(ctx context.Context, input VerificationInput) (VerificationResult, error) {
	if f == nil {
		return VerificationResult{}, nil
	}
	return f(ctx, input)
}

// VerificationInput is passed to a verifier after a reply run ends.
type VerificationInput struct {
	AgentName string
	SessionID string
	ReplyID   string
	Spec      Spec
	State     *state.AgentState
}

// VerificationResult is the verifier's decision.
type VerificationResult struct {
	Passed     bool
	Reason     string
	Evidence   []string
	NextAction string
}
