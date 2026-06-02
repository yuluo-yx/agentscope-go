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

package utils_test

import (
	"encoding/hex"
	"testing"

	"github.com/yuluo-yx/agentscope-go/utils"
)

func TestNewIDUsesPythonCompatibleHexUUID(t *testing.T) {
	t.Parallel()

	id := utils.NewID()
	if len(id) != 32 {
		t.Fatalf("id should be 32 hex characters, got %q", id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("id should be hex encoded: %v", err)
	}
}
