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

package sandboxed

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var errGatewayOperationsClosing = errors.New("workspace/sandboxed: gateway operations are closing")

type internalGatewayOperationKey struct{}

type gatewayOperationLeaser interface {
	beginGatewayOperation(context.Context) (func(), error)
}

type operationGate struct {
	mu      sync.Mutex
	closing bool
	active  int
	drained chan struct{}
}

func (g *operationGate) begin(ctx context.Context) (func(), error) {
	if ctx == nil {
		return nil, fmt.Errorf("workspace/sandboxed: nil operation context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if internal, _ := ctx.Value(internalGatewayOperationKey{}).(bool); internal {
		return func() {}, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return nil, errGatewayOperationsClosing
	}
	g.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.active--
			if g.closing && g.active == 0 && g.drained != nil {
				close(g.drained)
				g.drained = nil
			}
		})
	}, nil
}

func (g *operationGate) closeAndWait(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("workspace/sandboxed: nil operation context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	if !g.closing {
		g.closing = true
		g.drained = make(chan struct{})
		if g.active == 0 {
			close(g.drained)
			g.drained = nil
			g.mu.Unlock()
			return nil
		}
	}
	drained := g.drained
	if drained == nil {
		g.mu.Unlock()
		return nil
	}
	g.mu.Unlock()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *operationGate) reopen() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closing = false
	g.drained = nil
}

func internalGatewayContext(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, internalGatewayOperationKey{}, true)
}

type gatewayBackend struct {
	Backend
	gate *operationGate
}

func (b *gatewayBackend) beginGatewayOperation(ctx context.Context) (func(), error) {
	if b == nil || b.gate == nil {
		return nil, fmt.Errorf("workspace/sandboxed: nil gateway operation gate")
	}
	return b.gate.begin(ctx)
}

func (b *gatewayBackend) ReadFileLimit(ctx context.Context, filename string, maxBytes int64) ([]byte, error) {
	reader, ok := b.Backend.(LimitedFileReader)
	if !ok {
		data, err := b.Backend.ReadFile(ctx, filename)
		if err != nil {
			return nil, err
		}
		if maxBytes <= 0 || int64(len(data)) > maxBytes {
			return nil, fmt.Errorf("workspace/sandboxed: remote file exceeds %d bytes", maxBytes)
		}
		return data, nil
	}
	return reader.ReadFileLimit(ctx, filename, maxBytes)
}

var _ LimitedFileReader = (*gatewayBackend)(nil)
