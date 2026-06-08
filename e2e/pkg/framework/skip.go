package framework

import (
	"fmt"
	"strings"
)

// SkipError marks an E2E case as intentionally skipped instead of failed.
type SkipError struct {
	Reason string
}

func (e *SkipError) Error() string {
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		return "skipped"
	}
	return "skipped: " + reason
}

func Skip(reason string) error {
	return &SkipError{Reason: reason}
}

func Skipf(format string, args ...any) error {
	return Skip(fmt.Sprintf(format, args...))
}
