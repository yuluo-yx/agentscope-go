package framework

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TestReport struct {
	Profile      string            `json:"profile"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	Duration     string            `json:"duration"`
	Status       string            `json:"status"`
	ExitCode     int               `json:"exit_code"`
	TotalTests   int               `json:"total_tests"`
	PassedTests  int               `json:"passed_tests"`
	FailedTests  int               `json:"failed_tests"`
	SkippedTests int               `json:"skipped_tests"`
	Environment  map[string]string `json:"environment,omitempty"`
	TestResults  []TestResult      `json:"test_results"`
}

type ReportGenerator struct {
	report    *TestReport
	startTime time.Time
}

func NewReportGenerator(profile string) *ReportGenerator {
	start := time.Now()
	return &ReportGenerator{
		startTime: start,
		report: &TestReport{
			Profile:     profile,
			StartTime:   start,
			Environment: map[string]string{},
		},
	}
}

func (g *ReportGenerator) SetEnvironment(key, value string) {
	g.report.Environment[key] = value
}

func (g *ReportGenerator) AddTestResults(results []TestResult) {
	g.report.TestResults = results
	g.report.TotalTests = len(results)
	for _, result := range results {
		if result.Skipped {
			g.report.SkippedTests++
		} else if result.Passed {
			g.report.PassedTests++
		} else {
			g.report.FailedTests++
		}
	}
}

func (g *ReportGenerator) Finalize(exitCode int) {
	g.report.EndTime = time.Now()
	g.report.Duration = g.report.EndTime.Sub(g.report.StartTime).Round(time.Millisecond).String()
	g.report.ExitCode = exitCode
	if exitCode == 0 {
		g.report.Status = "PASSED"
	} else {
		g.report.Status = "FAILED"
	}
}

func (g *ReportGenerator) WriteJSON(filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(g.report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0o644)
}

func (g *ReportGenerator) WriteMarkdown(filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filename, []byte(g.markdown()), 0o644)
}

func (g *ReportGenerator) markdown() string {
	r := g.report
	var b strings.Builder
	fmt.Fprintf(&b, "# AgentScope Go E2E Report - %s\n\n", r.Profile)
	fmt.Fprintf(&b, "- Status: `%s`\n", r.Status)
	fmt.Fprintf(&b, "- Duration: `%s`\n", r.Duration)
	fmt.Fprintf(&b, "- Exit Code: `%d`\n", r.ExitCode)
	fmt.Fprintf(&b, "- Total: `%d`, Passed: `%d`, Failed: `%d`, Skipped: `%d`\n\n", r.TotalTests, r.PassedTests, r.FailedTests, r.SkippedTests)
	if len(r.Environment) > 0 {
		b.WriteString("## Environment\n\n")
		for key, value := range r.Environment {
			fmt.Fprintf(&b, "- `%s`: `%s`\n", key, value)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Results\n\n")
	b.WriteString("| Test | Status | Duration | Error |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, result := range r.TestResults {
		status := "PASSED"
		if result.Skipped {
			status = "SKIPPED"
		} else if !result.Passed {
			status = "FAILED"
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | %s |\n", result.Name, status, result.Duration, escapeMarkdownCell(result.Error))
	}
	return b.String()
}

func escapeMarkdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "|", "\\|")
	if value == "" {
		return ""
	}
	return "`" + value + "`"
}
