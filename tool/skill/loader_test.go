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

package skill_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yuluo-yx/agentscope-go/tool/skill"
)

func TestLocalLoaderLoadsSkillMarkdownAndFrontMatter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, root, "code-review", "Review code carefully", "Use a checklist.\n")

	loader := skill.NewLocalLoader(root)
	skills, err := loader.ListSkills(context.Background())
	if err != nil {
		t.Fatalf("ListSkills returned error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected one skill, got %d %#v", len(skills), skills)
	}
	got := skills[0]
	if got.Name != "code-review" || got.Description != "Review code carefully" {
		t.Fatalf("front matter not parsed: %#v", got)
	}
	if got.Dir != root || got.Markdown != "Use a checklist.\n" {
		t.Fatalf("skill body metadata mismatch: %#v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be set from SKILL.md mtime")
	}
}

func TestLocalLoaderScansSubdirectoriesWhenEnabled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "one"), "one", "First skill", "first\n")
	writeSkill(t, filepath.Join(root, "two"), "two", "Second skill", "second\n")

	withoutSubdirs, err := skill.NewLocalLoader(root).ListSkills(context.Background())
	if err != nil {
		t.Fatalf("ListSkills without subdirs returned error: %v", err)
	}
	if len(withoutSubdirs) != 0 {
		t.Fatalf("loader should not scan subdirectories by default: %#v", withoutSubdirs)
	}

	withSubdirs, err := skill.NewLocalLoader(root, skill.WithScanSubdirs(true)).ListSkills(context.Background())
	if err != nil {
		t.Fatalf("ListSkills with subdirs returned error: %v", err)
	}
	if len(withSubdirs) != 2 {
		t.Fatalf("expected two subdirectory skills, got %d %#v", len(withSubdirs), withSubdirs)
	}
}

func TestLocalLoaderSkipsInvalidSkillFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: missing-description\n---\nbody\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	skills, err := skill.NewLocalLoader(root).ListSkills(context.Background())
	if err != nil {
		t.Fatalf("ListSkills returned error: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("invalid skill should be skipped: %#v", skills)
	}
}

func writeSkill(t *testing.T, dir, name, description, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
