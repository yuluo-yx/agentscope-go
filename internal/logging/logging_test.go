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

package logging_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/internal/logging"
)

func TestSetDefaultReplacesPackageLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	restore := logging.SetDefault(logger)
	defer restore()

	logging.Logger().DebugContext(context.Background(), "permission checked", slog.String("tool", "Read"))

	got := buf.String()
	if !strings.Contains(got, "permission checked") || !strings.Contains(got, "tool=Read") {
		t.Fatalf("logger output missing expected fields: %q", got)
	}
}
