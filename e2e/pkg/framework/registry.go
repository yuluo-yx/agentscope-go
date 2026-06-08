package framework

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type ProfileFactory func() Profile

var (
	profilesMu sync.RWMutex
	profiles   = map[string]ProfileFactory{}
)

func RegisterProfile(name string, factory ProfileFactory) {
	name = strings.TrimSpace(name)
	if name == "" {
		panic("agentscope e2e: profile name is empty")
	}
	if factory == nil {
		panic(fmt.Sprintf("agentscope e2e: profile %q has nil factory", name))
	}

	profilesMu.Lock()
	defer profilesMu.Unlock()
	if _, exists := profiles[name]; exists {
		panic(fmt.Sprintf("agentscope e2e: profile %q already registered", name))
	}
	profiles[name] = factory
}

func NewProfileByName(name string) (Profile, error) {
	profilesMu.RLock()
	defer profilesMu.RUnlock()
	factory, ok := profiles[name]
	if !ok {
		return nil, fmt.Errorf("agentscope e2e: profile %q not found; available profiles: %s", name, strings.Join(RegisteredProfileNames(), ", "))
	}
	return factory(), nil
}

func RegisteredProfileNames() []string {
	profilesMu.RLock()
	defer profilesMu.RUnlock()

	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
