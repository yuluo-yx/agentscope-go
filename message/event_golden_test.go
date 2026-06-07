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

package message_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/message"
)

func TestPythonEventGoldenFixtureRoundTrip(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("testdata", "python_event_roundtrip.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		t.Fatalf("fixture is not a JSON event array: %v", err)
	}

	seen := map[message.EventType]bool{}
	encoded := make([]json.RawMessage, 0, len(raws))
	for _, raw := range raws {
		event, err := message.UnmarshalEvent(raw)
		if err != nil {
			t.Fatalf("UnmarshalEvent(%s) returned error: %v", raw, err)
		}
		seen[event.GetType()] = true
		out, err := message.MarshalEvent(event)
		if err != nil {
			t.Fatalf("MarshalEvent(%s) returned error: %v", event.GetType(), err)
		}
		if _, err := message.UnmarshalEvent(out); err != nil {
			t.Fatalf("Go encoded event %s is not decodable: %v\njson: %s", event.GetType(), err, out)
		}
		encoded = append(encoded, out)
	}

	for _, typ := range []message.EventType{
		message.TextBlockDeltaType,
		message.ToolCallStartType,
		message.HintBlockType,
		message.CustomType,
		message.ModelCallEndType,
		message.ExceedMaxItersType,
		message.ToolResultEndType,
	} {
		if !seen[typ] {
			t.Fatalf("fixture did not cover %s", typ)
		}
	}

	assertPythonDecodesEquivalent(t, fixturePath, encoded)
}

func assertPythonDecodesEquivalent(t *testing.T, fixturePath string, encoded []json.RawMessage) {
	t.Helper()

	pythonSrc := filepath.Clean(filepath.Join("..", "..", "agentscope", "src"))
	if _, err := os.Stat(filepath.Join(pythonSrc, "agentscope", "event", "_event.py")); err != nil {
		t.Skipf("Python agentscope source not available at %s", pythonSrc)
	}
	python, err := pythonForGoldenFixture(pythonSrc)
	if err != nil {
		t.Skipf("%v; Go fixture round-trip already passed", err)
	}

	tmpDir := t.TempDir()
	goPath := filepath.Join(tmpDir, "go_events.json")
	payload, err := json.Marshal(encoded)
	if err != nil {
		t.Fatalf("Marshal encoded events returned error: %v", err)
	}
	if writeErr := os.WriteFile(goPath, payload, 0o600); writeErr != nil {
		t.Fatalf("WriteFile returned error: %v", writeErr)
	}

	const script = `
import json
import sys

from pydantic import TypeAdapter
from agentscope.event import AgentEvent

adapter = TypeAdapter(AgentEvent)

def load(path):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)

def norm(value):
    event = adapter.validate_python(value)
    return event.model_dump(mode="json", exclude_none=True)

original = [norm(item) for item in load(sys.argv[1])]
go_encoded = [norm(item) for item in load(sys.argv[2])]

if original != go_encoded:
    print(json.dumps({"original": original, "go_encoded": go_encoded}, ensure_ascii=False, indent=2))
    sys.exit(1)
`
	cmd := exec.Command(python, "-c", script, fixturePath, goPath)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+pythonSrc)
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := string(out)
		if strings.Contains(output, "ModuleNotFoundError") || strings.Contains(output, "ImportError") {
			t.Skipf("Python agentscope dependencies not importable: %s", output)
		}
		t.Fatalf("Python AgentEvent decode did not match Go round-trip: %v\n%s", err, output)
	}
}

func pythonForGoldenFixture(pythonSrc string) (string, error) {
	venvPython := filepath.Clean(filepath.Join(pythonSrc, "..", ".venv", "bin", "python"))
	if _, err := os.Stat(venvPython); err == nil {
		return venvPython, nil
	}
	return exec.LookPath("python3")
}
