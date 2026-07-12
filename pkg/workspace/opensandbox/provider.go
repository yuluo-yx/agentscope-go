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

package opensandbox

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	sdk "github.com/alibaba/OpenSandbox/sdks/sandbox/go"

	"github.com/yuluo-yx/agentscope-go/pkg/workspace/internal/sandboxed"
)

const providerHealthTimeout = 30 * time.Second

type provider struct {
	mu      sync.Mutex
	spec    sandboxSpec
	runtime sandboxRuntime
	handle  sandboxHandle
}

func (p *provider) Open(ctx context.Context) (sandboxed.Backend, error) {
	if p == nil || p.runtime == nil {
		return nil, fmt.Errorf("workspace/opensandbox: nil provider runtime")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle != nil {
		if err := p.waitHealthy(ctx, p.handle); err != nil {
			return nil, err
		}
		return &backend{handle: p.handle, workdir: defaultWorkdir}, nil
	}

	infos, err := p.runtime.List(ctx, p.spec.Connection, p.spec.ID)
	if err != nil {
		return nil, fmt.Errorf("workspace/opensandbox: list sandboxes: %w", err)
	}
	sort.SliceStable(infos, func(left, right int) bool {
		return infos[left].CreatedAt.After(infos[right].CreatedAt)
	})

	var handle sandboxHandle
	if len(infos) == 0 {
		handle, err = p.runtime.Create(ctx, p.spec)
	} else {
		selected := infos[0]
		switch selected.State {
		case sdk.StateRunning:
			handle, err = p.runtime.Connect(ctx, p.spec.Connection, selected.ID, p.spec.Timeout)
		case sdk.StatePaused:
			handle, err = p.runtime.Resume(ctx, p.spec.Connection, selected.ID, p.spec.Timeout)
		default:
			err = fmt.Errorf(
				"workspace/opensandbox: sandbox %q has non-attachable state %q",
				selected.ID,
				selected.State,
			)
		}
	}
	if err != nil {
		return nil, err
	}
	if handle == nil {
		return nil, fmt.Errorf("workspace/opensandbox: runtime returned nil sandbox")
	}
	p.handle = handle
	if err := p.waitHealthy(ctx, handle); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerHealthTimeout)
		defer cancel()
		closeErr := p.closeHandle(rollbackCtx)
		return nil, errors.Join(err, closeErr)
	}
	return &backend{handle: handle, workdir: defaultWorkdir}, nil
}

func (p *provider) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeHandle(ctx)
}

func (p *provider) SandboxID() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle == nil {
		return ""
	}
	return p.handle.ID()
}

func (p *provider) closeHandle(ctx context.Context) error {
	if p.handle == nil {
		return nil
	}
	handle := p.handle
	if err := handle.Pause(ctx); err != nil {
		return fmt.Errorf("workspace/opensandbox: pause sandbox %q: %w", handle.ID(), err)
	}
	p.handle = nil
	if err := handle.Close(); err != nil {
		return fmt.Errorf("workspace/opensandbox: close sandbox handle %q: %w", handle.ID(), err)
	}
	return nil
}

func (*provider) waitHealthy(ctx context.Context, handle sandboxHandle) error {
	healthCtx, cancel := context.WithTimeout(ctx, providerHealthTimeout)
	defer cancel()
	delay := 100 * time.Millisecond
	var lastErr error
	for {
		healthy, err := handle.Healthy(healthCtx)
		if err == nil && healthy {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		timer := time.NewTimer(delay)
		select {
		case <-healthCtx.Done():
			timer.Stop()
			if err := ctx.Err(); err != nil {
				return err
			}
			if lastErr == nil {
				return fmt.Errorf(
					"workspace/opensandbox: sandbox %q did not become healthy within %s",
					handle.ID(),
					providerHealthTimeout,
				)
			}
			return fmt.Errorf(
				"workspace/opensandbox: sandbox %q did not become healthy within %s: %w",
				handle.ID(),
				providerHealthTimeout,
				lastErr,
			)
		case <-timer.C:
		}
		if delay < time.Second {
			delay = min(delay*3/2, time.Second)
		}
	}
}

var _ sandboxed.Provider = (*provider)(nil)
