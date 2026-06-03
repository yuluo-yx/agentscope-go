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

package dockerworkspaceintegration_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/test/integration/workspace/internal/workspacetest"
	dockerworkspace "github.com/yuluo-yx/agentscope-go/workspace/docker"
)

func TestDockerWorkspaceToolAndOffloadIntegration(t *testing.T) {
	if os.Getenv("AGENTSCOPE_TEST_DOCKER") != "1" {
		t.Skip("set AGENTSCOPE_TEST_DOCKER=1 to run the Docker workspace integration test")
	}

	ctx := context.Background()
	workdir := t.TempDir()
	image := strings.TrimSpace(os.Getenv("AGENTSCOPE_DOCKER_IMAGE"))
	if image == "" {
		image = "ubuntu:latest"
	}
	ws, err := dockerworkspace.NewWorkspace(
		dockerworkspace.WithWorkspaceID("docker-integration"),
		dockerworkspace.WithImage(image),
		dockerworkspace.WithHostWorkdir(workdir),
		dockerworkspace.WithPullImage(false),
		dockerworkspace.WithNetworkDisabled(true),
	)
	if err != nil {
		t.Fatalf("NewWorkspace returned error: %v", err)
	}
	workspacetest.ExerciseToolsAndOffload(t, ctx, ws, "/workspace/data/brief.txt")
}
