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

package runtime

import (
	"fmt"
	"sync"

	agentpkg "github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/loop/core"
)

// Option configures the Loop Engineering Agent runtime.
type Option func(*options) error

type options struct {
	verifier   core.Verifier
	observer   core.Observer
	emitEvents bool
}

// WithVerifier sets the verifier used after a reply run finishes.
func WithVerifier(verifier core.Verifier) Option {
	return func(opts *options) error {
		if verifier == nil {
			return fmt.Errorf("loop: verifier is nil")
		}
		opts.verifier = verifier
		return nil
	}
}

// WithObserver sets an observer for loop run events.
func WithObserver(observer core.Observer) Option {
	return func(opts *options) error {
		if observer == nil {
			return fmt.Errorf("loop: observer is nil")
		}
		opts.observer = observer
		return nil
	}
}

// WithEventEmission controls whether loop custom events are emitted into the Agent stream.
func WithEventEmission(enabled bool) Option {
	return func(opts *options) error {
		opts.emitEvents = enabled
		return nil
	}
}

// Runtime attaches Loop Engineering behavior to one Agent run.
type Runtime struct {
	spec       core.Spec
	verifier   core.Verifier
	observer   core.Observer
	emitEvents bool

	mu     sync.Mutex
	hinted map[string]bool
}

// New creates a Loop Engineering runtime.
func New(spec core.Spec, opts ...Option) (*Runtime, error) {
	spec = core.NormalizeSpec(spec)
	if err := core.Validate(spec); err != nil {
		return nil, err
	}

	options := options{emitEvents: true}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&options); err != nil {
			return nil, err
		}
	}
	if spec.Mode == core.ModeUnattended && options.verifier == nil {
		return nil, fmt.Errorf("loop: unattended mode requires verifier")
	}

	return &Runtime{
		spec:       spec,
		verifier:   options.verifier,
		observer:   options.observer,
		emitEvents: options.emitEvents,
		hinted:     map[string]bool{},
	}, nil
}

// WithSpec installs the Loop Engineering runtime on an Agent.
func WithSpec(spec core.Spec, opts ...Option) agentpkg.AgentOption {
	return func(agent *agentpkg.Agent) error {
		runtime, err := New(spec, opts...)
		if err != nil {
			return err
		}
		return agentpkg.WithMiddlewares(runtime)(agent)
	}
}

// MiddlewareName returns the middleware name.
func (m *Runtime) MiddlewareName() string {
	if m == nil || m.spec.Name == "" {
		return "loop"
	}
	return "loop:" + m.spec.Name
}

var (
	_ agentpkg.ReplyMiddleware        = (*Runtime)(nil)
	_ agentpkg.ReasoningMiddleware    = (*Runtime)(nil)
	_ agentpkg.ActingMiddleware       = (*Runtime)(nil)
	_ agentpkg.ModelCallMiddleware    = (*Runtime)(nil)
	_ agentpkg.SystemPromptMiddleware = (*Runtime)(nil)
)
