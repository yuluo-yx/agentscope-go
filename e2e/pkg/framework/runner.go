package framework

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/yuluo-yx/agentscope-go/e2e/pkg/testcases"
)

type Runner struct {
	opts     *TestOptions
	profile  Profile
	reporter *ReportGenerator
}

func NewRunner(opts *TestOptions, profile Profile) *Runner {
	return &Runner{opts: opts, profile: profile}
}

func (r *Runner) Run(ctx context.Context) error {
	if r.opts.Timeout <= 0 {
		r.opts.Timeout = 5 * time.Minute
	}
	if r.opts.ReportDir == "" {
		r.opts.ReportDir = filepath.Join("e2e", "reports", r.profile.Name())
	}
	workDir, err := filepath.Abs(filepath.Join(r.opts.ReportDir, "_tmp"))
	if err != nil {
		return fmt.Errorf("resolve e2e work dir: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create e2e work dir: %w", err)
	}

	r.reporter = NewReportGenerator(r.profile.Name())
	r.reporter.SetEnvironment("go_version", runtime.Version())
	r.reporter.SetEnvironment("profile", r.profile.Name())
	r.reporter.SetEnvironment("parallel", fmt.Sprintf("%t", r.opts.Parallel))
	r.reporter.SetEnvironment("dashscope_api_key_set", fmt.Sprintf("%t", hasAnyEnv("DASHSCOPE_API_KEY", "AI_DASHSCOPE_API_KEY")))

	exitCode := 0
	defer func() {
		r.reporter.Finalize(exitCode)
		if err := r.reporter.WriteJSON(filepath.Join(r.opts.ReportDir, "test-report.json")); err != nil {
			r.log("failed to write json report: %v", err)
		}
		if err := r.reporter.WriteMarkdown(filepath.Join(r.opts.ReportDir, "test-report.md")); err != nil {
			r.log("failed to write markdown report: %v", err)
		}
	}()

	r.log("starting profile %s: %s", r.profile.Name(), r.profile.Description())
	if err := r.profile.Setup(ctx, &SetupOptions{Verbose: r.opts.Verbose, WorkDir: workDir, Timeout: r.opts.Timeout}); err != nil {
		exitCode = 1
		return fmt.Errorf("setup profile %s: %w", r.profile.Name(), err)
	}
	defer func() {
		if err := r.profile.Teardown(context.Background(), &TeardownOptions{Verbose: r.opts.Verbose, WorkDir: workDir}); err != nil {
			r.log("teardown profile %s failed: %v", r.profile.Name(), err)
		}
	}()

	cases, err := r.selectTestCases()
	if err != nil {
		exitCode = 1
		return err
	}
	results := r.runTests(ctx, cases, workDir)
	r.reporter.AddTestResults(results)
	r.printResults(results)
	for _, result := range results {
		if !result.Passed && !result.Skipped {
			exitCode = 1
			return fmt.Errorf("some e2e tests failed")
		}
	}
	return nil
}

func (r *Runner) selectTestCases() ([]testcases.TestCase, error) {
	if r.opts.Verbose {
		for _, tc := range testcases.List() {
			r.log("registered test: %s - %s", tc.Name, tc.Description)
		}
	}
	if len(r.opts.TestCases) > 0 {
		return testcases.ListByNames(r.opts.TestCases...)
	}
	return testcases.ListByNames(r.profile.GetTestCases()...)
}

func (r *Runner) runTests(ctx context.Context, cases []testcases.TestCase, workDir string) []TestResult {
	results := make([]TestResult, 0, len(cases))
	var mu sync.Mutex
	runOne := func(tc testcases.TestCase) {
		result := r.runSingleTest(ctx, tc, workDir)
		mu.Lock()
		results = append(results, result)
		mu.Unlock()
	}

	if r.opts.Parallel {
		var wg sync.WaitGroup
		for _, tc := range cases {
			wg.Add(1)
			go func(tc testcases.TestCase) {
				defer wg.Done()
				runOne(tc)
			}(tc)
		}
		wg.Wait()
		return results
	}
	for _, tc := range cases {
		runOne(tc)
	}
	return results
}

func (r *Runner) runSingleTest(ctx context.Context, tc testcases.TestCase, workDir string) TestResult {
	r.log("running test %s", tc.Name)
	start := time.Now()
	details := map[string]any{}
	testCtx, cancel := context.WithTimeout(ctx, r.opts.Timeout)
	defer cancel()
	testWorkDir := filepath.Join(workDir, tc.Name)
	result := TestResult{
		Name:        tc.Name,
		Description: tc.Description,
		Tags:        append([]string(nil), tc.Tags...),
		Details:     details,
	}
	err := os.MkdirAll(testWorkDir, 0o755)
	if err == nil {
		err = tc.Fn(testCtx, testcases.TestCaseOptions{
			Verbose: r.opts.Verbose,
			Profile: r.profile.Name(),
			Timeout: r.opts.Timeout,
			WorkDir: testWorkDir,
			SetDetails: func(value map[string]any) {
				details = value
				result.Details = value
			},
		})
	}
	result.Duration = time.Since(start).Round(time.Millisecond).String()
	result.Passed = err == nil
	if isSkipError(err) {
		result.Skipped = true
		result.Passed = false
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func (r *Runner) printResults(results []TestResult) {
	passed := 0
	skipped := 0
	for _, result := range results {
		if result.Skipped {
			skipped++
		} else if result.Passed {
			passed++
		}
	}
	fmt.Printf("E2E profile %s: total=%d passed=%d failed=%d skipped=%d\n", r.profile.Name(), len(results), passed, len(results)-passed-skipped, skipped)
	for _, result := range results {
		status := "PASS"
		if result.Skipped {
			status = "SKIP"
		} else if !result.Passed {
			status = "FAIL"
		}
		fmt.Printf("%s %s (%s)\n", status, result.Name, result.Duration)
		if result.Error != "" {
			fmt.Printf("  %s\n", result.Error)
		}
	}
}

func isSkipError(err error) bool {
	var skip *SkipError
	return errors.As(err, &skip)
}

func (r *Runner) log(format string, args ...any) {
	if r.opts.Verbose {
		fmt.Printf("[e2e] "+format+"\n", args...)
	}
}

func hasAnyEnv(names ...string) bool {
	for _, name := range names {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}
