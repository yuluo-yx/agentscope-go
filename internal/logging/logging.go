// Copyright 20\d\d AgentScope Go
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

package logging

import (
	"log/slog"
	"os"
	"sync"
)

var (
	mu            sync.RWMutex
	defaultLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{}))
)

// Logger returns the structured logger used internally by AgentScope-Go.
func Logger() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return defaultLogger
}

// SetDefault replaces the package logger and returns a restore function for tests.
func SetDefault(logger *slog.Logger) func() {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{}))
	}

	mu.Lock()
	previous := defaultLogger
	defaultLogger = logger
	mu.Unlock()

	return func() {
		mu.Lock()
		defaultLogger = previous
		mu.Unlock()
	}
}
