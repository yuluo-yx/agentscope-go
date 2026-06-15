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

package skill_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestLocalLoaderEmptyCanceledAndCacheBranches(t *testing.T) {
	t.Parallel()

	if skills, err := (*skill.LocalLoader)(nil).ListSkills(context.Background()); err != nil || skills != nil {
		t.Fatalf("nil loader should return nil skills and nil error: %#v %v", skills, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := skill.NewLocalLoader(t.TempDir()).ListSkills(ctx); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("canceled ListSkills error mismatch: %v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	skills, err := skill.NewLocalLoader(missing).ListSkills(context.Background())
	if err != nil || len(skills) != 0 {
		t.Fatalf("missing skill dir should be empty: %#v %v", skills, err)
	}

	filePath := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	skills, err = skill.NewLocalLoader(filePath).ListSkills(context.Background())
	if err != nil || len(skills) != 0 {
		t.Fatalf("file loader path should be empty: %#v %v", skills, err)
	}

	root := t.TempDir()
	writeSkill(t, root, "cached", "Cached skill", "first\n")
	loader := skill.NewLocalLoader(root)
	first, err := loader.ListSkills(context.Background())
	if err != nil || len(first) != 1 {
		t.Fatalf("first ListSkills mismatch: %#v %v", first, err)
	}
	second, err := loader.ListSkills(context.Background())
	if err != nil || len(second) != 1 || second[0].Name != "cached" {
		t.Fatalf("cached ListSkills mismatch: %#v %v", second, err)
	}
}

func TestLoadDirErrorBranches(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := skill.LoadDir(ctx, t.TempDir()); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("canceled LoadDir error mismatch: %v", err)
	}

	if _, err := skill.LoadDir(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing skill dir should return error")
	}

	dirWithSkillDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dirWithSkillDir, "SKILL.md"), 0o700); err != nil {
		t.Fatalf("Mkdir returned error: %v", err)
	}
	if _, err := skill.LoadDir(context.Background(), dirWithSkillDir); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("directory SKILL.md error mismatch: %v", err)
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "missing front matter", content: "plain body\n", want: "missing YAML front matter"},
		{name: "unclosed front matter", content: "---\nname: a\n", want: "front matter is not closed"},
		{name: "invalid yaml", content: "---\nname: [\n---\nbody\n", want: "invalid YAML front matter"},
		{name: "missing required fields", content: "---\nname: only-name\n---\nbody\n", want: "missing required name or description"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(tt.content), 0o600); err != nil {
				t.Fatalf("WriteFile returned error: %v", err)
			}
			if _, err := skill.LoadDir(context.Background(), root); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadDir error = %v, want %q", err, tt.want)
			}
		})
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
