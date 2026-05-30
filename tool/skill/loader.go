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

package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// LocalLoader loads skills from a local directory.
type LocalLoader struct {
	directory   string
	scanSubdirs bool
	cache       map[string]Skill
}

// LocalLoaderOption configures a local skill loader.
type LocalLoaderOption func(*LocalLoader)

// WithScanSubdirs controls whether subdirectories are scanned for SKILL.md.
func WithScanSubdirs(scan bool) LocalLoaderOption {
	return func(loader *LocalLoader) {
		loader.scanSubdirs = scan
	}
}

// NewLocalLoader creates a local filesystem skill loader.
func NewLocalLoader(directory string, opts ...LocalLoaderOption) *LocalLoader {
	absDir, err := filepath.Abs(directory)
	if err == nil {
		directory = absDir
	}
	loader := &LocalLoader{
		directory: directory,
		cache:     map[string]Skill{},
	}
	for _, opt := range opts {
		opt(loader)
	}
	return loader
}

// ListSkills lists valid skills under the configured directory.
func (l *LocalLoader) ListSkills(ctx context.Context) ([]Skill, error) {
	if l == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(l.directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Skill{}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return []Skill{}, nil
	}
	dirs, err := l.skillDirs()
	if err != nil {
		return nil, err
	}
	skills := make([]Skill, 0, len(dirs))
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		skill, err := l.loadSingle(ctx, dir)
		if err != nil || skill == nil {
			continue
		}
		skills = append(skills, *skill)
	}
	return skills, nil
}

func (l *LocalLoader) skillDirs() ([]string, error) {
	dirs := []string{}
	if hasSkillFile(l.directory) {
		dirs = append(dirs, l.directory)
	}
	if !l.scanSubdirs {
		return dirs, nil
	}
	err := filepath.WalkDir(l.directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() || path == l.directory {
			return nil
		}
		if hasSkillFile(path) {
			dirs = append(dirs, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	return dirs, nil
}

func (l *LocalLoader) loadSingle(ctx context.Context, dir string) (*Skill, error) {
	path := filepath.Join(dir, "SKILL.md")
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if cached, ok := l.cache[dir]; ok && cached.UpdatedAt.Equal(info.ModTime()) {
		cp := cached
		return &cp, nil
	}
	loaded, err := LoadDir(ctx, dir)
	if err != nil {
		return nil, err
	}
	l.cache[dir] = *loaded
	return loaded, nil
}

func hasSkillFile(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
}
