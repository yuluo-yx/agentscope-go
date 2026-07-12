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

package sandboxed

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/skill"
)

type remoteSkillLoader struct {
	workspace *Workspace
}

const skillManifestFile = ".agentscope-manifest"

type localSkillFile struct {
	relative string
	data     []byte
}

func (l *remoteSkillLoader) ListSkills(ctx context.Context) ([]skill.Skill, error) {
	if l == nil || l.workspace == nil {
		return []skill.Skill{}, nil
	}
	return l.workspace.listSkills(ctx)
}

// ListSkills scans the remote skills directory and parses valid SKILL.md files.
func (w *Workspace) ListSkills(ctx context.Context) ([]skill.Skill, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	return (&remoteSkillLoader{workspace: w}).ListSkills(ctx)
}

func (w *Workspace) listSkills(ctx context.Context) ([]skill.Skill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive || w.backend == nil {
		return nil, fmt.Errorf("workspace/sandboxed: workspace is not initialized")
	}
	return w.listSkillsLocked(ctx)
}

func (w *Workspace) listSkillsLocked(ctx context.Context) ([]skill.Skill, error) {
	result, err := w.backend.Exec(ctx, []string{
		"find", w.skillsDir,
		"-mindepth", "2",
		"-type", "f",
		"-name", "SKILL.md",
		"-print",
	}, ExecOptions{CWD: w.workdir})
	if err != nil {
		return nil, fmt.Errorf("workspace/sandboxed: scan skills: %w", err)
	}
	if !result.OK() {
		return nil, commandError("scan skills", result)
	}
	paths := nonEmptyLines(result.Stdout)
	sort.Strings(paths)
	out := make([]skill.Skill, 0, len(paths))
	for _, filename := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, readErr := w.backend.ReadFile(ctx, filename)
		if readErr != nil {
			continue
		}
		loaded, parseErr := parseRemoteSkill(filename, data)
		if parseErr != nil {
			continue
		}
		out = append(out, *loaded)
	}
	return out, nil
}

// AddSkill copies a local Skill directory into the remote skills directory.
func (w *Workspace) AddSkill(ctx context.Context, sourceDir string) error {
	if w == nil {
		return fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive || w.backend == nil {
		return fmt.Errorf("workspace/sandboxed: workspace is not initialized")
	}
	return w.addSkillLocked(ctx, sourceDir)
}

func (w *Workspace) addSkillLocked(ctx context.Context, sourceDir string) error {
	loaded, err := skill.LoadDir(ctx, sourceDir)
	if err != nil {
		return err
	}
	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		return err
	}
	files, manifest, err := collectLocalSkillFiles(ctx, absSource)
	if err != nil {
		return err
	}
	dirName := skillDirName(loaded.Name)
	destination := path.Join(w.skillsDir, dirName)
	if !insideRemoteDir(w.skillsDir, destination) {
		return fmt.Errorf("workspace/sandboxed: skill destination escapes skills directory")
	}
	exists, err := w.pathExists(ctx, destination)
	if err != nil {
		return err
	}
	if exists {
		remoteManifest, readErr := w.backend.ReadFile(ctx, path.Join(destination, skillManifestFile))
		if readErr == nil && strings.TrimSpace(string(remoteManifest)) == manifest {
			return nil
		}
		return fmt.Errorf("workspace/sandboxed: skill %q already exists", loaded.Name)
	}

	written := false
	defer func() {
		if !written {
			cleanupCtx, cancel := w.detachedContext(ctx)
			defer cancel()
			_ = w.deleteTrees(cleanupCtx, destination)
		}
	}()
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		remotePath := path.Join(destination, file.relative)
		if !insideRemoteDir(destination, remotePath) {
			return fmt.Errorf("workspace/sandboxed: skill file escapes destination")
		}
		if err := w.backend.WriteFile(ctx, remotePath, file.data); err != nil {
			return err
		}
	}
	if err := w.backend.WriteFile(ctx, path.Join(destination, skillManifestFile), []byte(manifest+"\n")); err != nil {
		return err
	}
	written = true
	return nil
}

func skillDirName(name string) string {
	dirName := sanitizeSkillDir(name)
	if dirName != "" {
		return dirName
	}
	hash := sha256.Sum256([]byte(name))
	return "skill-" + hex.EncodeToString(hash[:6])
}

func collectLocalSkillFiles(
	ctx context.Context,
	root string,
) ([]localSkillFile, string, error) {
	sourceRoot, err := openLocalSkillRoot(root)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = sourceRoot.Close() }()

	files := []localSkillFile{}
	err = fs.WalkDir(sourceRoot.FS(), ".", func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("workspace/sandboxed: skill contains symlink %q", filename)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("workspace/sandboxed: skill contains non-regular file %q", filename)
		}
		relative := filepath.ToSlash(filename)
		if relative == skillManifestFile {
			return fmt.Errorf("workspace/sandboxed: skill uses reserved file %q", skillManifestFile)
		}
		data, err := sourceRoot.ReadFile(filepath.FromSlash(filename))
		if err != nil {
			return err
		}
		files = append(files, localSkillFile{relative: relative, data: data})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Slice(files, func(left, right int) bool {
		return files[left].relative < files[right].relative
	})
	digest := sha256.New()
	for _, file := range files {
		writeFingerprintPart(digest, []byte(file.relative))
		writeFingerprintPart(digest, file.data)
	}
	return files, hex.EncodeToString(digest.Sum(nil)), nil
}

func openLocalSkillRoot(root string) (*os.Root, error) {
	initial, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if initial.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("workspace/sandboxed: skill contains symlink %q", root)
	}

	sourceRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	opened, openedErr := sourceRoot.Stat(".")
	current, currentErr := os.Lstat(root)
	if err := errors.Join(openedErr, currentErr); err != nil {
		_ = sourceRoot.Close()
		return nil, err
	}
	if current.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(initial, opened) ||
		!os.SameFile(opened, current) {
		_ = sourceRoot.Close()
		return nil, fmt.Errorf("workspace/sandboxed: skill root changed during collection")
	}

	return sourceRoot, nil
}

// RemoveSkill removes a remote Skill by its agent-visible name.
func (w *Workspace) RemoveSkill(ctx context.Context, name string) error {
	if w == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("workspace/sandboxed: skill name is empty")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive || w.backend == nil {
		return fmt.Errorf("workspace/sandboxed: workspace is not initialized")
	}
	skills, err := w.listSkillsLocked(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, current := range skills {
		if current.Name != name {
			continue
		}
		if err := w.deleteTrees(ctx, current.Dir); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// OffloadContext appends messages to a remote session JSONL file.
func (w *Workspace) OffloadContext(
	ctx context.Context,
	sessionID string,
	messages []*message.Message,
) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive || w.backend == nil {
		return "", fmt.Errorf("workspace/sandboxed: workspace is not initialized")
	}
	sessionDir, err := remoteChildPath(w.sessionsDir, sessionID)
	if err != nil {
		return "", fmt.Errorf("workspace/sandboxed: invalid session id: %w", err)
	}
	filename := path.Join(sessionDir, "context.jsonl")
	var payload []byte
	if exists, existsErr := w.fileExists(ctx, filename); existsErr != nil {
		return "", existsErr
	} else if exists {
		payload, err = w.backend.ReadFile(ctx, filename)
		if err != nil {
			return "", err
		}
	}
	for _, current := range messages {
		if current == nil {
			continue
		}
		cloned := current.Clone()
		if err := w.replaceBase64BlocksLocked(ctx, cloned.Content); err != nil {
			return "", err
		}
		line, err := json.Marshal(cloned)
		if err != nil {
			return "", err
		}
		payload = append(payload, line...)
		payload = append(payload, '\n')
	}
	if err := w.backend.WriteFile(ctx, filename, payload); err != nil {
		return "", err
	}
	return filename, nil
}

// OffloadToolResult writes a tool result into the remote session directory.
func (w *Workspace) OffloadToolResult(
	ctx context.Context,
	sessionID string,
	result *message.ToolResultBlock,
) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	if result == nil {
		return "", fmt.Errorf("workspace/sandboxed: nil tool result")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive || w.backend == nil {
		return "", fmt.Errorf("workspace/sandboxed: workspace is not initialized")
	}
	sessionDir, err := remoteChildPath(w.sessionsDir, sessionID)
	if err != nil {
		return "", fmt.Errorf("workspace/sandboxed: invalid session id: %w", err)
	}
	filename, err := w.uniqueRemoteFile(ctx, sessionDir, "tool_result-"+safeFileSegment(result.ID)+".txt")
	if err != nil {
		return "", err
	}
	content, err := w.toolResultContentLocked(ctx, result)
	if err != nil {
		return "", err
	}
	if err := w.backend.WriteFile(ctx, filename, []byte(content)); err != nil {
		return "", err
	}
	return filename, nil
}

func (w *Workspace) toolResultContentLocked(
	ctx context.Context,
	result *message.ToolResultBlock,
) (string, error) {
	if result.Output.Raw != "" {
		return result.Output.Raw, nil
	}
	var builder strings.Builder
	for _, block := range result.Output.Blocks {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		switch typed := block.(type) {
		case *message.TextBlock:
			builder.WriteString(typed.Text)
		case *message.DataBlock:
			stored, err := w.offloadDataBlockLocked(ctx, typed)
			if err != nil {
				return "", err
			}
			source, ok := stored.Source.(*message.URLSource)
			if !ok {
				continue
			}
			name := ""
			if stored.Name != nil {
				name = *stored.Name
			}
			fmt.Fprintf(
				&builder,
				"<data url='%s' name='%s' media_type='%s'/>",
				html.EscapeString(source.URL),
				html.EscapeString(name),
				html.EscapeString(source.MediaType),
			)
		}
	}
	return builder.String(), nil
}

// OffloadDataBlock writes a base64 DataBlock into the remote data directory.
func (w *Workspace) OffloadDataBlock(
	ctx context.Context,
	block *message.DataBlock,
) (*message.DataBlock, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace/sandboxed: nil workspace")
	}
	if block == nil {
		return nil, fmt.Errorf("workspace/sandboxed: nil data block")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive || w.backend == nil {
		return nil, fmt.Errorf("workspace/sandboxed: workspace is not initialized")
	}
	return w.offloadDataBlockLocked(ctx, block)
}

func (w *Workspace) offloadDataBlockLocked(
	ctx context.Context,
	block *message.DataBlock,
) (*message.DataBlock, error) {
	if block == nil {
		return nil, fmt.Errorf("workspace/sandboxed: nil data block")
	}
	source, ok := block.Source.(*message.Base64Source)
	if !ok {
		return block.Clone().(*message.DataBlock), nil
	}
	data, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	filename := path.Join(
		w.dataDir,
		hex.EncodeToString(hash[:])+mediaExtension(source.MediaType),
	)
	exists, err := w.fileExists(ctx, filename)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := w.backend.WriteFile(ctx, filename, data); err != nil {
			return nil, err
		}
	}
	remoteURL := (&url.URL{Scheme: "file", Path: filename}).String()
	return message.NewDataBlock(
		message.NewURLSource(remoteURL, source.MediaType),
		message.WithDataBlockID(block.ID),
		dataBlockNameOption(block.Name),
	), nil
}

func (w *Workspace) replaceBase64BlocksLocked(
	ctx context.Context,
	blocks message.ContentBlockList,
) error {
	for _, block := range blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch typed := block.(type) {
		case *message.DataBlock:
			if _, ok := typed.Source.(*message.Base64Source); ok {
				stored, err := w.offloadDataBlockLocked(ctx, typed)
				if err != nil {
					return err
				}
				*typed = *stored
			}
		case *message.ToolResultBlock:
			if err := w.replaceBase64BlocksLocked(ctx, typed.Output.Blocks); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Workspace) pathExists(ctx context.Context, filename string) (bool, error) {
	result, err := w.backend.Exec(ctx, []string{"test", "-e", filename}, ExecOptions{CWD: "/"})
	if err != nil {
		return false, fmt.Errorf("workspace/sandboxed: test path %q: %w", filename, err)
	}
	switch result.ExitCode {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, commandError("test path "+filename, result)
	}
}

func (w *Workspace) uniqueRemoteFile(ctx context.Context, dir, filename string) (string, error) {
	extension := path.Ext(filename)
	base := strings.TrimSuffix(filename, extension)
	for index := 0; ; index++ {
		candidate := path.Join(dir, filename)
		if index > 0 {
			candidate = path.Join(dir, fmt.Sprintf("%s-%d%s", base, index, extension))
		}
		exists, err := w.fileExists(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
}

type remoteSkillFrontMatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func parseRemoteSkill(filename string, data []byte) (*skill.Skill, error) {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return nil, fmt.Errorf("workspace/sandboxed: SKILL.md missing YAML front matter")
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, fmt.Errorf("workspace/sandboxed: SKILL.md front matter is not closed")
	}
	var frontMatter remoteSkillFrontMatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &frontMatter); err != nil {
		return nil, err
	}
	frontMatter.Name = strings.TrimSpace(frontMatter.Name)
	frontMatter.Description = strings.TrimSpace(frontMatter.Description)
	if frontMatter.Name == "" || frontMatter.Description == "" {
		return nil, fmt.Errorf("workspace/sandboxed: SKILL.md missing name or description")
	}
	markdown := strings.TrimPrefix(rest[end+len("\n---"):], "\n")
	return &skill.Skill{
		Name:        frontMatter.Name,
		Description: frontMatter.Description,
		Dir:         path.Dir(filename),
		Markdown:    markdown,
		UpdatedAt:   time.Time{},
	}, nil
}

func remoteChildPath(root, relative string) (string, error) {
	relative = strings.TrimSpace(relative)
	if relative == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.ContainsRune(relative, '\x00') || strings.HasPrefix(relative, "/") {
		return "", fmt.Errorf("path must be a relative sandbox path")
	}
	destination := path.Join(root, relative)
	if path.Clean(destination) == path.Clean(root) || !insideRemoteDir(root, destination) {
		return "", fmt.Errorf("path escapes root")
	}
	return destination, nil
}

func insideRemoteDir(root, destination string) bool {
	root = path.Clean(root)
	destination = path.Clean(destination)
	return destination == root || strings.HasPrefix(destination, strings.TrimRight(root, "/")+"/")
}

func sanitizeSkillDir(name string) string {
	var builder strings.Builder
	lastWasDash := false
	for _, current := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsLetter(current), unicode.IsDigit(current):
			builder.WriteRune(current)
			lastWasDash = false
		case current == '-', current == '_', unicode.IsSpace(current):
			if builder.Len() > 0 && !lastWasDash {
				builder.WriteByte('-')
				lastWasDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func safeFileSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "result"
	}
	var builder strings.Builder
	for _, current := range value {
		if unicode.IsLetter(current) || unicode.IsDigit(current) || current == '-' || current == '_' {
			builder.WriteRune(current)
		}
	}
	if builder.Len() == 0 {
		return "result"
	}
	return builder.String()
}

func mediaExtension(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	}
	if extensions, err := mime.ExtensionsByType(mediaType); err == nil && len(extensions) > 0 {
		return extensions[0]
	}
	return ".bin"
}

func dataBlockNameOption(name *string) message.DataBlockOption {
	return func(block *message.DataBlock) {
		if name != nil {
			value := *name
			block.Name = &value
		}
	}
}

func nonEmptyLines(data []byte) []string {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
