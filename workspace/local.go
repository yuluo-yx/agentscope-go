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

package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/tool/builtin"
	"github.com/yuluo-yx/agentscope-go/tool/skill"
)

const defaultWorkspaceInstructions = `<workspace>
You have access to a local workspace at {workdir}.

This workspace contains:
- data/ for offloaded multimodal files
- skills/ for reusable skills
- sessions/ for offloaded context and tool results
</workspace>`

// LocalWorkspace is a local-directory workspace.
type LocalWorkspace struct {
	id           string
	workdir      string
	instructions string
	alive        bool
	skillPaths   []string
	mcps         []MCPClient
}

// LocalOption configures a LocalWorkspace.
type LocalOption func(*LocalWorkspace)

// WithWorkspaceID sets a stable workspace ID.
func WithWorkspaceID(id string) LocalOption {
	return func(workspace *LocalWorkspace) {
		workspace.id = id
	}
}

// WithInstructions sets the workspace instruction template.
func WithInstructions(instructions string) LocalOption {
	return func(workspace *LocalWorkspace) {
		workspace.instructions = instructions
	}
}

// WithSkillPaths seeds skills during initialization.
func WithSkillPaths(paths ...string) LocalOption {
	return func(workspace *LocalWorkspace) {
		workspace.skillPaths = append(workspace.skillPaths, paths...)
	}
}

// WithMCPs seeds MCP clients during initialization.
func WithMCPs(mcps ...MCPClient) LocalOption {
	return func(workspace *LocalWorkspace) {
		workspace.mcps = append(workspace.mcps, mcps...)
	}
}

// NewLocalWorkspace creates a local workspace rooted at workdir.
func NewLocalWorkspace(workdir string, opts ...LocalOption) (*LocalWorkspace, error) {
	if strings.TrimSpace(workdir) == "" {
		return nil, fmt.Errorf("workspace: workdir is empty")
	}
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}
	workspace := &LocalWorkspace{
		id:           uuid.New().String(),
		workdir:      absWorkdir,
		instructions: defaultWorkspaceInstructions,
	}
	for _, opt := range opts {
		opt(workspace)
	}
	if workspace.id == "" {
		workspace.id = uuid.New().String()
	}
	if workspace.instructions == "" {
		workspace.instructions = defaultWorkspaceInstructions
	}
	return workspace, nil
}

// WorkspaceID returns the workspace identifier.
func (w *LocalWorkspace) WorkspaceID() string {
	if w == nil {
		return ""
	}
	return w.id
}

// IsAlive reports whether the workspace is initialized.
func (w *LocalWorkspace) IsAlive() bool {
	return w != nil && w.alive
}

// Initialize creates workspace directories and seeds configured skills.
func (w *LocalWorkspace) Initialize(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("workspace: nil local workspace")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.alive {
		return nil
	}
	for _, subdir := range []string{"data", "skills", "sessions"} {
		if err := os.MkdirAll(filepath.Join(w.workdir, subdir), 0o700); err != nil {
			return err
		}
	}
	for _, path := range w.skillPaths {
		if err := w.AddSkill(ctx, path); err != nil {
			return err
		}
	}
	w.alive = true
	return nil
}

// Close marks the local workspace as inactive.
func (w *LocalWorkspace) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w != nil {
		w.alive = false
	}
	return nil
}

// Reset removes workspace-owned data, sessions, and skills.
func (w *LocalWorkspace) Reset(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, subdir := range []string{"data", "skills", "sessions"} {
		if err := os.RemoveAll(filepath.Join(w.workdir, subdir)); err != nil {
			return err
		}
	}
	w.mcps = nil
	w.alive = false
	return nil
}

// GetInstructions returns the workspace system-prompt fragment.
func (w *LocalWorkspace) GetInstructions(ctx context.Context) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace: nil local workspace")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return strings.ReplaceAll(w.instructions, "{workdir}", w.workdir), nil
}

// ListTools returns the built-in tools scoped to this local workspace.
func (w *LocalWorkspace) ListTools(ctx context.Context) ([]Tool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []Tool{
		builtin.NewBash(),
		builtin.NewEdit(),
		builtin.NewGlob(),
		builtin.NewGrep(),
		builtin.NewRead(),
		builtin.NewWrite(),
	}, nil
}

// ListMCPs returns MCP clients registered on the workspace.
func (w *LocalWorkspace) ListMCPs(ctx context.Context) ([]MCPClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w == nil || len(w.mcps) == 0 {
		return []MCPClient{}, nil
	}
	out := make([]MCPClient, len(w.mcps))
	copy(out, w.mcps)
	return out, nil
}

// ListSkills returns skills available under the workspace skills directory.
func (w *LocalWorkspace) ListSkills(ctx context.Context) ([]skill.Skill, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace: nil local workspace")
	}
	return skill.NewLocalLoader(filepath.Join(w.workdir, "skills"), skill.WithScanSubdirs(true)).ListSkills(ctx)
}

// AddMCP registers an MCP client in memory.
func (w *LocalWorkspace) AddMCP(ctx context.Context, mcp MCPClient) error {
	if w == nil {
		return fmt.Errorf("workspace: nil local workspace")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if mcp == nil {
		return fmt.Errorf("workspace: nil MCP client")
	}
	for _, existing := range w.mcps {
		if existing != nil && existing.Name() == mcp.Name() {
			return fmt.Errorf("workspace: MCP %q already exists", mcp.Name())
		}
	}
	w.mcps = append(w.mcps, mcp)
	return nil
}

// RemoveMCP removes an MCP client by name.
func (w *LocalWorkspace) RemoveMCP(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return nil
	}
	for index, mcp := range w.mcps {
		if mcp != nil && mcp.Name() == name {
			w.mcps = append(w.mcps[:index], w.mcps[index+1:]...)
			return nil
		}
	}
	return nil
}

// AddSkill copies one skill directory into the workspace.
func (w *LocalWorkspace) AddSkill(ctx context.Context, sourceDir string) error {
	if w == nil {
		return fmt.Errorf("workspace: nil local workspace")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	loaded, err := skill.LoadDir(ctx, sourceDir)
	if err != nil {
		return err
	}
	sourceHash, err := skillHash(sourceDir)
	if err != nil {
		return err
	}
	skillsDir := filepath.Join(w.workdir, "skills")
	if mkdirErr := os.MkdirAll(skillsDir, 0o700); mkdirErr != nil {
		return mkdirErr
	}
	duplicate, err := w.hasSkillHash(skillsDir, sourceHash)
	if err != nil {
		return err
	}
	if duplicate {
		return nil
	}
	dirName := uniqueDirName(skillsDir, sanitizeDirName(loaded.Name))
	destination := filepath.Join(skillsDir, dirName)
	if !insideDir(skillsDir, destination) {
		return fmt.Errorf("workspace: skill destination escapes skills directory")
	}
	return copyDir(sourceDir, destination)
}

// RemoveSkill removes a skill by its agent-facing name.
func (w *LocalWorkspace) RemoveSkill(ctx context.Context, name string) error {
	if w == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	skills, err := w.ListSkills(ctx)
	if err != nil {
		return err
	}
	for _, loaded := range skills {
		if loaded.Name == name {
			return os.RemoveAll(loaded.Dir)
		}
	}
	return nil
}

// OffloadContext appends messages to a session JSONL file.
func (w *LocalWorkspace) OffloadContext(ctx context.Context, sessionID string, messages []*message.Message) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace: nil local workspace")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := filepath.Join(w.workdir, "sessions", sessionID, "context.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		cloned := msg.Clone()
		if err := w.replaceBase64DataBlocks(ctx, cloned.Content); err != nil {
			return "", err
		}
		if err := encoder.Encode(cloned); err != nil {
			return "", err
		}
	}
	return path, nil
}

// OffloadToolResult writes a tool result into a session text file.
func (w *LocalWorkspace) OffloadToolResult(ctx context.Context, sessionID string, result *message.ToolResultBlock) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace: nil local workspace")
	}
	if result == nil {
		return "", fmt.Errorf("workspace: nil tool result")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir := filepath.Join(w.workdir, "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := uniqueFilePath(dir, fmt.Sprintf("tool_result-%s.txt", result.ID))
	var builder strings.Builder
	if result.Output.Raw != "" {
		builder.WriteString(result.Output.Raw)
	} else {
		for _, block := range result.Output.Blocks {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			switch typed := block.(type) {
			case *message.TextBlock:
				builder.WriteString(typed.Text)
			case *message.DataBlock:
				blockForOutput := typed
				if _, ok := typed.Source.(*message.Base64Source); ok {
					offloaded, err := w.offloadDataBlock(ctx, typed)
					if err != nil {
						return "", err
					}
					blockForOutput = offloaded
				}
				source, ok := blockForOutput.Source.(*message.URLSource)
				if !ok {
					continue
				}
				name := ""
				if blockForOutput.Name != nil {
					name = *blockForOutput.Name
				}
				fmt.Fprintf(&builder, "<data url='%s' name='%s' media_type='%s'/>", source.URL, name, source.MediaType)
			}
		}
	}
	return path, os.WriteFile(path, []byte(builder.String()), 0o600)
}

func (w *LocalWorkspace) replaceBase64DataBlocks(ctx context.Context, blocks message.ContentBlockList) error {
	for _, block := range blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch typed := block.(type) {
		case *message.DataBlock:
			if _, ok := typed.Source.(*message.Base64Source); ok {
				offloaded, err := w.offloadDataBlock(ctx, typed)
				if err != nil {
					return err
				}
				*typed = *offloaded
			}
		case *message.ToolResultBlock:
			if err := w.replaceBase64DataBlocks(ctx, typed.Output.Blocks); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *LocalWorkspace) offloadDataBlock(ctx context.Context, block *message.DataBlock) (*message.DataBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, ok := block.Source.(*message.Base64Source)
	if !ok {
		return block.Clone().(*message.DataBlock), nil
	}
	data, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(source.Data))
	extension := ".bin"
	if extensions, err := mime.ExtensionsByType(source.MediaType); err == nil && len(extensions) > 0 {
		extension = extensions[0]
	}
	path := filepath.Join(w.workdir, "data", hex.EncodeToString(hash[:])+extension)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
	}
	return message.NewDataBlock(message.NewURLSource(fileURL(path), source.MediaType), message.WithDataBlockID(block.ID), dataBlockNameOption(block.Name)), nil
}

func dataBlockNameOption(name *string) message.DataBlockOption {
	return func(block *message.DataBlock) {
		if name != nil {
			value := *name
			block.Name = &value
		}
	}
}

func (w *LocalWorkspace) hasSkillHash(skillsDir, sourceHash string) (bool, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		hash, err := skillHash(filepath.Join(skillsDir, entry.Name()))
		if err != nil {
			continue
		}
		if hash == sourceHash {
			return true, nil
		}
	}
	return false, nil
}

func skillHash(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func sanitizeDirName(name string) string {
	var builder strings.Builder
	for _, r := range name {
		if r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	out := strings.Trim(builder.String(), "_")
	if out == "" {
		return "skill"
	}
	return out
}

func uniqueDirName(parent, base string) string {
	candidate := base
	for index := 1; ; index++ {
		if _, err := os.Stat(filepath.Join(parent, candidate)); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d", base, index)
	}
}

func uniqueFilePath(dir, name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	ext := filepath.Ext(name)
	candidate := filepath.Join(dir, name)
	for index := 1; ; index++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, index, ext))
	}
}

func insideDir(parent, child string) bool {
	parentReal, err := filepath.EvalSymlinks(parent)
	if err != nil {
		parentReal = filepath.Clean(parent)
	}
	childReal, err := filepath.EvalSymlinks(filepath.Dir(child))
	if err != nil {
		childReal = filepath.Clean(filepath.Dir(child))
	}
	rel, err := filepath.Rel(parentReal, childReal)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func copyDir(source, destination string) error {
	source = filepath.Clean(source)
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyFile(path, target)
	})
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

var _ Workspace = (*LocalWorkspace)(nil)
