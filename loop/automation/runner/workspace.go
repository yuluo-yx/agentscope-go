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

package runner

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yuluo-yx/agentscope-go/loop/automation/event"
)

const (
	// RouteMetadataWorkspaceRoot is the RouteDecision metadata key for the
	// allocated workspace root.
	RouteMetadataWorkspaceRoot = "workspace_root"
	// RouteMetadataWorkspaceMetadata is the RouteDecision metadata key for the
	// allocated workspace metadata map.
	RouteMetadataWorkspaceMetadata = "workspace_metadata"
)

// WorkspaceAllocator allocates an isolated workspace for one automation run.
type WorkspaceAllocator interface {
	Allocate(context.Context, event.Event, event.RouteDecision) (WorkspaceLease, error)
}

// WorkspaceAllocatorFunc adapts a function to WorkspaceAllocator.
type WorkspaceAllocatorFunc func(context.Context, event.Event, event.RouteDecision) (WorkspaceLease, error)

// Allocate calls f(ctx, event, decision).
func (f WorkspaceAllocatorFunc) Allocate(ctx context.Context, event event.Event, decision event.RouteDecision) (WorkspaceLease, error) {
	if f == nil {
		return nil, fmt.Errorf("automation: workspace allocator is nil")
	}
	return f(ctx, event, decision)
}

// WorkspaceLease represents one allocated workspace.
type WorkspaceLease interface {
	Root() string
	Metadata() map[string]string
	Close(context.Context) error
}

// NoopWorkspaceAllocator returns the current process working directory, or the
// configured Root, without creating or cleaning up any external resources.
type NoopWorkspaceAllocator struct {
	Root     string
	Metadata map[string]string
}

// Allocate returns a static workspace lease.
func (a NoopWorkspaceAllocator) Allocate(ctx context.Context, _ event.Event, _ event.RouteDecision) (WorkspaceLease, error) {
	if ctx == nil {
		return nil, fmt.Errorf("automation: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := strings.TrimSpace(a.Root)
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("automation: get working directory: %w", err)
		}
		root = wd
	}
	return StaticWorkspaceLease{RootPath: root, Values: event.CloneStringMap(a.Metadata)}, nil
}

// StaticWorkspaceLease is a lease for an already-known workspace root.
type StaticWorkspaceLease struct {
	RootPath string
	Values   map[string]string
}

// Root returns the workspace root.
func (l StaticWorkspaceLease) Root() string {
	return l.RootPath
}

// Metadata returns a copy of workspace metadata.
func (l StaticWorkspaceLease) Metadata() map[string]string {
	return event.CloneStringMap(l.Values)
}

// Close releases the lease. Static leases have no external resource to close.
func (l StaticWorkspaceLease) Close(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("automation: context is nil")
	}
	return ctx.Err()
}
