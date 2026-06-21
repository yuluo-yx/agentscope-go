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

package template

import (
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/loop/automation/runner"
	"github.com/yuluo-yx/agentscope-go/loop/core"
	"github.com/yuluo-yx/agentscope-go/utils"
)

// LoopTemplate captures reusable loop configuration and project knowledge
// references without binding the automation package to a plugin format.
type LoopTemplate struct {
	Name        string
	Description string
	Spec        core.Spec
	SkillRefs   []SkillRef
	Mapper      string
	Metadata    map[string]any
}

// SkillRef declares project knowledge required or recommended by a loop
// template. Source is application-defined, for example a file path, URI, or
// plugin resource identifier.
type SkillRef struct {
	Name     string
	Version  string
	Required bool
	Source   string
}

// LoopTemplateConfig is the instantiated runtime configuration for a template.
type LoopTemplateConfig struct {
	TemplateName string
	Description  string
	Spec         core.Spec
	SkillRefs    []SkillRef
	Mapper       *runner.TemplateMapper
	Metadata     map[string]any
}

// Validate checks whether a template can be instantiated.
func (t LoopTemplate) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("automation: template name is empty")
	}
	if err := core.Validate(t.Spec); err != nil {
		return err
	}
	if strings.TrimSpace(t.Mapper) == "" {
		return fmt.Errorf("automation: template mapper is empty")
	}
	if _, err := runner.NewTemplateMapper(t.Mapper); err != nil {
		return fmt.Errorf("automation: template mapper is invalid: %w", err)
	}
	for i, ref := range t.SkillRefs {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("automation: skill ref %d: %w", i, err)
		}
	}
	return nil
}

// Instantiate validates the template and returns independent runtime values.
func (t LoopTemplate) Instantiate() (LoopTemplateConfig, error) {
	if err := t.Validate(); err != nil {
		return LoopTemplateConfig{}, err
	}
	mapper, err := runner.NewTemplateMapper(t.Mapper)
	if err != nil {
		return LoopTemplateConfig{}, err
	}
	return LoopTemplateConfig{
		TemplateName: t.Name,
		Description:  t.Description,
		Spec:         cloneSpec(t.Spec),
		SkillRefs:    cloneSkillRefs(t.SkillRefs),
		Mapper:       mapper,
		Metadata:     utils.CloneAnyMap(t.Metadata),
	}, nil
}

// Validate checks whether a skill reference has enough information to resolve
// project knowledge outside the automation package.
func (r SkillRef) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("skill name is empty")
	}
	if strings.TrimSpace(r.Source) == "" {
		return fmt.Errorf("skill source is empty")
	}
	return nil
}

func cloneSkillRefs(refs []SkillRef) []SkillRef {
	return append([]SkillRef(nil), refs...)
}

func cloneSpec(spec core.Spec) core.Spec {
	cp := spec
	cp.NonGoals = append([]string(nil), spec.NonGoals...)
	cp.SuccessCriteria = append([]core.SuccessCriterion(nil), spec.SuccessCriteria...)
	cp.Scope.Paths = append([]string(nil), spec.Scope.Paths...)
	cp.Scope.ToolNames = append([]string(nil), spec.Scope.ToolNames...)
	cp.Scope.TaskLabels = append([]string(nil), spec.Scope.TaskLabels...)
	cp.Scope.Metadata = utils.CloneAnyMap(spec.Scope.Metadata)
	cp.HumanGates = make([]core.HumanGate, 0, len(spec.HumanGates))
	for _, gate := range spec.HumanGates {
		gate.MatchPaths = append([]string(nil), gate.MatchPaths...)
		cp.HumanGates = append(cp.HumanGates, gate)
	}
	cp.Metadata = utils.CloneAnyMap(spec.Metadata)
	return cp
}
