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

// Package skill loads agent skills from local SKILL.md directories.
package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Skill is one loaded agent skill.
type Skill struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Dir         string    `json:"dir"`
	Markdown    string    `json:"markdown"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Loader lists skills from a backing source.
type Loader interface {
	ListSkills(context.Context) ([]Skill, error)
}

type skillFrontMatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// LoadDir loads one skill from a directory containing SKILL.md.
func LoadDir(ctx context.Context, dir string) (*Skill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(absDir, "SKILL.md")
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("skill: %s is a directory", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	frontMatter, markdown, err := parseSkillMarkdown(string(data))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(frontMatter.Name) == "" || strings.TrimSpace(frontMatter.Description) == "" {
		return nil, fmt.Errorf("skill: %s missing required name or description", path)
	}
	return &Skill{
		Name:        strings.TrimSpace(frontMatter.Name),
		Description: strings.TrimSpace(frontMatter.Description),
		Dir:         absDir,
		Markdown:    markdown,
		UpdatedAt:   info.ModTime(),
	}, nil
}

func parseSkillMarkdown(content string) (skillFrontMatter, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return skillFrontMatter{}, "", fmt.Errorf("skill: SKILL.md missing YAML front matter")
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return skillFrontMatter{}, "", fmt.Errorf("skill: SKILL.md front matter is not closed")
	}
	rawFrontMatter := rest[:end]
	markdown := rest[end+len("\n---"):]
	markdown = strings.TrimPrefix(markdown, "\n")
	var frontMatter skillFrontMatter
	if err := yaml.Unmarshal([]byte(rawFrontMatter), &frontMatter); err != nil {
		return skillFrontMatter{}, "", fmt.Errorf("skill: invalid YAML front matter: %w", err)
	}
	return frontMatter, markdown, nil
}
