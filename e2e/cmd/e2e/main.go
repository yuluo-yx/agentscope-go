package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yuluo-yx/agentscope-go/e2e/pkg/framework"
	_ "github.com/yuluo-yx/agentscope-go/e2e/profiles/all"
)

const version = "v1.0.0"

func main() {
	profile := flag.String("profile", "local", fmt.Sprintf("test profile to run (%s)", strings.Join(framework.RegisteredProfileNames(), ", ")))
	tests := flag.String("tests", "", "comma-separated test cases to run")
	verbose := flag.Bool("verbose", true, "enable verbose logging")
	parallel := flag.Bool("parallel", false, "run test cases in parallel")
	reportDir := flag.String("report-dir", "", "directory for test-report.json and test-report.md")
	timeout := flag.Duration("timeout", 5*time.Minute, "timeout per test case")
	showVersion := flag.Bool("version", false, "print runner version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	testCases := splitCSV(*tests)
	profileImpl, err := framework.NewProfileByName(*profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	opts := &framework.TestOptions{
		Profile:   *profile,
		Verbose:   *verbose,
		Parallel:  *parallel,
		TestCases: testCases,
		ReportDir: *reportDir,
		Timeout:   *timeout,
	}
	if opts.ReportDir == "" {
		opts.ReportDir = "reports/" + profileImpl.Name()
	}
	runner := framework.NewRunner(opts, profileImpl)
	if err := runner.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
