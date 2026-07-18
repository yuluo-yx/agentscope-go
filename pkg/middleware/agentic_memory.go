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

package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	agentpkg "github.com/yuluo-yx/agentscope-go/pkg/agent"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/model"
	"github.com/yuluo-yx/agentscope-go/pkg/types"
)

const (
	agenticMemoryHintSource     = "agentic-memory"
	agenticMemoryIndexFilename  = "MEMORY.md"
	agenticMemorySelectionLimit = 5
	agenticMemoryEmptyIndexText = "Your MEMORY.md is currently empty. When you save new memories, they will appear here."

	agenticMemoryDefaultDir                   = "Memory"
	agenticMemoryDefaultMaxTokens             = 4000
	agenticMemoryDefaultRetrievalMaxTokensPer = 2000
	agenticMemoryDefaultRetrievalMaxFiles     = 200
	agenticMemoryDefaultFrontmatterMaxTokens  = 256
)

// DefaultAgenticMemoryInstructions is the default instruction block appended to the
// system prompt before the MEMORY.md snapshot. "{memory_dir}" is replaced with the
// concrete memory directory at runtime.
const DefaultAgenticMemoryInstructions = `# Auto Memory

You have a persistent, file-based memory system at ` + "`{memory_dir}`" + `. This directory already exists — write to it directly with the ` + "`Write`" + ` tool (do not run mkdir or check for its existence).

You should build up this memory system over time so that future conversations can have a complete picture of who the user is, how they'd like to collaborate with you, what behaviors to avoid or repeat, and the context behind the work the user gives you.

If the user explicitly asks you to remember something, save it immediately as whichever type fits best. If they ask you to forget something, find and remove the relevant entry.

## Types of memory

There are several discrete types of memory that you can store in your memory system:

<types>
<type>
    <name>user</name>
    <description>Contain information about the user's role, goals, responsibilities, and knowledge. Great user memories help you tailor your future behavior to the user's preferences and perspective. Your goal in reading and writing these memories is to build up an understanding of who the user is and how you can be most helpful to them specifically. For example, you should collaborate with a senior software engineer differently than a student who is coding for the very first time. Keep in mind, that the aim here is to be helpful to the user. Avoid writing memories about the user that could be viewed as a negative judgement or that are not relevant to the work you're trying to accomplish together.</description>
    <when_to_save>When you learn any details about the user's role, preferences, responsibilities, or knowledge</when_to_save>
    <how_to_use>When your work should be informed by the user's profile or perspective. For example, if the user is asking you to explain a part of the code, you should answer that question in a way that is tailored to the specific details that they will find most valuable or that helps them build their mental model in relation to domain knowledge they already have.</how_to_use>
    <examples>
    user: I'm a data scientist investigating what logging we have in place
    assistant: [saves user memory: user is a data scientist, currently focused on observability/logging]

    user: I've been writing Go for ten years but this is my first time touching the React side of this repo
    assistant: [saves user memory: deep Go expertise, new to React and this project's frontend — frame frontend explanations in terms of backend analogues]
    </examples>
</type>
<type>
    <name>feedback</name>
    <description>Guidance the user has given you about how to approach work — both what to avoid and what to keep doing. These are a very important type of memory to read and write as they allow you to remain coherent and responsive to the way you should approach work in the project. Record from failure AND success: if you only save corrections, you will avoid past mistakes but drift away from approaches the user has already validated, and may grow overly cautious.</description>
    <when_to_save>Any time the user corrects your approach ("no not that", "don't", "stop doing X") OR confirms a non-obvious approach worked ("yes exactly", "perfect, keep doing that", accepting an unusual choice without pushback). Corrections are easy to notice; confirmations are quieter — watch for them. In both cases, save what is applicable to future conversations, especially if surprising or not obvious from the code. Include *why* so you can judge edge cases later.</when_to_save>
    <how_to_use>Let these memories guide your behavior so that the user does not need to offer the same guidance twice.</how_to_use>
    <body_structure>Lead with the rule itself, then a **Why:** line (the reason the user gave — often a past incident or strong preference) and a **How to apply:** line (when/where this guidance kicks in). Knowing *why* lets you judge edge cases instead of blindly following the rule.</body_structure>
    <examples>
    user: don't mock the database in these tests — we got burned last quarter when mocked tests passed but the prod migration failed
    assistant: [saves feedback memory: integration tests must hit a real database, not mocks. Reason: prior incident where mock/prod divergence
masked a broken migration]

    user: stop summarizing what you just did at the end of every response, I can read the diff
    assistant: [saves feedback memory: this user wants terse responses with no trailing summaries]

    user: yeah the single bundled PR was the right call here, splitting this one would've just been churn
    assistant: [saves feedback memory: for refactors in this area, user prefers one bundled PR over many small ones. Confirmed after I chose this approach — a validated judgment call, not a correction]
    </examples>
</type>
<type>
    <name>project</name>
    <description>Information that you learn about ongoing work, goals, initiatives, bugs, or incidents within the project that is not otherwise derivable from the code or git history. Project memories help you understand the broader context and motivation behind the work the user is doing within this working directory.</description>
    <when_to_save>When you learn who is doing what, why, or by when. These states change relatively quickly so try to keep your understanding of this up to date. Always convert relative dates in user messages to absolute dates when saving (e.g., "Thursday" → "2026-03-05"), so the memory remains interpretable after time passes.</when_to_save>
    <how_to_use>Use these memories to more fully understand the details and nuance behind the user's request and make better informed suggestions.</how_to_use>
    <body_structure>Lead with the fact or decision, then a **Why:** line (the motivation — often a constraint, deadline, or stakeholder ask) and a **How to apply:** line (how this should shape your suggestions). Project memories decay fast, so the why helps future-you judge whether the memory is still load-bearing.</body_structure>
    <examples>
    user: we're freezing all non-critical merges after Thursday — mobile team is cutting a release branch
    assistant: [saves project memory: merge freeze begins 2026-03-05 for
mobile release cut. Flag any non-critical PR work scheduled after that date]

    user: the reason we're ripping out the old auth middleware is that legal flagged it for storing session tokens in a way that doesn't meet the new compliance requirements
    assistant: [saves project memory: auth middleware rewrite is driven by legal/compliance requirements around session token storage, not tech-debt cleanup — scope decisions should favor compliance over ergonomics]
    </examples>
</type>
<type>
    <name>reference</name>
    <description>Stores pointers to where information can be found in external systems. These memories allow you to remember where to look to find up-to-date information outside of the project directory.</description>
    <when_to_save>When you learn about resources in external systems and their purpose. For example, that bugs are tracked in a specific project in Linear or that feedback can be found in a specific Slack channel.</when_to_save>
    <how_to_use>When the user references an external system or information that may be in an external system.</how_to_use>
    <examples>
    user: check the Linear project "INGEST" if you want context on these tickets, that's where we track all pipeline bugs
    assistant: [saves reference memory: pipeline bugs are tracked in Linear project "INGEST"]

    user: the Grafana board at grafana.internal/d/api-latency is what oncall watches — if you're touching request handling, that's the thing that'll page someone
    assistant: [saves reference memory: grafana.internal/d/api-latency is the oncall latency dashboard — check it when editing request-path code]
    </examples>
</type>
</types>

## What NOT to save in memory

- Code patterns, conventions, architecture, file paths, or project structure — these can be derived by reading the current project state.
- Git history, recent changes, or who-changed-what — ` + "`git log` / `git blame`" + ` are authoritative.
- Debugging solutions or fix recipes — the fix is in the code; the commit message has the context.
- Anything already documented in CLAUDE.md files.
- Ephemeral task details: in-progress work, temporary state, current conversation context.

These exclusions apply even when the user explicitly asks you to save. If they ask you to save a PR list or activity summary, ask what was *surprising* or *non-obvious* about it — that is the part worth keeping.

## How to save memories

Saving a memory is a two-step process:

**Step 1** — write the memory to its own file (e.g., ` + "`user_role.md`, `feedback_testing.md`" + `) using this frontmatter format:

` + "```markdown" + `
---
name: {{memory name}}
description: {{the retrieval trigger — one line stating the situation in which future-you should stop and Read this file before acting. See "Writing the description field".}}
type: {{user, feedback, project, reference}}
---

{{memory content — for feedback/project types, structure as: rule/fact, then **Why:** and **How to apply:** lines}}
` + "```" + `

**Step 2** — add a pointer to that file in ` + "`MEMORY.md`. `MEMORY.md`" + ` is an index, not a memory — each entry should be one line, under ~150 characters:
- [Title](file.md) — one-line hook. It has no frontmatter. Never write memory content directly into MEMORY.md.

- MEMORY.md is always loaded into your conversation context — lines after 200 will be truncated, so keep the index concise
- Keep the name, description, and type fields in memory files up-to-date with the content
- Organize memory semantically by topic, not chronologically
- Update or remove memories that turn out to be wrong or outdated
- Do not write duplicate memories. First check if there is an existing memory you can update before writing a new one.

## Writing the ` + "`description`" + ` field

The ` + "`description`" + ` is not a summary of the file — it is the *retrieval trigger* that a future conversation sees in ` + "`MEMORY.md`" + ` and in your system prompt *before the file is ever opened*. Write it so that from that one line alone, future-you can decide whether to stop and ` + "`Read`" + ` the file. A description that only names the topic (e.g. "notes on PR review") fails: it says what the file is about, not *when* it becomes load-bearing.

State **the recall condition** — the situation that should make you open the file, phrased as a trigger, not a label. Prefer the form: "When you are about to <situation> and this file's content is not already in your context, Read it first, then act."

- Weak:   ` + "`Guidelines for reviewing PRs`" + `
- Strong: ` + "`When you are about to review a PR in the xxx repo and this file is not already in your context, you must Read it before acting`" + `

Keep it to one line, but spend that line on the *trigger*, not on restating the title.

## When to access memories
- When memories seem relevant, or the user references prior-conversation work.
- You MUST access memory when the user explicitly asks you to check, recall, or remember.
- If the user says to ignore or not use memory: proceed as if MEMORY.md were empty. Do not apply remembered facts, cite, compare against, or mention memory content.
- Memory records can become stale over time. Use memory as context for what was true at a given point in time. Before answering the user or building assumptions based solely on information in memory records, verify that the memory is still correct and up-to-date by reading the current state of the files or resources. If a recalled memory conflicts with current information, trust what you observe now — and update or remove the stale memory rather than acting on it.

## Before recommending from memory

A memory that names a specific function, file, or flag is a claim that it existed *when the memory was written*. It may have been renamed, removed, or never merged. Before recommending it:
- If the memory names a file path: check the file exists.
- If the memory names a function or flag: grep for it.
- If the user is about to act on your recommendation (not just asking about history), verify first.

"The memory says X exists" is not the same as "X exists now."

A memory that summarizes repo state (activity logs, architecture snapshots) is frozen in time. If the user asks about *recent* or *current* state, prefer ` + "`git log`" + ` or reading the code over recalling the snapshot.

## Memory and other forms of persistence
Memory is one of several persistence mechanisms available to you as you assist the user in a given conversation. The distinction is often that memory can be recalled in future conversations and should not be used for persisting information that is only useful within the scope of the current conversation.
- When to use or update a plan instead of memory: If you are about to start a non-trivial implementation task and would like to reach alignment with the user on your approach you should use a Plan rather than saving this information to memory. Similarly, if you already have a plan within the conversation and you have changed your approach persist that change by updating the plan rather than saving a memory.
- When to use or update tasks instead of memory: When you need to break your work in current conversation into discrete steps or keep track of your progress use tasks instead of saving to memory. Tasks are great for persisting information about the work that needs to be done in the current conversation, but memory should be reserved for information that will be useful in future conversations.

## Searching past context

When looking for past context:
1. Search topic files in your memory directory:
` + "```" + `
Grep with pattern="<search>" path="{memory_dir}" glob="*.md"</search>
# or Bash command:
grep -rn "<search term>" {memory_dir} --include="*.md"
` + "```" + `
Use narrow search terms (error messages, file paths, function names) rather than broad keywords.
`

// DefaultAgenticRetrievalInstructions is the default instruction used to select
// relevant memory files for a user query in the asynchronous retrieval task.
const DefaultAgenticRetrievalInstructions = "You are selecting memory files that will be useful as context for " +
	"processing a user's query. You will be given the user's query and a " +
	"list of available memory files with their filenames and descriptions.\n\n" +
	"Return a list of filenames for the memories that will clearly be " +
	"useful (up to 5). Only include memories that you are certain will be " +
	"helpful based on their name and description.\n" +
	"- If you are unsure whether a memory will be useful, do not include " +
	"it. Be selective and discerning.\n" +
	"- If no memories would clearly be useful, return an empty list."

// agenticMemoryFileHeader is the lightweight header for one memory file, read from frontmatter only.
type agenticMemoryFileHeader struct {
	// Filename is the path relative to the memory directory (e.g. "user_role.md").
	Filename string
	// Path is the absolute path of the memory file.
	Path string
	// Description is the one-line description from frontmatter; empty when absent.
	Description string
	// Type is the memory type tag from frontmatter (user/feedback/project/reference).
	Type string
	// MTime is the modification time; zero when unavailable.
	MTime time.Time
}

// agenticMemorySelectionResponse is the structured output schema of the memory relevance selector.
type agenticMemorySelectionResponse struct {
	SelectedFiles []string `json:"selected_files"`
}

// AgenticMemoryFileStore is the storage backend used by AgenticMemoryMiddleware.
// The default implementation is the local filesystem rooted at the agent workdir.
type AgenticMemoryFileStore interface {
	// ReadFile reads the whole file at path.
	ReadFile(ctx context.Context, path string) ([]byte, error)
	// WriteFile writes data to path, creating parent directories as needed.
	WriteFile(ctx context.Context, path string, data []byte) error
	// FileExists reports whether path exists.
	FileExists(ctx context.Context, path string) (bool, error)
	// ListMarkdownFiles lists Markdown files under dir recursively and returns
	// paths relative to dir. MEMORY.md entries are kept; callers filter.
	ListMarkdownFiles(ctx context.Context, dir string) ([]string, error)
	// StatMTime returns the file modification time. A zero time is returned
	// when the mtime is unavailable.
	StatMTime(ctx context.Context, path string) (time.Time, error)
}

type localAgenticMemoryFileStore struct{}

func (localAgenticMemoryFileStore) ReadFile(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (localAgenticMemoryFileStore) WriteFile(_ context.Context, path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("agentscope/middleware: create memory dir: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

func (localAgenticMemoryFileStore) FileExists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (localAgenticMemoryFileStore) ListMarkdownFiles(_ context.Context, dir string) ([]string, error) {
	files := []string{}
	root := filepath.Clean(dir)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func (localAgenticMemoryFileStore) StatMTime(_ context.Context, path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// AgenticMemoryOption configures AgenticMemoryMiddleware.
type AgenticMemoryOption func(*AgenticMemoryMiddleware)

// AgenticMemoryMiddleware keeps a workspace-local Markdown memory store. The LLM
// decides when and what to save; a bounded MEMORY.md index is injected into the
// system prompt, and an asynchronous per-reply retrieval task surfaces relevant
// topic files as hint blocks during the reasoning loop.
type AgenticMemoryMiddleware struct {
	workdir   string
	memoryDir string
	store     AgenticMemoryFileStore

	// memoryMaxTokens caps the MEMORY.md snapshot inserted into the system prompt.
	memoryMaxTokens int
	// memoryInstructions is appended to the system prompt before the MEMORY.md snapshot.
	memoryInstructions string

	// retrievalAsync controls whether an asynchronous retrieval task runs during the reply.
	retrievalAsync bool
	// retrievalModel selects relevant memory files. When nil, retrieval is skipped
	// unless WithAgenticRetrievalModel supplied a model: Go middleware cannot reach
	// the agent's model through AgentAccessor.
	retrievalModel model.ChatModel
	// retrievalMaxTokensPerFile caps tokens read from each surfaced memory file.
	retrievalMaxTokensPerFile int
	// retrievalMaxFiles caps Markdown memory files considered during relevance selection.
	retrievalMaxFiles int
	// retrievalMaxTokensPerFrontmatter caps tokens read from the beginning of each
	// Markdown file when parsing frontmatter.
	retrievalMaxTokensPerFrontmatter int
	// retrievalInstructions is the selector prompt used by the retrieval task.
	retrievalInstructions string

	// mu guards the per-reply retrieval task state. OnReply starts the task and
	// OnReasoning polls it across reasoning iterations, mirroring the Python
	// asyncio task that lives on the middleware instance between hooks.
	mu             sync.Mutex
	cachedInput    string
	retrievalCh    chan string
	retrievalErrCh chan error
}

// NewAgenticMemoryMiddleware creates a filesystem-backed long-term memory middleware.
// workdir is the agent working directory; memory files live under <workdir>/Memory
// unless WithAgenticMemoryDir overrides the directory name.
func NewAgenticMemoryMiddleware(workdir string, opts ...AgenticMemoryOption) *AgenticMemoryMiddleware {
	m := &AgenticMemoryMiddleware{
		workdir:                          strings.TrimSpace(workdir),
		memoryDir:                        agenticMemoryDefaultDir,
		store:                            localAgenticMemoryFileStore{},
		memoryMaxTokens:                  agenticMemoryDefaultMaxTokens,
		memoryInstructions:               DefaultAgenticMemoryInstructions,
		retrievalAsync:                   true,
		retrievalMaxTokensPerFile:        agenticMemoryDefaultRetrievalMaxTokensPer,
		retrievalMaxFiles:                agenticMemoryDefaultRetrievalMaxFiles,
		retrievalMaxTokensPerFrontmatter: agenticMemoryDefaultFrontmatterMaxTokens,
		retrievalInstructions:            DefaultAgenticRetrievalInstructions,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	if strings.TrimSpace(m.memoryDir) == "" {
		m.memoryDir = agenticMemoryDefaultDir
	}
	if m.store == nil {
		m.store = localAgenticMemoryFileStore{}
	}
	if m.memoryMaxTokens <= 0 {
		m.memoryMaxTokens = agenticMemoryDefaultMaxTokens
	}
	if strings.TrimSpace(m.memoryInstructions) == "" {
		m.memoryInstructions = DefaultAgenticMemoryInstructions
	}
	if m.retrievalMaxTokensPerFile <= 0 {
		m.retrievalMaxTokensPerFile = agenticMemoryDefaultRetrievalMaxTokensPer
	}
	if m.retrievalMaxFiles <= 0 {
		m.retrievalMaxFiles = agenticMemoryDefaultRetrievalMaxFiles
	}
	if m.retrievalMaxTokensPerFrontmatter <= 0 {
		m.retrievalMaxTokensPerFrontmatter = agenticMemoryDefaultFrontmatterMaxTokens
	}
	if strings.TrimSpace(m.retrievalInstructions) == "" {
		m.retrievalInstructions = DefaultAgenticRetrievalInstructions
	}
	return m
}

// WithAgenticMemoryDir sets the directory (relative to the workdir) that stores
// the long-term memory files, including MEMORY.md.
func WithAgenticMemoryDir(dir string) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.memoryDir = strings.TrimSpace(dir)
	}
}

// WithAgenticMemoryStore sets the storage backend. The default is the local filesystem.
func WithAgenticMemoryStore(store AgenticMemoryFileStore) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.store = store
	}
}

// WithAgenticMemoryMaxTokens sets the maximum tokens of MEMORY.md inserted into the system prompt.
func WithAgenticMemoryMaxTokens(maxTokens int) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.memoryMaxTokens = maxTokens
	}
}

// WithAgenticMemoryInstructions sets the instructions appended to the system prompt
// before the MEMORY.md snapshot. "{memory_dir}" is replaced at runtime.
func WithAgenticMemoryInstructions(instructions string) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.memoryInstructions = instructions
	}
}

// WithAgenticRetrievalAsync controls whether relevant memory files are retrieved
// asynchronously during the agent reply. Enabled by default.
func WithAgenticRetrievalAsync(enabled bool) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.retrievalAsync = enabled
	}
}

// WithAgenticRetrievalModel sets the LLM used to select relevant memory files.
// Unlike the Python version, Go middleware cannot fall back to the agent's model,
// so async retrieval requires an explicit retrieval model.
func WithAgenticRetrievalModel(retrievalModel model.ChatModel) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.retrievalModel = retrievalModel
	}
}

// WithAgenticRetrievalMaxTokensPerFile caps tokens read from each surfaced memory file.
func WithAgenticRetrievalMaxTokensPerFile(maxTokens int) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.retrievalMaxTokensPerFile = maxTokens
	}
}

// WithAgenticRetrievalMaxFiles caps Markdown memory files considered during relevance selection.
func WithAgenticRetrievalMaxFiles(maxFiles int) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.retrievalMaxFiles = maxFiles
	}
}

// WithAgenticRetrievalMaxTokensPerFrontmatter caps tokens read from the beginning of
// each Markdown file when parsing frontmatter.
func WithAgenticRetrievalMaxTokensPerFrontmatter(maxTokens int) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.retrievalMaxTokensPerFrontmatter = maxTokens
	}
}

// WithAgenticRetrievalInstructions sets the instructions used to select relevant
// memory files for a user query in the asynchronous retrieval task.
func WithAgenticRetrievalInstructions(instructions string) AgenticMemoryOption {
	return func(m *AgenticMemoryMiddleware) {
		m.retrievalInstructions = instructions
	}
}

// MiddlewareName returns the middleware name.
func (*AgenticMemoryMiddleware) MiddlewareName() string {
	return "agentic-memory"
}

// OnSystemPrompt appends memory instructions and a bounded MEMORY.md snapshot.
func (m *AgenticMemoryMiddleware) OnSystemPrompt(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	currentPrompt string,
) (string, error) {
	_ = agent
	if m == nil {
		return currentPrompt, nil
	}
	if err := m.ensureLayout(ctx); err != nil {
		return "", err
	}
	memoryContent, err := m.memoryIndexContent(ctx)
	if err != nil {
		return "", err
	}

	memoryTruncated := truncateToTokenBudget(memoryContent, m.memoryMaxTokens)
	if len(memoryTruncated) != len(memoryContent) {
		remainLines := len(strings.Split(memoryTruncated, "\n"))
		omittedLines := len(strings.Split(memoryContent, "\n")) - remainLines
		memoryTruncated += fmt.Sprintf(
			"\n<<<TRUNCATED>>>\n<system-reminder>The remaining %d lines have been omitted due to context "+
				"length limits. Use the `Read` tool with offset `%d` to access the rest of '%s'.</system-reminder>",
			omittedLines, remainLines, m.memoryIndexPath(),
		)
	}
	if strings.TrimSpace(memoryTruncated) == "" {
		memoryTruncated = agenticMemoryEmptyIndexText
	}

	instructions := strings.ReplaceAll(m.memoryInstructions, "{memory_dir}", m.memoryDirPath())
	content := instructions + "\n## MEMORY.md\n" + memoryTruncated
	return appendPrompt(currentPrompt, content), nil
}

// OnReply caches the user input and kicks off an asynchronous retrieval task that
// runs concurrently with the agent reply. The result is consumed by OnReasoning.
func (m *AgenticMemoryMiddleware) OnReply(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	if m == nil || !m.retrievalAsync {
		return next(ctx)
	}

	query := agenticMemoryQueryText(input["input"])
	if strings.TrimSpace(query) != "" && m.retrievalModel != nil {
		m.startRetrieval(ctx, agent, query)
	}

	events, err := next(ctx)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return nil, fmt.Errorf("agentscope/middleware: nil event stream")
	}

	out := make(chan message.Event)
	go func() {
		defer close(out)
		for event := range events {
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// OnReasoning polls the in-flight retrieval task; when it has finished, its result
// is injected into the agent context as a hint block exactly once.
func (m *AgenticMemoryMiddleware) OnReasoning(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	input agentpkg.HookInput,
	next agentpkg.EventHandler,
) (<-chan message.Event, error) {
	if m == nil {
		return next(ctx)
	}
	if retrievalResult, done := m.pollRetrieval(); done && strings.TrimSpace(retrievalResult) != "" {
		blocks := message.ContentBlockList{
			message.NewHintBlock(retrievalResult, message.WithHintSource(agenticMemoryHintSource)),
		}
		if err := appendInboxBlocks(agent, blocks); err != nil {
			return nil, err
		}
	}
	return next(ctx)
}

// startRetrieval launches the asynchronous relevance-selection task. Any task
// still in flight from a previous reply is replaced; its goroutine writes to
// buffered channels and is garbage-collected without blocking.
func (m *AgenticMemoryMiddleware) startRetrieval(ctx context.Context, agent agentpkg.AgentAccessor, query string) {
	m.mu.Lock()
	m.cachedInput = query
	m.retrievalCh = make(chan string, 1)
	m.retrievalErrCh = make(chan error, 1)
	resultCh := m.retrievalCh
	errCh := m.retrievalErrCh
	m.mu.Unlock()

	go func() {
		result, err := m.retrieveRelevantFiles(ctx, agent, query)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()
}

// pollRetrieval consumes the finished retrieval result exactly once. The second
// return value reports whether the task has finished (successfully or not).
func (m *AgenticMemoryMiddleware) pollRetrieval() (string, bool) {
	m.mu.Lock()
	resultCh := m.retrievalCh
	errCh := m.retrievalErrCh
	if resultCh == nil {
		m.mu.Unlock()
		return "", false
	}
	select {
	case result := <-resultCh:
		m.retrievalCh = nil
		m.retrievalErrCh = nil
		m.mu.Unlock()
		return result, true
	case err := <-errCh:
		m.retrievalCh = nil
		m.retrievalErrCh = nil
		m.mu.Unlock()
		if err != nil {
			slog.Default().Debug("agentic memory retrieval failed", "error", err)
		}
		return "", true
	default:
		m.mu.Unlock()
		return "", false
	}
}

// retrieveRelevantFiles uses an LLM to identify memory files relevant to query and
// returns their content as an injectable string. Empty when nothing relevant was found.
func (m *AgenticMemoryMiddleware) retrieveRelevantFiles(
	ctx context.Context,
	agent agentpkg.AgentAccessor,
	query string,
) (string, error) {
	_ = agent
	if err := m.ensureLayout(ctx); err != nil {
		return "", err
	}

	headers, err := m.listMemoryFiles(ctx)
	if err != nil {
		return "", err
	}
	if len(headers) == 0 {
		return "", nil
	}
	validFilenames := make(map[string]struct{}, len(headers))
	for _, header := range headers {
		validFilenames[header.Filename] = struct{}{}
	}
	manifest := formatAgenticMemoryManifest(headers)

	selected, err := m.selectRelevantFiles(ctx, query, manifest)
	if err != nil {
		return "", err
	}
	filtered := make([]string, 0, len(selected))
	for _, filename := range selected {
		if _, ok := validFilenames[filename]; ok {
			filtered = append(filtered, filename)
		}
		if len(filtered) >= agenticMemorySelectionLimit {
			break
		}
	}
	if len(filtered) == 0 {
		return "", nil
	}

	headerByFilename := make(map[string]agenticMemoryFileHeader, len(headers))
	for _, header := range headers {
		headerByFilename[header.Filename] = header
	}
	parts := make([]string, 0, len(filtered))
	for _, filename := range filtered {
		header := headerByFilename[filename]
		content, err := m.store.ReadFile(ctx, header.Path)
		if err != nil {
			continue
		}
		text := truncateToTokenBudget(string(content), m.retrievalMaxTokensPerFile)
		parts = append(parts, agenticMemoryFileHeaderLine(header, time.Now())+"\n\n"+text)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}

// selectRelevantFiles asks the retrieval model to pick relevant memory files.
func (m *AgenticMemoryMiddleware) selectRelevantFiles(
	ctx context.Context,
	query, manifest string,
) ([]string, error) {
	systemMsg, err := message.NewSystemMessage("system", m.retrievalInstructions)
	if err != nil {
		return nil, fmt.Errorf("agentscope/middleware: build retrieval system message: %w", err)
	}
	userMsg, err := message.NewUserMessage("user", "Query: "+query+"\n\nAvailable memories:\n"+manifest)
	if err != nil {
		return nil, fmt.Errorf("agentscope/middleware: build retrieval user message: %w", err)
	}
	request := model.StructuredOutputRequest{
		CallRequest: model.CallRequest{Messages: []*message.Message{systemMsg, userMsg}},
		Name:        "agentic_memory_selection",
		Schema:      agenticMemorySelectionSchema(),
		Strict:      true,
	}
	structuredModel, ok := m.retrievalModel.(model.StructuredOutputModel)
	if !ok {
		return nil, fmt.Errorf(
			"agentscope/middleware: retrieval model %q does not support structured output",
			m.retrievalModel.Name(),
		)
	}
	response, err := structuredModel.GenerateStructured(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("agentscope/middleware: structured retrieval call: %w", err)
	}
	var selection agenticMemorySelectionResponse
	if err := remarshalAny(response.Content, &selection); err != nil {
		return nil, fmt.Errorf("agentscope/middleware: decode retrieval selection: %w", err)
	}
	files := make([]string, 0, len(selection.SelectedFiles))
	for _, filename := range selection.SelectedFiles {
		if filename = strings.TrimSpace(filename); filename != "" {
			files = append(files, filename)
		}
	}
	return files, nil
}

// ensureLayout creates the memory directory and the MEMORY.md index idempotently.
// Existing human-edited documents are never replaced.
func (m *AgenticMemoryMiddleware) ensureLayout(ctx context.Context) error {
	exists, err := m.store.FileExists(ctx, m.memoryIndexPath())
	if err != nil {
		return fmt.Errorf("agentscope/middleware: check memory index: %w", err)
	}
	if exists {
		return nil
	}
	slog.Default().Debug("creating MEMORY.md file", "workdir", m.workdir)
	if err := m.store.WriteFile(ctx, m.memoryIndexPath(), nil); err != nil {
		return fmt.Errorf("agentscope/middleware: create memory index: %w", err)
	}
	return nil
}

// memoryDirPath returns the absolute path of the memory directory.
func (m *AgenticMemoryMiddleware) memoryDirPath() string {
	return filepath.Join(m.workdir, m.memoryDir)
}

// memoryIndexPath returns the absolute path of the MEMORY.md index file.
func (m *AgenticMemoryMiddleware) memoryIndexPath() string {
	return filepath.Join(m.memoryDirPath(), agenticMemoryIndexFilename)
}

// memoryIndexContent returns the decoded MEMORY.md content; empty when the file
// does not exist.
func (m *AgenticMemoryMiddleware) memoryIndexContent(ctx context.Context) (string, error) {
	exists, err := m.store.FileExists(ctx, m.memoryIndexPath())
	if err != nil {
		return "", fmt.Errorf("agentscope/middleware: check memory index: %w", err)
	}
	if !exists {
		return "", nil
	}
	content, err := m.store.ReadFile(ctx, m.memoryIndexPath())
	if err != nil {
		return "", fmt.Errorf("agentscope/middleware: read memory index: %w", err)
	}
	return string(content), nil
}

var (
	agenticMemoryFrontmatterRe = regexp.MustCompile(`(?s)^\s*---\s*\n(.*?)\n---\s*\n`)
	agenticMemoryFieldRe       = regexp.MustCompile(`(?m)^(\w+)\s*:\s*(.+)$`)
)

// parseFrontmatterFields returns the scalar key/value pairs of the first frontmatter
// block. Nested structures are intentionally ignored.
func parseFrontmatterFields(content string) map[string]string {
	match := agenticMemoryFrontmatterRe.FindStringSubmatch(content)
	if match == nil {
		return map[string]string{}
	}
	fields := map[string]string{}
	for _, field := range agenticMemoryFieldRe.FindAllStringSubmatch(match[1], -1) {
		fields[field[1]] = strings.TrimSpace(field[2])
	}
	return fields
}

// listMemoryFiles scans the memory directory for topic files. MEMORY.md is excluded.
// Headers are sorted newest-first and capped by retrievalMaxFiles.
func (m *AgenticMemoryMiddleware) listMemoryFiles(ctx context.Context) ([]agenticMemoryFileHeader, error) {
	dir := m.memoryDirPath()
	files, err := m.store.ListMarkdownFiles(ctx, dir)
	if err != nil {
		slog.Default().Debug("agentic memory scan failed", "dir", dir, "error", err)
		return nil, nil
	}

	maxFrontmatterBytes := estimateTokenBytes(m.retrievalMaxTokensPerFrontmatter)
	headers := make([]agenticMemoryFileHeader, 0, len(files))
	for _, filename := range files {
		filename = filepath.ToSlash(filename)
		if filename == agenticMemoryIndexFilename || !strings.HasSuffix(filename, ".md") {
			continue
		}
		fullPath := filepath.Join(dir, filepath.FromSlash(filename))
		raw, err := m.store.ReadFile(ctx, fullPath)
		if err != nil {
			continue
		}
		if len(raw) > maxFrontmatterBytes {
			raw = raw[:maxFrontmatterBytes]
		}
		fields := parseFrontmatterFields(string(raw))
		mtime, err := m.store.StatMTime(ctx, fullPath)
		if err != nil {
			mtime = time.Time{}
		}
		headers = append(headers, agenticMemoryFileHeader{
			Filename:    filename,
			Path:        fullPath,
			Description: fields["description"],
			Type:        fields["type"],
			MTime:       mtime,
		})
	}
	sort.SliceStable(headers, func(i, j int) bool {
		return headers[i].MTime.After(headers[j].MTime)
	})
	if len(headers) > m.retrievalMaxFiles {
		headers = headers[:m.retrievalMaxFiles]
	}
	return headers, nil
}

// formatAgenticMemoryManifest formats headers into a one-line-per-file manifest for the selector prompt.
func formatAgenticMemoryManifest(headers []agenticMemoryFileHeader) string {
	lines := make([]string, 0, len(headers))
	for _, header := range headers {
		tag := ""
		if header.Type != "" {
			tag = "[" + header.Type + "] "
		}
		timestamp := "unknown"
		if !header.MTime.IsZero() {
			timestamp = header.MTime.Format("2006-01-02")
		}
		description := ""
		if header.Description != "" {
			description = ": " + header.Description
		}
		lines = append(lines, fmt.Sprintf("- %s%s (%s)%s", tag, header.Filename, timestamp, description))
	}
	return strings.Join(lines, "\n")
}

// agenticMemoryFileHeaderLine formats the "Memory (saved ...): path:" header line of one injected file.
func agenticMemoryFileHeaderLine(header agenticMemoryFileHeader, now time.Time) string {
	if header.MTime.IsZero() {
		return "Memory: " + header.Path + ":"
	}
	days := int(now.Sub(header.MTime).Hours() / 24)
	if days < 0 {
		days = 0
	}
	age := ""
	switch days {
	case 0:
		age = "today"
	case 1:
		age = "yesterday"
	default:
		age = fmt.Sprintf("%d days ago", days)
	}
	return fmt.Sprintf("Memory (saved %s): %s:", age, header.Path)
}

// agenticMemoryQueryText extracts the retrieval query from the reply input, joining
// "name: text" lines for message inputs like the Python version does.
func agenticMemoryQueryText(value any) string {
	switch typed := value.(type) {
	case *message.Message:
		if typed == nil {
			return ""
		}
		return agenticMemoryQueryLine(typed)
	case message.Message:
		return agenticMemoryQueryLine(&typed)
	case []*message.Message:
		lines := make([]string, 0, len(typed))
		for _, msg := range typed {
			if line := agenticMemoryQueryLine(msg); line != "" {
				lines = append(lines, line)
			}
		}
		return strings.Join(lines, "\n")
	case []message.Message:
		lines := make([]string, 0, len(typed))
		for index := range typed {
			if line := agenticMemoryQueryLine(&typed[index]); line != "" {
				lines = append(lines, line)
			}
		}
		return strings.Join(lines, "\n")
	default:
		return memoryText(value)
	}
}

func agenticMemoryQueryLine(msg *message.Message) string {
	if msg == nil {
		return ""
	}
	text := msg.GetTextContent("\n")
	if text == nil || strings.TrimSpace(*text) == "" {
		return ""
	}
	return strings.TrimSpace(msg.Name) + ": " + strings.TrimSpace(*text)
}

// agenticMemorySelectionSchema returns the JSON schema of the selector structured output.
func agenticMemorySelectionSchema() types.JSONSchema {
	return types.JSONSchema{
		"type": "object",
		"properties": map[string]any{
			"selected_files": map[string]any{
				"type": "array",
				"description": "Filenames of the memory files to surface, relative to the " +
					"memory directory (e.g. 'user_role.md'). Up to 5 entries.",
				"items": map[string]any{"type": "string"},
			},
		},
		"required":             []string{"selected_files"},
		"additionalProperties": false,
	}
}

// estimateTokens approximates the token count of text as len(utf-8 bytes)/4 rounded
// to nearest, matching the Python _estimate_tokens helper.
func estimateTokens(text string) int {
	return int(float64(len([]byte(text)))/4 + 0.5)
}

// estimateTokenBytes approximates the byte length of a token budget.
func estimateTokenBytes(tokens int) int {
	return tokens * 4
}

// truncateToTokenBudget returns content truncated to at most maxTokens estimated tokens.
func truncateToTokenBudget(content string, maxTokens int) string {
	if maxTokens <= 0 {
		return ""
	}
	nTokens := estimateTokens(content)
	if nTokens <= maxTokens {
		return content
	}
	index := int(float64(maxTokens) / float64(nTokens) * float64(len(content)))
	for index > 0 && estimateTokens(content[:index]) > maxTokens {
		index = max(0, index-10)
	}
	return content[:index]
}

// remarshalAny converts an arbitrary decoded JSON value into out via JSON round-trip.
func remarshalAny(value any, out any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

var (
	_ agentpkg.ReplyMiddleware        = (*AgenticMemoryMiddleware)(nil)
	_ agentpkg.ReasoningMiddleware    = (*AgenticMemoryMiddleware)(nil)
	_ agentpkg.SystemPromptMiddleware = (*AgenticMemoryMiddleware)(nil)
)
