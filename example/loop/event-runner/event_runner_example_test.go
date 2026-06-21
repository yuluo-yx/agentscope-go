// Copyright The AgentScope Go Authors
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

package main

import (
	"context"
	"strings"
	"testing"

	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

func TestEventRunnerExampleRecordsLoopRun(t *testing.T) {
	t.Parallel()

	if err := run(context.Background()); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
}

func TestEventRunnerScriptedModelReportsMissingResponse(t *testing.T) {
	t.Parallel()

	scripted := &scriptedChatModel{}

	_, err := scripted.Call(context.Background(), asmodel.CallRequest{})
	if err == nil || !strings.Contains(err.Error(), "no response") {
		t.Fatalf("Call error = %v, want missing response error", err)
	}
}
