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

package template_test

import (
	"context"
	"strings"
	"testing"

	automationevent "github.com/yuluo-yx/agentscope-go/loop/automation/event"
	templatepkg "github.com/yuluo-yx/agentscope-go/loop/automation/template"
	"github.com/yuluo-yx/agentscope-go/loop/core"
)

func TestLoopTemplateValidateRejectsIncompleteTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template templatepkg.LoopTemplate
		contains string
	}{
		{
			name:     "empty name",
			template: templatepkg.LoopTemplate{Spec: validTemplateSpec(), Mapper: "handle {{.Type}}"},
			contains: "template name",
		},
		{
			name:     "invalid spec",
			template: templatepkg.LoopTemplate{Name: "daily-triage", Spec: core.Spec{Name: "daily-triage"}, Mapper: "handle {{.Type}}"},
			contains: "goal",
		},
		{
			name:     "empty mapper",
			template: templatepkg.LoopTemplate{Name: "daily-triage", Spec: validTemplateSpec()},
			contains: "mapper",
		},
		{
			name: "required skill missing name",
			template: templatepkg.LoopTemplate{
				Name:   "daily-triage",
				Spec:   validTemplateSpec(),
				Mapper: "handle {{.Type}}",
				SkillRefs: []templatepkg.SkillRef{
					{Required: true, Source: "file://skills/testing.md"},
				},
			},
			contains: "skill name",
		},
		{
			name: "skill missing source",
			template: templatepkg.LoopTemplate{
				Name:   "daily-triage",
				Spec:   validTemplateSpec(),
				Mapper: "handle {{.Type}}",
				SkillRefs: []templatepkg.SkillRef{
					{Name: "testing", Required: true},
				},
			},
			contains: "skill source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.template.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("Validate error = %v, want substring %q", err, tt.contains)
			}
		})
	}
}

func TestLoopTemplateInstantiateReturnsIndependentConfig(t *testing.T) {
	t.Parallel()

	template := templatepkg.LoopTemplate{
		Name:        "daily-triage",
		Description: "Scan repository signals.",
		Spec:        validTemplateSpec(),
		SkillRefs: []templatepkg.SkillRef{
			{Name: "testing", Version: "v1", Required: true, Source: "file://skills/testing.md"},
		},
		Mapper:   "Handle {{.Type}} for {{.Route.LoopName}}.",
		Metadata: map[string]any{"cadence": "daily"},
	}

	config, err := template.Instantiate()
	if err != nil {
		t.Fatalf("Instantiate returned error: %v", err)
	}
	if config.TemplateName != template.Name || config.Spec.Name != template.Spec.Name {
		t.Fatalf("config mismatch: %#v", config)
	}
	if len(config.SkillRefs) != 1 || config.SkillRefs[0].Name != "testing" {
		t.Fatalf("skill refs mismatch: %#v", config.SkillRefs)
	}
	if config.Metadata["cadence"] != "daily" {
		t.Fatalf("metadata mismatch: %#v", config.Metadata)
	}
	template.Spec.Scope.Paths[0] = "mutated"
	template.SkillRefs[0].Name = "mutated"
	template.Metadata["cadence"] = "mutated"
	if config.Spec.Scope.Paths[0] != "loop" || config.SkillRefs[0].Name != "testing" || config.Metadata["cadence"] != "daily" {
		t.Fatalf("Instantiate should return independent copies: %#v", config)
	}

	event := automationevent.Event{ID: "evt-1", Source: "schedule://daily", Type: automationevent.EventTypeScheduleTick}
	decision := automationevent.RouteDecision{LoopName: config.Spec.Name}
	message, err := config.Mapper.MapInput(context.Background(), event, decision)
	if err != nil {
		t.Fatalf("MapInput returned error: %v", err)
	}
	if got := *message.GetTextContent(""); !strings.Contains(got, automationevent.EventTypeScheduleTick) || !strings.Contains(got, config.Spec.Name) {
		t.Fatalf("mapped message = %q", got)
	}
}

func validTemplateSpec() core.Spec {
	return core.Spec{
		Name:   "daily-triage",
		Goal:   "Scan repository signals and report findings.",
		Mode:   core.ModeReportOnly,
		Policy: core.DefaultPolicy(core.ModeReportOnly),
		Scope:  core.Scope{Paths: []string{"loop"}},
	}
}
