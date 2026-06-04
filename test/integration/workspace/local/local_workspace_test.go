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

package localworkspaceintegration_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yuluo-yx/agentscope-go/test/integration/workspace/internal/workspacetest"
	wslocal "github.com/yuluo-yx/agentscope-go/workspace/local"
)

func TestLocalWorkspaceToolAndOffloadIntegration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workdir := t.TempDir()
	ws, err := wslocal.NewWorkspace(workdir, wslocal.WithWorkspaceID("local-integration"))
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	workspacetest.ExerciseToolsAndOffload(t, ctx, ws, filepath.Join(workdir, "data", "brief.txt"))
}
