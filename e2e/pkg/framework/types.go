package framework

import (
	"context"
	"time"
)

type Profile interface {
	Name() string
	Description() string
	Setup(context.Context, *SetupOptions) error
	Teardown(context.Context, *TeardownOptions) error
	GetTestCases() []string
}

type SetupOptions struct {
	Verbose bool
	WorkDir string
	Timeout time.Duration
}

type TeardownOptions struct {
	Verbose bool
	WorkDir string
}

type TestOptions struct {
	Profile   string
	Verbose   bool
	Parallel  bool
	TestCases []string
	ReportDir string
	Timeout   time.Duration
}

type TestResult struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Passed      bool           `json:"passed"`
	Skipped     bool           `json:"skipped,omitempty"`
	Error       string         `json:"error,omitempty"`
	Duration    string         `json:"duration"`
	Details     map[string]any `json:"details,omitempty"`
}
