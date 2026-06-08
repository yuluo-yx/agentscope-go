package docker

import (
	"context"
	"fmt"
	"os"

	"github.com/yuluo-yx/agentscope-go/e2e/pkg/framework"
)

type Profile struct{}

func init() {
	framework.RegisterProfile("docker", func() framework.Profile { return &Profile{} })
}

func (*Profile) Name() string { return "docker" }

func (*Profile) Description() string {
	return "Docker workspace E2E suite"
}

func (*Profile) Setup(context.Context, *framework.SetupOptions) error {
	if os.Getenv("AGENTSCOPE_E2E_DOCKER") != "1" {
		return fmt.Errorf("set AGENTSCOPE_E2E_DOCKER=1 to run docker profile")
	}
	return nil
}

func (*Profile) Teardown(context.Context, *framework.TeardownOptions) error { return nil }

func (*Profile) GetTestCases() []string {
	return []string{"workspace-docker-agent-loop"}
}
