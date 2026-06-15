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

package local

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
	"sort"
	"strings"
	"unicode"

	"github.com/yuluo-yx/agentscope-go/message"
	"github.com/yuluo-yx/agentscope-go/tool/builtin"
	toolmcp "github.com/yuluo-yx/agentscope-go/tool/mcp"
	"github.com/yuluo-yx/agentscope-go/tool/skill"
	"github.com/yuluo-yx/agentscope-go/utils"
	asworkspace "github.com/yuluo-yx/agentscope-go/workspace"
)

const defaultWorkspaceInstructions = `<workspace>
You have access to a local workspace at {workdir}.

This workspace contains:
- data/ for offloaded multimodal files
- skills/ for reusable skills
- sessions/ for offloaded context and tool results
</workspace>`

// Workspace is a local-directory workspace.
type Workspace struct {
	id           string
	workdir      string
	instructions string
	alive        bool
	skillPaths   []string
	mcps         []asworkspace.MCPClient
	mcpFactory   asworkspace.MCPClientFactory
}

// Option configures a Workspace.
type Option func(*Workspace)

// WithWorkspaceID sets a stable workspace ID.
func WithWorkspaceID(id string) Option {
	return func(workspace *Workspace) {
		workspace.id = id
	}
}

// WithInstructions sets the workspace instruction template.
func WithInstructions(instructions string) Option {
	return func(workspace *Workspace) {
		workspace.instructions = instructions
	}
}

// WithSkillPaths seeds skills during initialization.
func WithSkillPaths(paths ...string) Option {
	return func(workspace *Workspace) {
		workspace.skillPaths = append(workspace.skillPaths, paths...)
	}
}

// WithMCPs seeds MCP clients during initialization.
func WithMCPs(mcps ...asworkspace.MCPClient) Option {
	return func(workspace *Workspace) {
		workspace.mcps = append(workspace.mcps, mcps...)
	}
}

// WithMCPClientFactory sets the factory used to restore persisted MCP configs.
func WithMCPClientFactory(factory asworkspace.MCPClientFactory) Option {
	return func(workspace *Workspace) {
		workspace.mcpFactory = factory
	}
}

// NewWorkspace creates a local workspace rooted at workdir.
func NewWorkspace(workdir string, opts ...Option) (*Workspace, error) {
	if strings.TrimSpace(workdir) == "" {
		return nil, fmt.Errorf("workspace: workdir is empty")
	}
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}
	workspace := &Workspace{
		id:           utils.NewID(),
		workdir:      absWorkdir,
		instructions: defaultWorkspaceInstructions,
		mcpFactory:   defaultMCPFactory,
	}
	for _, opt := range opts {
		opt(workspace)
	}
	if workspace.id == "" {
		workspace.id = utils.NewID()
	}
	if workspace.instructions == "" {
		workspace.instructions = defaultWorkspaceInstructions
	}
	if workspace.mcpFactory == nil {
		workspace.mcpFactory = defaultMCPFactory
	}
	return workspace, nil
}

// WorkspaceID returns the workspace identifier.
func (w *Workspace) WorkspaceID() string {
	if w == nil {
		return ""
	}
	return w.id
}

// WorkspaceRoot returns the local root exposed to the agent.
func (w *Workspace) WorkspaceRoot() string {
	if w == nil {
		return ""
	}
	return w.workdir
}

// IsAlive reports whether the workspace is initialized.
func (w *Workspace) IsAlive() bool {
	return w != nil && w.alive
}

// Initialize creates workspace directories and seeds configured skills.
func (w *Workspace) Initialize(ctx context.Context) error {
	if w == nil {
		return fmt.Errorf("workspace: nil local workspace")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.alive {
		return nil
	}
	if err := os.MkdirAll(w.workdir, 0o700); err != nil {
		return err
	}
	for _, subdir := range []string{"data", "skills", "sessions"} {
		if err := os.MkdirAll(filepath.Join(w.workdir, subdir), 0o700); err != nil {
			return err
		}
	}
	if err := w.restoreOrSeedMCPs(ctx); err != nil {
		return err
	}
	for _, mcp := range w.mcps {
		if mcp != nil && mcp.IsStateful() && !mcp.IsConnected() {
			if err := mcp.Connect(ctx); err != nil {
				return err
			}
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
func (w *Workspace) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w != nil {
		w.alive = false
	}
	return nil
}

// Reset removes workspace-owned data, sessions, and skills.
func (w *Workspace) Reset(ctx context.Context) error {
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
	if err := os.Remove(filepath.Join(w.workdir, ".mcp")); err != nil && !os.IsNotExist(err) {
		return err
	}
	w.mcps = nil
	w.alive = false
	return nil
}

// GetInstructions returns the workspace system-prompt fragment.
func (w *Workspace) GetInstructions(ctx context.Context) (string, error) {
	if w == nil {
		return "", fmt.Errorf("workspace: nil local workspace")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return strings.ReplaceAll(w.instructions, "{workdir}", w.workdir), nil
}

// ListTools returns the built-in tools scoped to this local workspace.
func (w *Workspace) ListTools(ctx context.Context) ([]asworkspace.Tool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []asworkspace.Tool{
		builtin.NewBash(),
		builtin.NewEdit(),
		builtin.NewGlob(),
		builtin.NewGrep(),
		builtin.NewRead(),
		builtin.NewWrite(),
	}, nil
}

// ListMCPs returns MCP clients registered on the workspace.
func (w *Workspace) ListMCPs(ctx context.Context) ([]asworkspace.MCPClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w == nil || len(w.mcps) == 0 {
		return []asworkspace.MCPClient{}, nil
	}
	out := make([]asworkspace.MCPClient, len(w.mcps))
	copy(out, w.mcps)
	return out, nil
}

// ListSkills returns skills available under the workspace skills directory.
func (w *Workspace) ListSkills(ctx context.Context) ([]skill.Skill, error) {
	if w == nil {
		return nil, fmt.Errorf("workspace: nil local workspace")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	skillsDir := filepath.Join(w.workdir, "skills")
	index, err := w.reconcileSkillsIndex(skillsDir)
	if err != nil {
		return nil, err
	}
	out := make([]skill.Skill, 0, len(index.Skills))
	for _, dirName := range sortedSkillDirs(index.Skills) {
		entry := index.Skills[dirName]
		loaded, err := skill.LoadDir(ctx, filepath.Join(skillsDir, dirName))
		if err != nil || loaded == nil {
			continue
		}
		loaded.Name = entry.SkillName
		out = append(out, *loaded)
	}
	return out, nil
}

// AddMCP registers an MCP client in memory.
func (w *Workspace) AddMCP(ctx context.Context, mcp asworkspace.MCPClient) error {
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
	return w.saveMCPFile()
}

// RemoveMCP removes an MCP client by name.
func (w *Workspace) RemoveMCP(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return nil
	}
	for index, mcp := range w.mcps {
		if mcp != nil && mcp.Name() == name {
			if mcp.IsStateful() && mcp.IsConnected() {
				_ = mcp.Close()
			}
			w.mcps = append(w.mcps[:index], w.mcps[index+1:]...)
			return w.saveMCPFile()
		}
	}
	return nil
}

// AddSkill copies one skill directory into the workspace.
func (w *Workspace) AddSkill(ctx context.Context, sourceDir string) error {
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
	index, err := w.reconcileSkillsIndex(skillsDir)
	if err != nil {
		return err
	}
	if index.hasHash(sourceHash) {
		return nil
	}
	agentName := uniqueSkillName(index.Skills, loaded.Name)
	dirName := uniqueDirNameFromIndex(index.Skills, sanitizeDirName(loaded.Name), skillsDir)
	destination := filepath.Join(skillsDir, dirName)
	if !insideDir(skillsDir, destination) {
		return fmt.Errorf("workspace: skill destination escapes skills directory")
	}
	if err := copyDir(sourceDir, destination); err != nil {
		return err
	}
	index.Skills[dirName] = skillIndexEntry{Hash: sourceHash, SkillName: agentName}
	return w.saveSkillsIndex(skillsDir, index)
}

// RemoveSkill removes a skill by its agent-facing name.
func (w *Workspace) RemoveSkill(ctx context.Context, name string) error {
	if w == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	skillsDir := filepath.Join(w.workdir, "skills")
	index, err := w.reconcileSkillsIndex(skillsDir)
	if err != nil {
		return err
	}
	for dirName, entry := range index.Skills {
		if entry.SkillName == name {
			if err := os.RemoveAll(filepath.Join(skillsDir, dirName)); err != nil {
				return err
			}
			delete(index.Skills, dirName)
			return w.saveSkillsIndex(skillsDir, index)
		}
	}
	return nil
}

// OffloadContext appends messages to a session JSONL file.
func (w *Workspace) OffloadContext(ctx context.Context, sessionID string, messages []*message.Message) (string, error) {
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
func (w *Workspace) OffloadToolResult(ctx context.Context, sessionID string, result *message.ToolResultBlock) (string, error) {
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

func (w *Workspace) replaceBase64DataBlocks(ctx context.Context, blocks message.ContentBlockList) error {
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

// OffloadDataBlock persists a base64 DataBlock and returns a URL-backed block.
func (w *Workspace) OffloadDataBlock(ctx context.Context, block *message.DataBlock) (*message.DataBlock, error) {
	return w.offloadDataBlock(ctx, block)
}

func (w *Workspace) offloadDataBlock(ctx context.Context, block *message.DataBlock) (*message.DataBlock, error) {
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
	if mapped := mediaExtension(source.MediaType); mapped != "" {
		extension = mapped
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
	return ""
}

func dataBlockNameOption(name *string) message.DataBlockOption {
	return func(block *message.DataBlock) {
		if name != nil {
			value := *name
			block.Name = &value
		}
	}
}

type skillsIndex struct {
	SkillsDirMTime int64                      `json:"skills_dir_mtime"`
	Skills         map[string]skillIndexEntry `json:"skills"`
}

type skillIndexEntry struct {
	Hash      string `json:"hash"`
	SkillName string `json:"skill_name"`
}

func (i skillsIndex) hasHash(hash string) bool {
	for _, entry := range i.Skills {
		if entry.Hash == hash {
			return true
		}
	}
	return false
}

func (w *Workspace) restoreOrSeedMCPs(ctx context.Context) error {
	path := filepath.Join(w.workdir, ".mcp")
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		var configs []asworkspace.MCPClientConfig
		if len(strings.TrimSpace(string(data))) > 0 {
			decodeErr := json.Unmarshal(data, &configs)
			if decodeErr != nil {
				return decodeErr
			}
		}
		restored := make([]asworkspace.MCPClient, 0, len(configs))
		for _, config := range configs {
			contextErr := ctx.Err()
			if contextErr != nil {
				return contextErr
			}
			client, factoryErr := w.mcpFactory(config)
			if factoryErr != nil {
				return factoryErr
			}
			restored = append(restored, client)
		}
		w.mcps = restored
		return nil
	case !os.IsNotExist(readErr):
		return readErr
	}
	return w.saveMCPFile()
}

func (w *Workspace) saveMCPFile() error {
	if w == nil {
		return nil
	}
	configs := make([]asworkspace.MCPClientConfig, 0, len(w.mcps))
	for _, client := range w.mcps {
		if client == nil {
			continue
		}
		provider, ok := client.(asworkspace.MCPConfigProvider)
		if !ok {
			return fmt.Errorf("workspace: MCP %q cannot be persisted", client.Name())
		}
		config, err := provider.MCPClientConfig()
		if err != nil {
			return err
		}
		configs = append(configs, config)
	}
	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(w.workdir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.workdir, ".mcp"), data, 0o600)
}

func defaultMCPFactory(config asworkspace.MCPClientConfig) (asworkspace.MCPClient, error) {
	opts := []toolmcp.ClientOption{toolmcp.WithStateful(config.Stateful)}
	if len(config.EnabledTools) > 0 {
		opts = append(opts, toolmcp.WithEnabledTools(config.EnabledTools...))
	}
	if len(config.DisabledTools) > 0 {
		opts = append(opts, toolmcp.WithDisabledTools(config.DisabledTools...))
	}
	if config.ExecutionTimeout > 0 {
		opts = append(opts, toolmcp.WithExecutionTimeout(config.ExecutionTimeout))
	}
	switch config.Type {
	case asworkspace.MCPClientTypeStdio:
		if config.Stdio == nil {
			return nil, fmt.Errorf("workspace: stdio MCP %q missing config", config.Name)
		}
		return toolmcp.NewStdioClient(config.Name, toolmcp.StdioConfig{
			Command:              config.Stdio.Command,
			Args:                 append([]string(nil), config.Stdio.Args...),
			Env:                  cloneStringMap(config.Stdio.Env),
			CWD:                  config.Stdio.CWD,
			EncodingErrorHandler: config.Stdio.EncodingErrorHandler,
		}, opts...)
	case asworkspace.MCPClientTypeHTTP:
		if config.HTTP == nil {
			return nil, fmt.Errorf("workspace: HTTP MCP %q missing config", config.Name)
		}
		if config.HTTP.ContinuousListening {
			opts = append(opts, toolmcp.WithStreamableHTTPContinuousListening())
		}
		return toolmcp.NewHTTPClient(config.Name, toolmcp.HTTPConfig{
			URL:       config.HTTP.URL,
			Headers:   cloneStringMap(config.HTTP.Headers),
			Timeout:   config.HTTP.Timeout,
			Transport: toolmcp.HTTPTransport(config.HTTP.Transport),
		}, opts...)
	default:
		return nil, fmt.Errorf("workspace: unsupported MCP type %q", config.Type)
	}
}

func (w *Workspace) reconcileSkillsIndex(skillsDir string) (skillsIndex, error) {
	mkdirErr := os.MkdirAll(skillsDir, 0o700)
	if mkdirErr != nil {
		return skillsIndex{}, mkdirErr
	}
	index, err := w.loadSkillsIndex(skillsDir)
	if err != nil {
		return skillsIndex{}, err
	}
	actualDirs, err := skillSubdirs(skillsDir)
	if err != nil {
		return skillsIndex{}, err
	}
	changed := removeMissingSkillEntries(&index, actualDirs)
	existingNames := skillNameSet(index.Skills)
	existingHashes := skillHashSet(index.Skills)
	if addUnindexedSkills(skillsDir, &index, actualDirs, existingNames, existingHashes) {
		changed = true
	}
	info, err := os.Stat(skillsDir)
	if err != nil {
		return skillsIndex{}, err
	}
	mtime := info.ModTime().UnixNano()
	if index.SkillsDirMTime != mtime {
		index.SkillsDirMTime = mtime
		changed = true
	}
	if changed {
		if err := w.saveSkillsIndex(skillsDir, index); err != nil {
			return skillsIndex{}, err
		}
	}
	return index, nil
}

func removeMissingSkillEntries(index *skillsIndex, actualDirs map[string]struct{}) bool {
	changed := false
	for dirName := range index.Skills {
		if _, ok := actualDirs[dirName]; !ok {
			delete(index.Skills, dirName)
			changed = true
		}
	}
	return changed
}

func addUnindexedSkills(
	skillsDir string,
	index *skillsIndex,
	actualDirs map[string]struct{},
	existingNames map[string]struct{},
	existingHashes map[string]struct{},
) bool {
	changed := false
	for dirName := range actualDirs {
		if _, ok := index.Skills[dirName]; ok {
			continue
		}
		entry, ok := skillIndexEntryForDir(skillsDir, dirName, existingNames, existingHashes)
		if !ok {
			continue
		}
		index.Skills[dirName] = entry
		existingNames[entry.SkillName] = struct{}{}
		existingHashes[entry.Hash] = struct{}{}
		changed = true
	}
	return changed
}

func skillIndexEntryForDir(
	skillsDir string,
	dirName string,
	existingNames map[string]struct{},
	existingHashes map[string]struct{},
) (skillIndexEntry, bool) {
	dir := filepath.Join(skillsDir, dirName)
	loaded, loadErr := skill.LoadDir(context.Background(), dir)
	if loadErr != nil || loaded == nil {
		return skillIndexEntry{}, false
	}
	hash, hashErr := skillHash(dir)
	if hashErr != nil {
		return skillIndexEntry{}, false
	}
	if _, exists := existingHashes[hash]; exists {
		return skillIndexEntry{}, false
	}
	name := uniqueSkillNameFromSet(existingNames, loaded.Name)
	return skillIndexEntry{Hash: hash, SkillName: name}, true
}

func (w *Workspace) loadSkillsIndex(skillsDir string) (skillsIndex, error) {
	index := skillsIndex{Skills: map[string]skillIndexEntry{}}
	data, err := os.ReadFile(filepath.Join(skillsDir, ".skills"))
	if err != nil {
		if os.IsNotExist(err) {
			return index, nil
		}
		return index, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return index, nil
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return skillsIndex{Skills: map[string]skillIndexEntry{}}, nil
	}
	if index.Skills == nil {
		index.Skills = map[string]skillIndexEntry{}
	}
	return index, nil
}

func (w *Workspace) saveSkillsIndex(skillsDir string, index skillsIndex) error {
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		return err
	}
	if info, err := os.Stat(skillsDir); err == nil {
		index.SkillsDirMTime = info.ModTime().UnixNano()
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(skillsDir, ".skills"), data, 0o600)
}

func skillSubdirs(skillsDir string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			out[entry.Name()] = struct{}{}
		}
	}
	return out, nil
}

func sortedSkillDirs(entries map[string]skillIndexEntry) []string {
	dirs := make([]string, 0, len(entries))
	for dir := range entries {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return entries[dirs[i]].SkillName < entries[dirs[j]].SkillName
	})
	return dirs
}

func skillNameSet(entries map[string]skillIndexEntry) map[string]struct{} {
	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out[entry.SkillName] = struct{}{}
	}
	return out
}

func skillHashSet(entries map[string]skillIndexEntry) map[string]struct{} {
	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out[entry.Hash] = struct{}{}
	}
	return out
}

func uniqueSkillName(entries map[string]skillIndexEntry, raw string) string {
	return uniqueSkillNameFromSet(skillNameSet(entries), raw)
}

func uniqueSkillNameFromSet(names map[string]struct{}, raw string) string {
	name := raw
	for suffix := 1; ; suffix++ {
		if _, exists := names[name]; !exists {
			return name
		}
		name = fmt.Sprintf("%s (%d)", raw, suffix)
	}
}

func uniqueDirNameFromIndex(entries map[string]skillIndexEntry, base, skillsDir string) string {
	for suffix := 0; ; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
		if _, exists := entries[candidate]; exists {
			continue
		}
		if _, err := os.Stat(filepath.Join(skillsDir, candidate)); os.IsNotExist(err) {
			return candidate
		}
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("workspace: skill source contains symlink %s", path)
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

var (
	_ asworkspace.Workspace = (*Workspace)(nil)
	_ asworkspace.Offloader = (*Workspace)(nil)
)
