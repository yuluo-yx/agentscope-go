package framework_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuluo-yx/agentscope-go/e2e/pkg/framework"
	"github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
)

func TestRunnerTreatsSkipAsNonFailingResult(t *testing.T) {
	testcases.Register("framework-skip-sentinel", testcases.TestCase{
		Description: "sentinel skipped test",
		Fn: func(context.Context, testcases.TestCaseOptions) error {
			return framework.Skip("missing provider key")
		},
	})

	reportDir := filepath.Join(t.TempDir(), "reports")
	runner := framework.NewRunner(
		&framework.TestOptions{
			Profile:   "skip-test",
			ReportDir: reportDir,
			Timeout:   time.Second,
		},
		skipProfile{},
	)
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Runner should not fail on skipped test: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(reportDir, "test-report.json"))
	if err != nil {
		t.Fatalf("read json report: %v", err)
	}
	var report struct {
		Status       string `json:"status"`
		ExitCode     int    `json:"exit_code"`
		PassedTests  int    `json:"passed_tests"`
		FailedTests  int    `json:"failed_tests"`
		SkippedTests int    `json:"skipped_tests"`
		TestResults  []struct {
			Passed  bool   `json:"passed"`
			Skipped bool   `json:"skipped"`
			Error   string `json:"error"`
		} `json:"test_results"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode json report: %v", err)
	}
	if report.Status != "PASSED" || report.ExitCode != 0 || report.PassedTests != 0 || report.FailedTests != 0 || report.SkippedTests != 1 {
		t.Fatalf("unexpected skip report: %#v", report)
	}
	if len(report.TestResults) != 1 || report.TestResults[0].Passed || !report.TestResults[0].Skipped ||
		!strings.Contains(report.TestResults[0].Error, "missing provider key") {
		t.Fatalf("unexpected skipped result: %#v", report.TestResults)
	}
}

type skipProfile struct{}

func (skipProfile) Name() string { return "skip-test" }

func (skipProfile) Description() string { return "skip profile" }

func (skipProfile) Setup(context.Context, *framework.SetupOptions) error { return nil }

func (skipProfile) Teardown(context.Context, *framework.TeardownOptions) error { return nil }

func (skipProfile) GetTestCases() []string { return []string{"framework-skip-sentinel"} }
