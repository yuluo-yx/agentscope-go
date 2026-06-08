package testcases

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type TestCase struct {
	Name        string
	Description string
	Tags        []string
	Fn          func(context.Context, TestCaseOptions) error
}

type TestCaseOptions struct {
	Verbose    bool
	Profile    string
	Timeout    time.Duration
	WorkDir    string
	SetDetails func(map[string]any)
}

var (
	registryMu sync.RWMutex
	registry   = map[string]TestCase{}
)

func Register(name string, tc TestCase) {
	name = strings.TrimSpace(name)
	if name == "" {
		panic("agentscope e2e: test case name is empty")
	}
	if tc.Fn == nil {
		panic(fmt.Sprintf("agentscope e2e: test case %q has nil function", name))
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("agentscope e2e: test case %q already registered", name))
	}
	tc.Name = name
	registry[name] = tc
}

func Get(name string) (TestCase, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	tc, ok := registry[name]
	return tc, ok
}

func List() []TestCase {
	registryMu.RLock()
	defer registryMu.RUnlock()

	cases := make([]TestCase, 0, len(registry))
	for _, tc := range registry {
		cases = append(cases, tc)
	}
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].Name < cases[j].Name
	})
	return cases
}

func ListByNames(names ...string) ([]TestCase, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	cases := make([]TestCase, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		tc, ok := registry[name]
		if !ok {
			return nil, fmt.Errorf("agentscope e2e: test case %q not found", name)
		}
		cases = append(cases, tc)
	}
	return cases, nil
}
