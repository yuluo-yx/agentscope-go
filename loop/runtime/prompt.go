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

package runtime

import (
	"strings"

	"github.com/yuluo-yx/agentscope-go/loop/core"
)

func (m *Runtime) systemPrompt() string {
	var builder strings.Builder
	builder.WriteString("<loop_engineering>\n")
	builder.WriteString("Loop Engineering controls for this agent run:\n")
	builder.WriteString("- Loop name: ")
	builder.WriteString(m.spec.Name)
	builder.WriteString("\n- Mode: ")
	builder.WriteString(string(m.spec.Mode))
	builder.WriteString("\n- Goal: ")
	builder.WriteString(m.spec.Goal)
	builder.WriteString("\n")
	appendStringList(&builder, "Non-goals", m.spec.NonGoals)
	appendStringList(&builder, "Success criteria", successCriterionDescriptions(m.spec.SuccessCriteria))
	appendStringList(&builder, "Scope paths", m.spec.Scope.Paths)
	appendStringList(&builder, "Allowed/expected tools", m.spec.Scope.ToolNames)
	appendStringList(&builder, "Human gates", humanGateDescriptions(m.spec.HumanGates))
	builder.WriteString("If the goal is blocked, risky, or outside scope, stop and hand off with evidence.\n")
	builder.WriteString("</loop_engineering>")
	return builder.String()
}

func appendStringList(builder *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	builder.WriteString("- ")
	builder.WriteString(label)
	builder.WriteString(":\n")
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		builder.WriteString("  - ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
}

func successCriterionDescriptions(criteria []core.SuccessCriterion) []string {
	out := make([]string, 0, len(criteria))
	for _, criterion := range criteria {
		item := criterion.Name
		if criterion.Description != "" {
			if item != "" {
				item += ": "
			}
			item += criterion.Description
		}
		if criterion.Required {
			item += " (required)"
		}
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func humanGateDescriptions(gates []core.HumanGate) []string {
	out := make([]string, 0, len(gates))
	for _, gate := range gates {
		item := gate.Name
		if gate.Description != "" {
			if item != "" {
				item += ": "
			}
			item += gate.Description
		}
		if gate.Reason != "" {
			item += " Reason: " + gate.Reason
		}
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
